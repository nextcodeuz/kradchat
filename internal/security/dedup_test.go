package security

import (
	"testing"
	"time"
)

func TestDedupStore(t *testing.T) {
	d := NewDedupStore(time.Minute, 1000)
	if d.Seen("a") {
		t.Error("first Seen should be false")
	}
	if !d.Seen("a") {
		t.Error("second Seen should be true (duplicate)")
	}
	if d.Seen("b") {
		t.Error("different key should be false")
	}
}

func TestDedupStoreExpiry(t *testing.T) {
	d := NewDedupStore(50*time.Millisecond, 1000)
	_ = d.Seen("exp")
	time.Sleep(80 * time.Millisecond)
	if d.Seen("exp") {
		t.Error("expired key should be reusable")
	}
}

func TestDedupStoreMaxKeys(t *testing.T) {
	d := NewDedupStore(time.Minute, 10)
	for i := 0; i < 10; i++ {
		if d.Seen(string(rune('a' + i))) {
			t.Fatalf("key %d should be new", i)
		}
	}
	// maxKeys yetdi — fail-OPEN: eng eski yozuvlar evict qilinib, yangi legit
	// xabar qabul qilinadi (avval jimgina "duplicate" deb tashlanardi — yo'qotish).
	if d.Seen("overflow") {
		t.Error("over maxKeys should evict oldest and accept new keys (fail-open)")
	}
	// Evict qilingan eng eski kalit qayta ko'rilsa — endi yangi hisoblanadi.
	if !d.Seen("overflow") {
		t.Error("second overflow Seen should be true (duplicate)")
	}
}
