package security

import (
	"sync"
	"time"
)

// Bucket — token bucket rate limiter (in-memory).
type Bucket struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	refill float64 // tokens per second
	last   time.Time
}

func NewBucket(max float64, refillPerSec float64) *Bucket {
	return &Bucket{tokens: max, max: max, refill: refillPerSec, last: time.Now()}
}

// Allow 1 token sarflaydi; yetarli bo'lmasa false.
func (b *Bucket) Allow() bool {
	return b.AllowN(1)
}

func (b *Bucket) AllowN(n float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.refill
	b.last = now
	if b.tokens > b.max {
		b.tokens = b.max
	}
	if b.tokens >= n {
		b.tokens -= n
		return true
	}
	return false
}

// RateLimiter — per-key (IP, userID) limiter.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*Bucket
	max     float64
	refill  float64
	// maxKeys xotira chegarasi.
	maxKeys int
	// gcEvery — tozalash intervali.
	gcEvery time.Duration
}

func NewRateLimiter(maxPerWindow float64, window time.Duration) *RateLimiter {
	if window <= 0 {
		window = time.Minute
	}
	refill := maxPerWindow / window.Seconds()
	return &RateLimiter{
		buckets: make(map[string]*Bucket),
		max:     maxPerWindow,
		refill:  refill,
		maxKeys: 100000,
		gcEvery: window,
	}
}

// NewRateLimiterBurst — token bucket burst alohida sozlanadi.
// burst<=0 yoki burst>max bo'lsa max ishlatiladi. WS_MSG_BURST=10 kabi
// kichik burst bilan 60 msg/min rejimi to'g'ri ishlaydi (avval burst=max=60 edi).
func NewRateLimiterBurst(maxPerWindow float64, window time.Duration, burst float64) *RateLimiter {
	rl := NewRateLimiter(maxPerWindow, window)
	if burst > 0 && burst < maxPerWindow {
		rl.max = burst
	}
	return rl
}

func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	b, ok := r.buckets[key]
	if !ok {
		if len(r.buckets) >= r.maxKeys {
			// Fail-closed o'rniga eng uzoq bo'sh turgan bucket'ni chiqaramiz
			// (F10: limit to'lganda yangi user/IP'lar DoS bo'lmasligi uchun).
			r.evictOldestLocked()
		}
		b = NewBucket(r.max, r.refill)
		r.buckets[key] = b
	}
	r.mu.Unlock()
	return b.Allow()
}

// evictOldestLocked — eng uzoq faolsiz bucket'ni o'chiradi (r.mu ushlangan).
func (r *RateLimiter) evictOldestLocked() {
	var oldest string
	var oldestT time.Time
	first := true
	for k, b := range r.buckets {
		b.mu.Lock()
		last := b.last
		b.mu.Unlock()
		if first || last.Before(oldestT) {
			oldest, oldestT, first = k, last, false
		}
	}
	if oldest != "" {
		delete(r.buckets, oldest)
	}
}

// GCSweep faqat API handler oqimida chaqiriladi (intervalsiz thread-safe).
func (r *RateLimiter) GCSweep() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for k, b := range r.buckets {
		b.mu.Lock()
		idle := now.Sub(b.last)
		b.mu.Unlock()
		if idle > r.gcEvery*2 {
			delete(r.buckets, k)
		}
	}
}
