package cache

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// NewCache 根据输入缓存长度生成Cache对象
func NewCache[T1, T2 comparable](maxCacheLen uint16, outTime time.Duration) *Cache[T1, T2] {
	if maxCacheLen < 3 {
		maxCacheLen = 3
	}
	return &Cache[T1, T2]{
		maxCacheLen: int(maxCacheLen),
		m:           make(map[T1]T2, maxCacheLen),
		outTimer:    make(map[T1]*time.Timer),
		lastTime:    make(map[T1]int64, maxCacheLen),
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
type Cache[T1, T2 comparable] struct {
	gmutex, lmutex sync.RWMutex
	tmutex         sync.Mutex
	outTimer       map[T1]*time.Timer
	lastTime       map[T1]int64
	m              map[T1]T2          // 数据
	g              singleflight.Group // 防击穿
	maxCacheLen    int
	outTime        time.Duration
}

// Delete 根据key主动删除缓存
func (c *Cache[T1, T2]) Delete(k T1) {
	c.gmutex.Lock()
	delete(c.m, k)
	c.gmutex.Unlock()

	c.lmutex.Lock()
	delete(c.lastTime, k)
	c.lmutex.Unlock()
	go runtime.GC()
}

func (c *Cache[T1, T2]) Clear() {
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

func (c *Cache[T1, T2]) updateTime(k T1) {
	c.lmutex.Lock()
	c.lastTime[k] = time.Now().Unix()
	c.lmutex.Unlock()
}

// Store 存储key-value数据
func (c *Cache[T1, T2]) Store(key T1, data T2) {
	var minKey T1
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
		go func(key T1) {
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

func (c *Cache[T1, T2]) Load(k T1) (T2, bool) {
	c.gmutex.RLock()
	defer c.gmutex.RUnlock()
	v, ok := c.m[k]
	if ok {
		go c.updateTime(k)
	}
	return v, ok
}

// LoadOrStore 根据key读取数据，当没有数据时，根据输入的方法存储并返回数据
func (c *Cache[T1, T2]) LoadOrStore(k T1, fu func() (T2, error)) (T2, error) {
	if v, ok := c.Load(k); ok {
		return v, nil
	}

	res, err, _ := c.g.Do(fmt.Sprint(k), func() (interface{}, error) {
		return fu()
	})
	v, ok := res.(T2)
	if ok && err == nil {
		c.Store(k, v)
	}
	return v, err
}
