package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cfguard/internal/failover"
	"cfguard/internal/health"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type HealthChecker interface {
	Check(context.Context, health.Spec, string) error
}

type DNSProvider interface {
	Read(context.Context, *Monitor) (*CloudflareRecord, error)
	Switch(context.Context, *Monitor, string) (string, error)
}

type DNSConfigurator interface {
	ConfigureTTL(context.Context, *Monitor, *CloudflareRecord) error
}

type cloudflareDNS struct{}

func (cloudflareDNS) Read(ctx context.Context, m *Monitor) (*CloudflareRecord, error) {
	return readDNSRecord(ctx, m)
}
func (cloudflareDNS) Switch(ctx context.Context, m *Monitor, target string) (string, error) {
	return updateDNS(ctx, m, target)
}

type Engine struct {
	db      *gorm.DB
	checker HealthChecker
	dns     DNSProvider
	notify  func(string)
	ctx     context.Context
	cancel  context.CancelFunc
	cron    *cron.Cron
	slots   chan struct{}
	locks   [64]chan struct{}
	mu      sync.Mutex
	entries map[uint][]cron.EntryID
	plans   map[uint]string
	synced  map[uint]time.Time
	started bool
	stopped bool
	initial sync.WaitGroup
}

var engine *Engine

func NewEngine(db *gorm.DB, checker HealthChecker, dns DNSProvider, concurrency int, notify func(string)) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		db: db, checker: checker, dns: dns, notify: notify, ctx: ctx, cancel: cancel,
		slots: make(chan struct{}, max(concurrency, 1)), entries: make(map[uint][]cron.EntryID),
		plans:  make(map[uint]string),
		synced: make(map[uint]time.Time),
		cron:   cron.New(cron.WithChain(cron.Recover(cron.DefaultLogger), cron.SkipIfStillRunning(cron.DefaultLogger))),
	}
	for i := range e.locks {
		e.locks[i] = make(chan struct{}, 1)
	}
	return e
}

// Lock serializes checks, scheduled/manual switches and configuration mutations.
// A fixed set of stripes bounds lock memory even when monitors are repeatedly deleted.
func (e *Engine) Lock(ctx context.Context, id uint) (func(), error) {
	lock := e.locks[id%uint(len(e.locks))]
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.ctx.Done():
		return nil, e.ctx.Err()
	}
}

func (e *Engine) Invalidate(id uint) {
	e.mu.Lock()
	delete(e.synced, id)
	e.mu.Unlock()
}

func (e *Engine) Reload() error {
	var monitors []Monitor
	if err := e.db.Preload("Schedules").Find(&monitors).Error; err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return context.Canceled
	}
	present := make(map[uint]bool, len(monitors))
	for _, m := range monitors {
		m.ApplyDefaults()
		id := m.ID
		present[id] = true
		plan := fmt.Sprintf("%d:%v", m.Interval, m.Schedules)
		if e.plans[id] == plan {
			continue
		}
		_, existed := e.entries[id]
		for _, entry := range e.entries[id] {
			e.cron.Remove(entry)
		}
		e.plans[id] = plan
		entry, err := e.cron.AddFunc(fmt.Sprintf("@every %ds", m.Interval), func() { e.Check(id) })
		if err != nil {
			return err
		}
		e.entries[id] = []cron.EntryID{entry}
		for _, schedule := range m.Schedules {
			target := schedule.TargetIP
			scheduleID := schedule.ID
			entry, err := e.cron.AddFunc(schedule.Cron, func() {
				if _, err := e.switchMonitor(e.ctx, id, target, "scheduled", nil, scheduleID); err != nil && e.ctx.Err() == nil {
					log.Printf("Scheduled switch for monitor %d: %v", id, err)
				}
			})
			if err != nil {
				log.Printf("Invalid existing schedule for monitor %d: %v", id, err)
				continue
			}
			e.entries[id] = append(e.entries[id], entry)
		}
		if !existed {
			e.initial.Add(1)
			go func() {
				defer e.initial.Done()
				e.Check(id)
			}()
		}
	}
	for id, entries := range e.entries {
		if !present[id] {
			for _, entry := range entries {
				e.cron.Remove(entry)
			}
			delete(e.entries, id)
			delete(e.plans, id)
			delete(e.synced, id)
		}
	}
	if !e.started {
		if _, err := e.cron.AddFunc("@hourly", pruneHistory); err != nil {
			return err
		}
		e.cron.Start()
		e.started = true
	}
	return nil
}

