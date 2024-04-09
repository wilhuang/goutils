package cache

import (
	"sync"
)

// 参考代码库 "github.com/deckarep/golang-set/v2"

// 可重复插入的Set，一般用于作为任务锁
type MulSet[T comparable] struct {
	sync.RWMutex
	m map[T]uint32
}

func NewMulSet[T comparable]() *MulSet[T] {
	return &MulSet[T]{
		m: make(map[T]uint32),
	}
}

func (s *MulSet[T]) Add(k T) {
	s.Lock()
	v, ok := s.m[k]
	if !ok {
		v = 0
	}
	s.m[k] = v + 1
	s.Unlock()
}

func (s *MulSet[T]) Contains(k T) bool {
	s.RLock()
	_, ok := s.m[k]
	s.RUnlock()
	return ok
}

func (s *MulSet[T]) Remove(k T) {
	s.Lock()
	if v, ok := s.m[k]; ok {
		if v <= 1 {
			delete(s.m, k)
		} else {
			s.m[k] = v - 1
		}
	}
	s.Unlock()
}
