package keylock

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestIndependentKeysAndReclamation(t *testing.T) {
	var locks Locks
	first, ok := locks.TryLock(1)
	if !ok {
		t.Fatal("first lock failed")
	}
	if release, ok := locks.TryLock(1); ok {
		release()
		t.Fatal("duplicate lock succeeded")
	}
	second, ok := locks.TryLock(65)
	if !ok {
		t.Fatal("unrelated keys share a lock")
	}
	second()
	first()
	if len(locks.entries) != 0 {
		t.Fatal("idle locks leaked")
	}
}

func TestCancellationAndConcurrentAcquisition(t *testing.T) {
	var locks Locks
	var active atomic.Int32
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			for range 50 {
				unlock, err := locks.Lock(context.Background(), 1)
				if err != nil {
					t.Error(err)
					return
				}
				if active.Add(1) != 1 {
					t.Error("overlapping critical sections")
				}
				active.Add(-1)
				unlock()
			}
		})
	}
	group.Wait()
	unlock, _ := locks.Lock(context.Background(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := locks.Lock(ctx, 1); err == nil {
		release()
		t.Fatal("canceled wait succeeded")
	}
	unlock()
	if len(locks.entries) != 0 {
		t.Fatal("canceled waiter leaked a lock")
	}
}
