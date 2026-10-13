package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cfguard/internal/health"
)

type probeFunc func(context.Context, health.Spec, string) error

func (f probeFunc) Check(ctx context.Context, spec health.Spec, origin string) error {
	return f(ctx, spec, origin)
}

type memoryDNS struct {
	mu        sync.Mutex
	address   string
	failures  int
	switches  []string
	inFlight  atomic.Int32
	maxFlight atomic.Int32
}

func (d *memoryDNS) Read(_ context.Context, m *Monitor) (*CloudflareRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return &CloudflareRecord{ID: "record", Type: m.DNSType, Name: m.CFDomain, Content: d.address}, nil
}

func (d *memoryDNS) Switch(ctx context.Context, m *Monitor, target string) (string, error) {
	active := d.inFlight.Add(1)
	defer d.inFlight.Add(-1)
	for old := d.maxFlight.Load(); active > old; old = d.maxFlight.Load() {
		if d.maxFlight.CompareAndSwap(old, active) {
			break
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.switches = append(d.switches, target)
	if d.failures > 0 {
		d.failures--
		return "", errors.New("provider unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	from := d.address
	d.address = target
	return from, nil
}

func createTestMonitor(t *testing.T) Monitor {
	t.Helper()
	m := Monitor{Name: "website", AccountName: "test", Type: "http", Target: "https://website.example/health",
		DNSType: "A", CFDomain: "website.example", CFZoneID: "zone", CFRecordID: "record",
		OriginalIP: "192.0.2.1", BackupIP: "192.0.2.2", Interval: 60, Timeout: 1,
		Retries: 3, SuccessThreshold: 2, PreferredIP: "192.0.2.1", Status: "Unknown"}
	if err := DB.Create(&m).Error; err != nil {
		t.Fatal(err)
	}
	return m
}

func loadTestMonitor(t *testing.T, id uint) Monitor {
	t.Helper()
	var m Monitor
	if err := DB.First(&m, id).Error; err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFailoverRetriesWithoutCommittingFailedDNS(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	dns := &memoryDNS{address: m.OriginalIP, failures: 1}
	engine.dns = dns
	engine.checker = probeFunc(func(_ context.Context, _ health.Spec, origin string) error {
		if origin == m.OriginalIP {
			return errors.New("HTTP 503")
		}
		return nil
	})
	for range 3 {
		engine.Check(m.ID)
	}
	saved := loadTestMonitor(t, m.ID)
	if saved.CurrentIP != m.OriginalIP || saved.FailCount != 3 || saved.Status != "Error" {
		t.Fatalf("unconfirmed failover changed state: %+v", saved)
	}
	var count int64
	DB.Model(&SwitchEvent{}).Count(&count)
	if count != 0 {
		t.Fatal("failed switch recorded a successful event")
	}
	engine.Check(m.ID)
	saved = loadTestMonitor(t, m.ID)
	if saved.CurrentIP != m.BackupIP || saved.Status != "Failover" || len(dns.switches) != 2 {
		t.Fatalf("failover did not retry: %+v, switches=%v", saved, dns.switches)
	}
	var event SwitchEvent
	DB.First(&event)
	if event.FromIP != m.OriginalIP || event.ToIP != m.BackupIP || event.Type != "auto" {
		t.Fatalf("incorrect switch history: %+v", event)
	}
}

func TestBothOriginsDownAndRecoveryFlapping(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	dns := &memoryDNS{address: m.OriginalIP}
	engine.dns = dns
	var primary, backup atomic.Bool
	engine.checker = probeFunc(func(_ context.Context, _ health.Spec, origin string) error {
		if origin == m.OriginalIP && primary.Load() || origin == m.BackupIP && backup.Load() {
			return nil
		}
		return errors.New("unhealthy")
	})
	for range 4 {
		engine.Check(m.ID)
	}
	if got := loadTestMonitor(t, m.ID); got.Status != "Down" || len(dns.switches) != 0 {
		t.Fatalf("switched to an unhealthy backup: %+v", got)
	}
	backup.Store(true)
	engine.Check(m.ID)
	if got := loadTestMonitor(t, m.ID); got.CurrentIP != m.BackupIP {
		t.Fatal("healthy backup was not selected")
	}
	primary.Store(true)
	engine.Check(m.ID)
	primary.Store(false)
	engine.Check(m.ID)
	primary.Store(true)
	engine.Check(m.ID)
	if got := loadTestMonitor(t, m.ID); got.CurrentIP != m.BackupIP || got.SuccCount != 1 {
		t.Fatal("nonconsecutive recovery counted")
	}
	engine.Check(m.ID)
	if got := loadTestMonitor(t, m.ID); got.CurrentIP != m.OriginalIP || got.Status != "Normal" {
		t.Fatal("primary recovery failed")
	}
}

func TestScheduledPreferenceRemainsProtected(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	dns := &memoryDNS{address: m.OriginalIP}
	engine.dns = dns
	var backupDown atomic.Bool
	engine.checker = probeFunc(func(_ context.Context, _ health.Spec, origin string) error {
		if origin == m.BackupIP && backupDown.Load() {
			return errors.New("backup unavailable")
		}
		return nil
	})
	if _, err := engine.Switch(context.Background(), m.ID, m.BackupIP, "scheduled", nil); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		engine.Check(m.ID)
	}
	if got := loadTestMonitor(t, m.ID); got.CurrentIP != m.BackupIP || got.PreferredIP != m.BackupIP {
		t.Fatal("scheduled preference was immediately undone")
	}
	backupDown.Store(true)
	for range 3 {
		engine.Check(m.ID)
	}
	if got := loadTestMonitor(t, m.ID); got.CurrentIP != m.OriginalIP {
		t.Fatal("scheduled active backup was not protected by failover")
	}
	backupDown.Store(false)
	for range 2 {
		engine.Check(m.ID)
	}
	if got := loadTestMonitor(t, m.ID); got.CurrentIP != m.BackupIP {
		t.Fatal("scheduled preference did not recover")
	}
}

func TestStartupReconcilesDNSInsteadOfTrustingLocalStatus(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	DB.Model(&m).Updates(map[string]any{"status": "Normal", "current_ip": m.OriginalIP})
	engine.dns = &memoryDNS{address: m.BackupIP}
	engine.checker = probeFunc(func(_ context.Context, _ health.Spec, origin string) error {
		if origin == m.OriginalIP {
			return errors.New("unavailable")
		}
		return nil
	})
	engine.Check(m.ID)
	if got := loadTestMonitor(t, m.ID); got.CurrentIP != m.BackupIP || got.Status != "Failover" {
		t.Fatalf("startup retained stale state: %+v", got)
	}
}

func TestConcurrentSwitchesAreSerialized(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	dns := &memoryDNS{address: m.OriginalIP}
	engine.dns = dns
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			target := m.OriginalIP
			if i%2 == 0 {
				target = m.BackupIP
			}
			if _, err := engine.Switch(context.Background(), m.ID, target, "scheduled", nil); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if dns.maxFlight.Load() != 1 {
		t.Fatalf("concurrent writes: %d", dns.maxFlight.Load())
	}
	if got := loadTestMonitor(t, m.ID); got.CurrentIP != dns.address {
		t.Fatal("DNS and stored state diverged")
	}
}

func TestSchedulerReloadDoesNotPostponeExistingChecks(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	if err := engine.Reload(); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	entry := engine.entries[m.ID][0]
	engine.mu.Unlock()
	if err := engine.Reload(); err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	unchanged := engine.entries[m.ID][0] == entry
	engine.mu.Unlock()
	if !unchanged {
		t.Fatal("unrelated reload restarted the monitoring interval")
	}
}

func TestStopCancelsInFlightProbes(t *testing.T) {
	testDatabase(t)
	createTestMonitor(t)
	started := make(chan struct{}, 2)
	engine.checker = probeFunc(func(ctx context.Context, _ health.Spec, _ string) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	if err := engine.Reload(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("initial check did not start")
	}
	stopped := make(chan struct{})
	go func() { engine.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel probes")
	}
}

func TestLegacyDuplicateMonitorsCannotFightOverDNS(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	duplicate := m
	duplicate.ID, duplicate.Name = 0, "legacy duplicate"
	if err := DB.Create(&duplicate).Error; err != nil {
		t.Fatal(err)
	}
	dns := &memoryDNS{address: m.OriginalIP}
	engine.dns = dns
	engine.Check(m.ID)
	engine.Check(duplicate.ID)
	if got := loadTestMonitor(t, m.ID); got.Status != "Error" {
		t.Fatal("conflicting monitor was not blocked")
	}
	if _, err := engine.Switch(context.Background(), duplicate.ID, m.BackupIP, "scheduled", nil); err == nil {
		t.Fatal("conflicting switch was accepted")
	}
	if len(dns.switches) != 0 {
		t.Fatal("conflicting monitors changed DNS")
	}
}

func TestRemovedScheduleCannotRunAnAlreadyQueuedSwitch(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	schedule := Schedule{MonitorID: m.ID, Cron: "0 */2 * * *", TargetIP: m.BackupIP}
	if err := DB.Create(&schedule).Error; err != nil {
		t.Fatal(err)
	}
	DB.Delete(&schedule)
	dns := &memoryDNS{address: m.OriginalIP}
	engine.dns = dns
	if _, err := engine.switchMonitor(context.Background(), m.ID, m.BackupIP, "scheduled", nil, schedule.ID); err == nil {
		t.Fatal("deleted schedule still executed")
	}
	if len(dns.switches) != 0 {
		t.Fatal("deleted schedule changed DNS")
	}
}
