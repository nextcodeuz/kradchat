package security

import (
	"testing"
	"time"
)

func TestBucket(t *testing.T) {
	b := NewBucket(3, 1) // 3 token, 1/s refill
	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("token %d should be allowed", i)
		}
	}
	if b.Allow() {
		t.Fatal("4th token should be denied")
	}
	time.Sleep(1100 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("after refill token should be allowed")
	}
}

func TestRateLimiterPerKey(t *testing.T) {
	r := NewRateLimiter(2, time.Minute)
	if !r.Allow("ip:1") || !r.Allow("ip:1") {
		t.Fatal("first two should pass")
	}
	if r.Allow("ip:1") {
		t.Fatal("third should be limited")
	}
	if !r.Allow("ip:2") {
		t.Fatal("different key should pass")
	}
}

func TestRateLimiterRefill(t *testing.T) {
	r := NewRateLimiter(10, time.Second)
	for i := 0; i < 10; i++ {
		r.Allow("k")
	}
	time.Sleep(1200 * time.Millisecond)
	if !r.Allow("k") {
		t.Fatal("should refill after window")
	}
}
