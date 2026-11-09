package main

import (
	"log"
	"runtime/debug"
)

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
