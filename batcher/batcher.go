package batcher

import (
	"context"
	"sync"
	"time"
)

// Batcher batches messages
type Batcher[T any] interface {
	Add(message T)
}

func New[T any](ctx context.Context, limit int, dur time.Duration, do func([]T)) Batcher[T] {
	b := batcher[T]{mu: sync.Mutex{}, vals: make([]T, 0), ch: make(chan struct{})}
	go func() {
		defer close(b.ch)
		send := func() {
			tmp := append([]T(nil), b.vals...)
			do(tmp)
			b.vals = b.vals[:0]
		}
		ticker := time.Tick(dur)
		for {
			select {
			case <-ctx.Done():
				return
			case _ = <-ticker:
				b.mu.Lock()
				if len(b.vals) > 0 {
					send()
				}
				b.mu.Unlock()
			case _ = <-b.ch:
				b.mu.Lock()
				if len(b.vals) >= limit {
					send()
				}
				b.mu.Unlock()
			}
		}
	}()
	return &b
}

type batcher[T any] struct {
	mu   sync.Mutex
	vals []T
	ch   chan struct{}
}

func (b *batcher[T]) Add(item T) {
	b.mu.Lock()
	b.vals = append(b.vals, item)
	b.mu.Unlock()
	b.ch <- struct{}{}
}
