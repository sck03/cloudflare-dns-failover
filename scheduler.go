package main

import (
	"context"
	"log"
	"runtime/debug"
	"slices"
	"time"

	"github.com/robfig/cron/v3"
)

// Periodic checks and switch schedules have independent lifetimes. Editing one
// must not postpone the other; the plan owns both registrations and their inputs.
type monitorPlan struct {
	interval  int
	check     cron.EntryID
	schedules []Schedule
	switches  []cron.EntryID
}

func (e *Engine) Reload() error {
	var monitors []Monitor
	if err := e.db.WithContext(e.ctx).Select("id", "interval").Preload("Schedules").Find(&monitors).Error; err != nil {
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
		plan, existed := e.plans[id]
		if !existed {
			plan = &monitorPlan{}
			e.plans[id] = plan
		}
		if plan.interval != m.Interval {
			entry := e.cron.Schedule(cron.Every(time.Duration(m.Interval)*time.Second), cron.FuncJob(func() { e.enqueueCheck(id) }))
			e.cron.Remove(plan.check)
			plan.check, plan.interval = entry, m.Interval
		}
		if !slices.Equal(plan.schedules, m.Schedules) {
			for _, entry := range plan.switches {
				e.cron.Remove(entry)
			}
			plan.switches = nil
			for _, schedule := range m.Schedules {
				entry, err := e.cron.AddFunc(schedule.Cron, func() {
					if _, err := e.switchMonitor(e.ctx, id, schedule.TargetIP, "scheduled", nil, schedule.ID); err != nil && e.ctx.Err() == nil {
						log.Printf("Scheduled switch for monitor %d: %v", id, err)
					}
				})
				if err != nil {
					log.Printf("Invalid existing schedule for monitor %d: %v", id, err)
					continue
				}
				plan.switches = append(plan.switches, entry)
			}
			plan.schedules = m.Schedules
		}
		if !existed {
			e.enqueueCheckLocked(id)
		}
	}
	for id, plan := range e.plans {
		if !present[id] {
			e.cron.Remove(plan.check)
			for _, entry := range plan.switches {
				e.cron.Remove(entry)
			}
			delete(e.plans, id)
			delete(e.synced, id)
		}
	}
	if !e.started {
		if _, err := e.cron.AddFunc("@hourly", func() { pruneHistory(e.db.WithContext(e.ctx)) }); err != nil {
			return err
		}
		e.cron.Start()
		e.started = true
	}
	return nil
}

// enqueueCheck coalesces periodic checks into a queue of monitor IDs. A fixed
// worker pool drains it, so waiting monitors do not each retain a goroutine or
// a monitor lock. Manual switches continue to share the engine's operation slots.
func (e *Engine) enqueueCheck(id uint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enqueueCheckLocked(id)
}

func (e *Engine) enqueueCheckLocked(id uint) {
	if e.stopped || e.pending[id] {
		return
	}
	e.pending[id] = true
	e.queue = append(e.queue, id)
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) checkWorker() {
	defer e.workers.Done()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-e.wake:
		}
		for {
			e.mu.Lock()
			if e.stopped || len(e.queue) == 0 {
				e.mu.Unlock()
				break
			}
			id := e.queue[0]
			e.queue = e.queue[1:]
			if len(e.queue) == 0 {
				e.queue = nil
			}
			e.mu.Unlock()
			e.runQueuedCheck(id)
		}
	}
}

func (e *Engine) runQueuedCheck(id uint) {
	defer func() {
		e.mu.Lock()
		delete(e.pending, id)
		e.mu.Unlock()
		if err := recover(); err != nil {
			log.Printf("Monitor %d check panic: %v\n%s", id, err, debug.Stack())
		}
	}()
	e.Check(id)
}
