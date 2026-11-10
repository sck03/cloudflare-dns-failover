package main

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"cfguard/internal/health"
	"github.com/robfig/cron/v3"
)

func TestScheduleChangesKeepPeriodicDeadline(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	if err := engine.Reload(); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	check := engine.plans[m.ID].check
	engine.mu.Unlock()
	before := engine.cron.Entry(check).Next
	schedule := Schedule{MonitorID: m.ID, Cron: "0 8 * * *", TargetIP: m.BackupIP}
	if err := DB.Create(&schedule).Error; err != nil {
		t.Fatal(err)
	}
	for _, remove := range []bool{false, true} {
		if remove {
			if err := DB.Delete(&schedule).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := engine.Reload(); err != nil {
			t.Fatal(err)
		}
		if got := engine.cron.Entry(check); got.ID != check || !got.Next.Equal(before) {
			t.Fatalf("editing a switch schedule reset the periodic check: before=%v after=%+v", before, got)
		}
	}
}

func TestCronPanicDoesNotDisableFutureRuns(t *testing.T) {
	testDatabase(t)
	var calls int
	id, err := engine.cron.AddFunc("@hourly", func() {
		calls++
		if calls == 1 {
			panic("simulated job failure")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	job := engine.cron.Entry(id).WrappedJob
	job.Run()
	job.Run()
	if calls != 2 {
		t.Fatal("a recovered panic permanently disabled the job")
	}
}

func TestIntervalChangeKeepsSwitchScheduleAndDeletionRemovesBoth(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	if err := DB.Create(&Schedule{MonitorID: m.ID, Cron: "0 8 * * *", TargetIP: m.BackupIP}).Error; err != nil {
		t.Fatal(err)
	}
	if err := engine.Reload(); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	check, scheduled := engine.plans[m.ID].check, engine.plans[m.ID].switches[0]
	engine.mu.Unlock()
	before := engine.cron.Entry(scheduled).Next
	if err := DB.Model(&m).Update("interval", 120).Error; err != nil {
		t.Fatal(err)
	}
	if err := engine.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := engine.cron.Entry(scheduled); got.ID != scheduled || !got.Next.Equal(before) {
		t.Fatalf("interval edit replaced the switch schedule: %+v", got)
	}
	if engine.cron.Entry(check).ID != 0 {
		t.Fatal("obsolete periodic check was retained")
	}
	response := requestAPI(t, NewRouter(), "DELETE", fmt.Sprintf("/api/monitors/%d", m.ID), nil)
	if response.Code != 200 {
		t.Fatalf("delete failed: %s", response.Body)
	}
	if engine.cron.Entry(scheduled).ID != 0 || len(engine.cron.Entries()) != 1 {
		t.Fatal("deleting a monitor retained scheduled jobs")
	}
}

func TestCronSkipsOnlyOverlappingRunsOfTheSameJob(t *testing.T) {
	testDatabase(t)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	id := engine.cron.Schedule(cron.Every(time.Hour), cron.FuncJob(func() {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
	}))
	job := engine.cron.Entry(id).WrappedJob
	go func() { job.Run(); close(done) }()
	<-started
	t.Cleanup(func() { close(release); <-done })
	job.Run()
	if calls.Load() != 1 {
		t.Fatal("an overlapping invocation ran")
	}
	other := engine.cron.Schedule(cron.Every(time.Hour), cron.FuncJob(func() { calls.Add(1) }))
	engine.cron.Entry(other).WrappedJob.Run()
	if calls.Load() != 2 {
		t.Fatal("an unrelated job was blocked")
	}
}

func TestCheckQueueBoundsWorkersAndCoalescesPendingMonitors(t *testing.T) {
	testDatabase(t)
	const count = 40
	monitors := make([]Monitor, count)
	for i := range monitors {
		monitors[i] = Monitor{Name: fmt.Sprint(i), Type: "ping", DNSType: "A",
			CFDomain: fmt.Sprintf("site%d.example", i), CFZoneID: "zone",
			OriginalIP: "192.0.2.1", BackupIP: "192.0.2.2", Interval: 86400}
	}
	if err := DB.Create(&monitors).Error; err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	probes := make(chan struct{}, 2*count)
	engine.checker = probeFunc(func(ctx context.Context, _ health.Spec, _ string) error {
		probes <- struct{}{}
		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	before := runtime.NumGoroutine()
	if err := engine.Reload(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(30 * time.Second)
	for range 4 { // Two workers, two origins each.
		select {
		case <-probes:
		case <-deadline:
			t.Fatal("workers did not start")
		}
	}
	for range 3 {
		for _, m := range monitors {
			engine.enqueueCheck(m.ID)
		}
	}
	engine.mu.Lock()
	pending, queued := len(engine.pending), len(engine.queue)
	engine.mu.Unlock()
	if pending != count || queued != count-2 {
		t.Fatalf("duplicate checks queued: pending=%d queued=%d", pending, queued)
	}
	if growth := runtime.NumGoroutine() - before; growth >= count/2 {
		t.Fatalf("waiting monitors retained goroutines: growth=%d", growth)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	unlock, err := engine.Lock(ctx, monitors[count-1].ID)
	if err != nil {
		t.Fatalf("queued check blocked configuration edits: %v", err)
	}
	unlock()
	close(gate)
	for range count*2 - 4 {
		select {
		case <-probes:
		case <-deadline:
			t.Fatal("queue did not drain all monitors")
		}
	}
	engine.Stop()
	if len(engine.pending) != 0 || len(engine.queue) != 0 {
		t.Fatal("stopped engine retained pending work")
	}
}
