package main

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"cfguard/internal/health"
)

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
