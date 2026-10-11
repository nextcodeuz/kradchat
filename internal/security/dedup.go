package security

import (
	"sync"
	"time"
)

// DedupStore client_msg_id asosida xabar takrorlanishini bloklaydi.
// Kirish: Xabar yuborilganda key qo'shiladi; qisqa TTLdan keyin tozalanadi.
type DedupStore struct {
	mu   sync.Mutex
	keys map[string]time.Time
	ttl  time.Duration
	// maxKeys — xotira chegarasi (DoS himoya).
	maxKeys int
	// calls — prune chastotasi uchun hisoblagich (har xabarda full-scan yo'q).
	calls     uint64
	lastPrune time.Time
}

func NewDedupStore(ttl time.Duration, maxKeys int) *DedupStore {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if maxKeys <= 0 {
		maxKeys = 100000
	}
	return &DedupStore{keys: make(map[string]time.Time), ttl: ttl, maxKeys: maxKeys}
}

// Seen qaytaradi: true agar kalit avval ko'rilgan bo'lsa (dublikat).
// Tozalash har chaqiruvda emas — har 64 chaqiruvda yoki 30s'da bir (CPU DoS oldi).
// To'lganda fail-OPEN: eng eski ~128 yozuv o'chiriladi, yangi legit xabar
// qabul qilinadi (avval "duplicate" deb jimgina tashlanardi — yo'qotish).
func (d *DedupStore) Seen(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	d.calls++
	if len(d.keys) >= pruneThreshold && (d.calls%64 == 0 || now.Sub(d.lastPrune) > 30*time.Second) {
		d.pruneLocked(now)
		d.lastPrune = now
	}
	if t, ok := d.keys[key]; ok {
		if now.Sub(t) < d.ttl {
			return true
		}
		// Kalit muddati o'tgan — qayta foydalanish mumkin.
		delete(d.keys, key)
	}
	if len(d.keys) >= d.maxKeys {
		d.evictOldestLocked(128)
	}
	d.keys[key] = now
	return false
}

// pruneThreshold — global tozalash boshlanadigan kalitlar soni.
const pruneThreshold = 512

// evictOldestLocked — eng eski n yozuvni o'chiradi (fail-open uchun).
func (d *DedupStore) evictOldestLocked(n int) {
	if len(d.keys) == 0 {
		return
	}
	// Birinchi o'tishda eskirganlarni o'chiramiz (arzon).
	cutoff := time.Now().Add(-d.ttl)
	removed := 0
	for k, t := range d.keys {
		if t.Before(cutoff) {
			delete(d.keys, k)
			if removed++; removed >= n {
				return
			}
		}
	}
	// Hali joy bo'lmasa — eng eski n tasini topib o'chiramiz.
	for removed < n && len(d.keys) > 0 {
		var oldest string
		var oldestT time.Time
		first := true
		for k, t := range d.keys {
			if first || t.Before(oldestT) {
				oldest, oldestT, first = k, t, false
			}
		}
		if oldest == "" {
			return
		}
		delete(d.keys, oldest)
		removed++
		if n > 512 {
			break // himoya: bitta Seen'da cheksiz sikl yo'q
		}
	}
}

func (d *DedupStore) pruneLocked(now time.Time) {
	if len(d.keys) < pruneThreshold {
		return
	}
	cutoff := now.Add(-d.ttl)
	for k, t := range d.keys {
		if t.Before(cutoff) {
			delete(d.keys, k)
		}
	}
}
