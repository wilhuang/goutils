package cache

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// NewCache 根据输入缓存长度生成AnyCache对象
func NewAnyCache[T comparable](maxCacheLen uint16, outTime time.Duration) *AnyCache[T] {
	if maxCacheLen < 3 {
		maxCacheLen = 3
	}
	return &AnyCache[T]{
		maxCacheLen: int(maxCacheLen),
		m:           make(map[T]any, maxCacheLen),
		outTimer:    make(map[T]*time.Timer),
		lastTime:    make(map[T]int64, maxCacheLen),
		outTime:     outTime,
	}
}

// 适用场景: 读多写少
// - 支持过期时间
// - 单个value自动扩容
// - 淘汰机制LRU
// - 锁机制：读写锁

// Cache 一个可并发、防击穿的LRU算法
// 最小长度缓存为1，达到缓存上限时，淘汰最久未访问的数据
// 直接使用时，自动使用最小缓存长度
type AnyCache[T comparable] struct {
	gmutex, lmutex sync.RWMutex
	tmutex         sync.Mutex
	outTimer       map[T]*time.Timer
	lastTime       map[T]int64
	m              map[T]any          // 数据
	g              singleflight.Group // 防击穿
	maxCacheLen    int
	outTime        time.Duration
}

// Delete 根据key主动删除缓存
func (c *AnyCache[T]) Delete(k T) {
	c.gmutex.Lock()
	delete(c.m, k)
	c.gmutex.Unlock()

	c.lmutex.Lock()
	delete(c.lastTime, k)
	c.lmutex.Unlock()
	go runtime.GC()
}

func (c *AnyCache[T]) Clear() {
	c.gmutex.Lock()
	for k := range c.m {
		delete(c.m, k)
	}
	c.gmutex.Unlock()
	c.lmutex.Lock()
	for k := range c.lastTime {
		delete(c.lastTime, k)
	}
	c.lmutex.Unlock()
	go runtime.GC()
}

func (c *AnyCache[T]) updateTime(k T) {
	c.lmutex.Lock()
	c.lastTime[k] = time.Now().Unix()
	c.lmutex.Unlock()
}

// Store 存储key-value数据
func (c *AnyCache[T]) Store(key T, data any) {
	var minKey T
	needOut := false
	c.lmutex.RLock()
	if _, ok := c.lastTime[key]; !ok {
		if len(c.lastTime) >= c.maxCacheLen {
			needOut = true
			minTime := time.Now().Unix()
			for k, v := range c.lastTime {
				if v < minTime {
					minKey = k
					minTime = v
				}
			}
		}
	}
	c.lmutex.RUnlock()

	c.gmutex.Lock()
	c.m[key] = data
	c.gmutex.Unlock()
	if c.outTime > 0 {
		go func(key T) {
			c.tmutex.Lock()
			if v, ok := c.outTimer[key]; ok {
				v.Reset(c.outTime / 2)
			} else {
				c.outTimer[key] = time.AfterFunc(c.outTime, func() {
					c.Delete(key)
				})
			}
			c.tmutex.Unlock()
		}(key)
	}
	if needOut {
		c.Delete(minKey)
	}
	go c.updateTime(key)
}

func (c *AnyCache[T]) Load(k T) (any, bool) {
	c.gmutex.RLock()
	defer c.gmutex.RUnlock()
	if v, ok := c.m[k]; ok {
		go c.updateTime(k)
		return v, true
	}
	return nil, false
}

// LoadOrStore 根据key读取数据，当没有数据时，根据输入的方法存储并返回数据
func (c *AnyCache[T]) LoadOrStore(k T, fu func() (any, error)) (any, error) {
	if v, ok := c.Load(k); ok {
		return v, nil
	}

	res, err, _ := c.g.Do(fmt.Sprint(k), fu)
	if err == nil {
		c.Store(k, res)
	}
	return res, err
}