func (e *Engine) Stop() {
	e.mu.Lock()
	e.stopped = true
	e.cancel()
	done := e.cron.Stop()
	e.mu.Unlock()
	<-done.Done()
	e.initial.Wait()
}

func (e *Engine) takeSlot(ctx context.Context) (func(), error) {
	select {
	case e.slots <- struct{}{}:
		return func() { <-e.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.ctx.Done():
		return nil, e.ctx.Err()
	}
}

func (e *Engine) Check(id uint) {
	lock := e.locks[id%uint(len(e.locks))]
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	default:
		return
	}
	release, err := e.takeSlot(e.ctx)
	if err != nil {
		return
	}
	defer release()
	var m Monitor
	if err := e.db.First(&m, id).Error; err != nil {
		return
	}
	oldStatus, oldError := m.Status, m.LastError
	err = m.Validate()
	if err == nil {
		err = checkDNSConflict(e.db, &m)
	}
	if err != nil {
		m.LastError, m.Status = err.Error(), "Error"
		m.FailCount, m.SuccCount = 0, 0
		e.persist(&m, oldStatus, oldError)
		return
	}
	results := make(chan error, 1)
	go func() { results <- e.checker.Check(e.ctx, m.healthSpec(), m.BackupIP) }()
	primaryErr := e.checker.Check(e.ctx, m.healthSpec(), m.OriginalIP)
	backupErr := <-results
	if e.ctx.Err() != nil {
		return
	}
	primaryUp, backupUp := primaryErr == nil, backupErr == nil
	m.PrimaryHealthy, m.BackupHealthy = &primaryUp, &backupUp
	m.PrimaryError, m.BackupError = errorText(primaryErr), errorText(backupErr)
	m.LastCheck, m.LastError = time.Now(), ""

	e.mu.Lock()
	lastSync := e.synced[id]
	e.mu.Unlock()
	if m.CurrentIP == "" || time.Since(lastSync) >= 5*time.Minute {
		ctx, cancel := context.WithTimeout(e.ctx, 20*time.Second)
		record, err := e.dns.Read(ctx, &m)
		if err != nil {
			m.LastError = "DNS reconciliation: " + err.Error()
		} else {
			switch {
			case sameAddress(record.Content, m.OriginalIP):
				m.CurrentIP = m.OriginalIP
			case sameAddress(record.Content, m.BackupIP):
				m.CurrentIP = m.BackupIP
			default:
				m.CurrentIP = record.Content
				m.LastError = "DNS address is outside the configured primary/backup pair"
			}
			if m.LastError == "" {
				activeUp := m.CurrentIP == m.OriginalIP && primaryUp || m.CurrentIP == m.BackupIP && backupUp
				if configurable, ok := e.dns.(DNSConfigurator); ok && activeUp {
					if err := configurable.ConfigureTTL(ctx, &m, record); err != nil {
						m.LastError = "DNS TTL configuration: " + err.Error()
					}
				}
			}
			if m.LastError == "" {
				e.mu.Lock()
				e.synced[id] = time.Now()
				e.mu.Unlock()
			}
		}
		cancel()
	}
	if m.LastError == "" {
		state, target := failover.Decide(failover.Policy{
			Primary: m.OriginalIP, Backup: m.BackupIP,
			FailureThreshold: m.Retries, RecoveryThreshold: m.SuccessThreshold,
		}, failover.State{
			Current: m.CurrentIP, Preferred: m.PreferredIP, Failures: m.FailCount, Successes: m.SuccCount,
		}, primaryUp, backupUp)
		m.FailCount, m.SuccCount, m.PreferredIP = state.Failures, state.Successes, state.Preferred
		if target != "" {
			if err := e.applySwitch(e.ctx, &m, target, "auto"); err == nil {
				return
			} else {
				m.LastError = err.Error()
			}
		}
	}
	m.Status = monitorStatus(&m)
	e.persist(&m, oldStatus, oldError)
}

