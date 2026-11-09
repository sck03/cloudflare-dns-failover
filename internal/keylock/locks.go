// Package keylock serializes work by key, retaining only active locks and waiters.
package keylock

import (
	"context"
	"sync"
)

type entry struct {
	token chan struct{}
	refs  int
}

// Locks has a usable zero value. Each successful acquisition must be released once.
type Locks struct {
	mu      sync.Mutex
	entries map[uint]*entry
}

func (l *Locks) retain(key uint) *entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[uint]*entry)
	}
	e := l.entries[key]
	if e == nil {
		e = &entry{token: make(chan struct{}, 1)}
		l.entries[key] = e
	}
	e.refs++
	return e
}

func (l *Locks) drop(key uint, e *entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.refs--
	if e.refs == 0 {
		delete(l.entries, key)
	}
}

func (l *Locks) release(key uint, e *entry) func() {
	return func() {
		<-e.token
		l.drop(key, e)
	}
}

func (l *Locks) Lock(ctx context.Context, key uint) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e := l.retain(key)
	select {
	case e.token <- struct{}{}:
		unlock := l.release(key, e)
		if err := ctx.Err(); err != nil {
			unlock()
			return nil, err
		}
		return unlock, nil
	case <-ctx.Done():
		l.drop(key, e)
		return nil, ctx.Err()
	}
}

// TryLock skips duplicate periodic checks without blocking unrelated keys.
func (l *Locks) TryLock(key uint) (func(), bool) {
	e := l.retain(key)
	select {
	case e.token <- struct{}{}:
		return l.release(key, e), true
	default:
		l.drop(key, e)
		return nil, false
	}
}
