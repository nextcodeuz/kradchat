package service

import (
	"strings"
	"sync"
	"time"
)

// --- Pending 2FA sessionlar (in-memory, TTL) ---
// Login (2FA) va TwoFAVerify orasidagi qisqa muddatli holat.
// Multi-instance rejimda markaziy store (Redis) kerak — hozircha single-node.

var (
	pendingMu      sync.Mutex
	pendingStore   = make(map[string]pendingEntry)
	maxPendingSize = 5000
)

type pendingEntry struct {
	UserID    string
	DeviceID  string
	IP        string
	UserAgent string
	ExpiresAt time.Time
	Attempts  int
}

func (a *AuthService) storePending(userID, deviceID, ip, userAgent, pendingID string) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	// Eskirgan va haddan tashqari ko'paygan yozuvlarni tozalaymiz.
	now := time.Now()
	for k, e := range pendingStore {
		if now.After(e.ExpiresAt) {
			delete(pendingStore, k)
		}
	}
	if len(pendingStore) >= maxPendingSize {
		// Eng eski (eng erta tugaydigan) kalitni o'chiramiz — tasodifiy emas.
		var oldest string
		var oldestExp time.Time
		first := true
		for k, e := range pendingStore {
			if first || e.ExpiresAt.Before(oldestExp) {
				oldest, oldestExp, first = k, e.ExpiresAt, false
			}
		}
		if oldest != "" {
			delete(pendingStore, oldest)
		}
	}
	pendingStore[pendingID] = pendingEntry{
		UserID:    userID,
		DeviceID:  deviceID,
		IP:        ip,
		UserAgent: userAgent,
		ExpiresAt: now.Add(a.cfg.Security.PendingSessionTTL),
	}
}

// peekPending — pending 2FA sessiyasini o'qiydi, lekin O'CHIRMAYDI.
// Noto'g'ri kod kiritilganda sessiya saqlanib qoladi, shunda kodni qayta
// kiritish mumkin. (Eski xato: har bir urinish sessiyani iste'mol qilar edi,
// shuning uchun bitta xato koddan keyin "invalid token" bilan to'xtab qolardi.)
func (a *AuthService) peekPending(pendingID string) (pendingEntry, bool) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	e, ok := pendingStore[pendingID]
	if !ok {
		return pendingEntry{}, false
	}
	if time.Now().After(e.ExpiresAt) {
		delete(pendingStore, pendingID)
		return pendingEntry{}, false
	}
	return e, true
}

func (a *AuthService) deletePending(pendingID string) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	delete(pendingStore, pendingID)
}

// incPendingAttempts — noto'g'ri kod urinishlari sonini oshiradi, yangi sonni
// qaytaradi (brute-force himoyasi uchun).
func (a *AuthService) incPendingAttempts(pendingID string) int {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	e, ok := pendingStore[pendingID]
	if !ok {
		return 0
	}
	e.Attempts++
	pendingStore[pendingID] = e
	return e.Attempts
}

// --- Per-user 2FA himoya (pending'dan mustaqil) ---
// Hujumchi to'g'ri parol bilan har safar yangi pendingID olib, pending
// başına 5-urinish limitini chetlab o'tardi (login→5 kod→login→5 kod...).
// Shuning uchun muvaffaqiyatsiz urinishlar userID bo'yicha ham sanaladi.
var (
	user2FAMu    sync.Mutex
	user2FAFails = make(map[string]userFailEntry)

	usedTOTPMu sync.Mutex
	usedTOTP   = make(map[string]time.Time) // key: userID+"\x00"+code → eskirishi
)

type userFailEntry struct {
	Count     int
	ExpiresAt time.Time
}

// maxUser2FAFails — bitta akkaunt uchun TTL ichida ruxsat etilgan jami
// noto'g'ri 2FA urinish. 6 xonali kod (1M variant) uchun bu nolga teng,
// legitim yanglishishlarga yetadi.
const maxUser2FAFails = 20

var user2FAFailTTL = 15 * time.Minute

// usedTOTPRemember — ishlatilgan kodni skew oynasidan biroz uzun eslab qolish
// (±1 step = 90 sek validlik + zaxira).
var usedTOTPRemember = 100 * time.Second

// noteUser2FAFail — muvaffaqiyatsiz urinishni user bo'yicha qayd qiladi.
// Limit to'lgan bo'lsa false qaytaradi (bloklash kerak).
func (a *AuthService) noteUser2FAFail(userID string) bool {
	now := time.Now()
	user2FAMu.Lock()
	defer user2FAMu.Unlock()
	e, ok := user2FAFails[userID]
	if !ok || now.After(e.ExpiresAt) {
		e = userFailEntry{ExpiresAt: now.Add(user2FAFailTTL)}
	}
	e.Count++
	user2FAFails[userID] = e
	// Eskirgan yozuvlarni vaqti-vaqti bilan tozalaymiz.
	if len(user2FAFails)%64 == 0 {
		for k, v := range user2FAFails {
			if now.After(v.ExpiresAt) {
				delete(user2FAFails, k)
			}
		}
	}
	return e.Count <= maxUser2FAFails
}

// resetUser2FAFail — muvaffaqiyatli verify'da hisobni tozalaydi.
func (a *AuthService) resetUser2FAFail(userID string) {
	user2FAMu.Lock()
	defer user2FAMu.Unlock()
	delete(user2FAFails, userID)
}

// checkTOTPReplay — kod shu oyna ichida ishlatilgan bo'lsa true
// (tutib olingan/kutilgan kodni yangi pending bilan qayta ishlatish).
func (a *AuthService) checkTOTPReplay(userID, code string) bool {
	code = strings.TrimSpace(code)
	usedTOTPMu.Lock()
	defer usedTOTPMu.Unlock()
	exp, ok := usedTOTP[userID+"\x00"+code]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(usedTOTP, userID+"\x00"+code)
		return false
	}
	return true
}

// markTOTPUsed — muvaffaqiyatli kodni replay'dan himoyalash uchun eslab qoladi.
func (a *AuthService) markTOTPUsed(userID, code string) {
	code = strings.TrimSpace(code)
	now := time.Now()
	usedTOTPMu.Lock()
	defer usedTOTPMu.Unlock()
	usedTOTP[userID+"\x00"+code] = now.Add(usedTOTPRemember)
	if len(usedTOTP)%64 == 0 {
		for k, exp := range usedTOTP {
			if now.After(exp) {
				delete(usedTOTP, k)
			}
		}
	}
}