func (e *Engine) persist(m *Monitor, oldStatus, oldError string) {
	if err := saveRuntime(e.db, m); err != nil {
		e.Invalidate(m.ID)
		log.Printf("Save monitor %d state: %v", m.ID, err)
		return
	}
	if (m.Status == "Down" || m.Status == "Error") && (m.Status != oldStatus || m.LastError != oldError) {
		e.notify(fmt.Sprintf("监控 %s: %s %s", m.Name, m.Status, m.LastError))
	}
}

func (e *Engine) Switch(ctx context.Context, id uint, target, kind string, proxied *bool) (*Monitor, error) {
	return e.switchMonitor(ctx, id, target, kind, proxied, 0)
}

func (e *Engine) switchMonitor(ctx context.Context, id uint, target, kind string, proxied *bool, scheduleID uint) (*Monitor, error) {
	unlock, err := e.Lock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	release, err := e.takeSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	var m Monitor
	if err := e.db.First(&m, id).Error; err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if err := checkDNSConflict(e.db, &m); err != nil {
		return nil, err
	}
	if scheduleID != 0 {
		var schedule Schedule
		if err := e.db.Where("id = ? AND monitor_id = ? AND target_ip = ?", scheduleID, id, target).First(&schedule).Error; err != nil {
			return nil, fmt.Errorf("schedule was removed or changed: %w", err)
		}
	}
	if kind == "manual" {
		target = m.OriginalIP
		if proxied != nil {
			m.OriginalIPCDNEnabled = *proxied
		}
	}
	if target != m.OriginalIP && target != m.BackupIP {
		return nil, fmt.Errorf("switch target must equal primary or backup")
	}
	if err := e.checker.Check(ctx, m.healthSpec(), target); err != nil {
		return nil, fmt.Errorf("target %s is unhealthy: %w", target, err)
	}
	up := true
	if target == m.OriginalIP {
		m.PrimaryHealthy, m.PrimaryError = &up, ""
	} else {
		m.BackupHealthy, m.BackupError = &up, ""
	}
	// A successful scheduled/manual switch becomes the preferred backend;
	// automatic checks can fail over and later recover to that preference.
	m.PreferredIP = target
	if err := e.applySwitch(ctx, &m, target, kind); err != nil {
		return nil, err
	}
	return &m, nil
}

func (e *Engine) applySwitch(ctx context.Context, m *Monitor, target, kind string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	from, err := e.dns.Switch(ctx, m, target)
	if err != nil {
		e.Invalidate(m.ID)
		return fmt.Errorf("DNS switch failed: %w", err)
	}
	next := *m
	next.CurrentIP, next.LastError = target, ""
	next.FailCount, next.SuccCount = 0, 0
	if !sameAddress(from, target) {
		next.LastSwitch = time.Now()
	}
	next.Status = monitorStatus(&next)
	if err := saveSwitch(e.db, &next, from, kind); err != nil {
		e.Invalidate(m.ID)
		return fmt.Errorf("DNS changed but local state could not be saved; reconciliation required: %w", err)
	}
	*m = next
	e.mu.Lock()
	e.synced[m.ID] = time.Now()
	e.mu.Unlock()
	if !sameAddress(from, target) {
		e.notify(fmt.Sprintf("%s: %s → %s (%s)", m.Name, from, target, kind))
	}
	return nil
}

func monitorStatus(m *Monitor) string {
	if m.LastError != "" {
		return "Error"
	}
	if m.PrimaryHealthy == nil || m.BackupHealthy == nil {
		return "Unknown"
	}
	primary, backup := *m.PrimaryHealthy, *m.BackupHealthy
	if !primary && !backup {
		return "Down"
	}
	activeUp := m.CurrentIP == m.OriginalIP && primary || m.CurrentIP == m.BackupIP && backup
	if !activeUp {
		return "Degraded"
	}
	if m.CurrentIP != m.PreferredIP {
		return "Failover"
	}
	if !primary || !backup {
		return "Degraded"
	}
	return "Normal"
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
