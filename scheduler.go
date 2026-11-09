package main

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"

	"github.com/robfig/cron/v3"
)

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
		plan := fmt.Sprintf("%d:%v", m.Interval, m.Schedules)
		if e.plans[id] == plan {
			continue
		}
		_, existed := e.entries[id]
		for _, entry := range e.entries[id] {
			e.cron.Remove(entry)
		}
		e.plans[id] = plan
		entry, err := e.cron.AddFunc(fmt.Sprintf("@every %ds", m.Interval), func() { e.enqueueCheck(id) })
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
			e.enqueueCheckLocked(id)
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
