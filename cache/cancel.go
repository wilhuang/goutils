package cache

import (
	"context"
	"sync"
)

// 用于超时任务操作集
type CancelMgr[T comparable] struct {
	m  map[T]context.CancelFunc
	mu sync.RWMutex
}

func NewCancelMgr[T comparable]() *CancelMgr[T] {
	return &CancelMgr[T]{
		m: make(map[T]context.CancelFunc),
	}
}

func (i *CancelMgr[T]) Cancel(key T) {
	i.mu.RLock()
	cancel, ok := i.m[key]
	i.mu.RUnlock()
	if !ok {
		return
	}
	i.mu.Lock()
	delete(i.m, key)
	i.mu.Unlock()
	cancel()
}

func (i *CancelMgr[T]) Store(key T, value context.CancelFunc) {
	i.mu.Lock()
	i.m[key] = value
	i.mu.Unlock()
}
