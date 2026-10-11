package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/repository"
	"kradchat/internal/ws"
)

// PresenceService — online holati va user snapshot cache'ini boshqaradi.
type PresenceService struct {
	cfg  *config.Config
	repo *repository.Repositories
	hub  *ws.Hub

	mu        sync.RWMutex
	snapshots map[string]*ws.UserPayload

	// blocks — presence e'lonlarida blok filtrasi uchun (nil bo'lsa filtrsiz).
	blocks *BlockService

	// showCache — show_online_status kesh (60s TTL): har connect/announce'da
	// DB o'qimaslik uchun. UpdateMe prefn'i o'zgartirganda invalidatsiya qilinadi.
	showMu    sync.RWMutex
	showCache map[string]showEntry

	// Presence e'lonlarini batchelab yuborish (massiv ulanishda O(N²)->O(N)).
	// Bir vaqtda minglab kishi kirganda har biri uchun hammaga alohida xabar
	// yuborilsa, ulanish soniga qarab portlash — chat qotib qolardi.
	pending   map[string]*ws.UserPayload
	flushCh   chan struct{}
	stopCh    chan struct{}
	stopOnce  sync.Once
	flushTick time.Duration
}

func NewPresenceService(cfg *config.Config, repo *repository.Repositories, hub *ws.Hub) *PresenceService {
	return &PresenceService{
		cfg:       cfg,
		repo:      repo,
		hub:       hub,
		snapshots: make(map[string]*ws.UserPayload),
		pending:   make(map[string]*ws.UserPayload),
		showCache: make(map[string]showEntry),
		flushCh:   make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		flushTick: 150 * time.Millisecond,
	}
}

// SetBlocks — blok filtrini ulaydi (service.New'da presence'dan keyin
// yaratiladi, shuning uchun setter orqali).
func (p *PresenceService) SetBlocks(b *BlockService) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blocks = b
}

type showEntry struct {
	show bool
	exp  time.Time
}

// showOnlineTTL — pref kesh muddati.
var showOnlineTTL = time.Minute

// showOnline — user online holatni ko'rsatishga ruxsat berganmi.
// DB xatoda fail-closed (false): maxfiylik sizmasligi kerak.
func (p *PresenceService) showOnline(userID string) bool {
	now := time.Now()
	p.showMu.RLock()
	if e, ok := p.showCache[userID]; ok && now.Before(e.exp) {
		show := e.show
		p.showMu.RUnlock()
		return show
	}
	p.showMu.RUnlock()
	show := false
	if p.repo != nil {
		if u, err := p.repo.Users.ByID(context.Background(), userID); err == nil && u != nil {
			show = u.ShowOnlineStatus
		}
	}
	p.showMu.Lock()
	p.showCache[userID] = showEntry{show: show, exp: now.Add(showOnlineTTL)}
	// Kesh chegarasi — eng eski yozuvlarni tozalaymiz.
	if len(p.showCache) > 10000 {
		for k, v := range p.showCache {
			if now.After(v.exp) {
				delete(p.showCache, k)
			}
		}
	}
	p.showMu.Unlock()
	return show
}

// InvalidateShowOnline — UpdateMe prefn'i o'zgartirganda kesh tozalanadi.
func (p *PresenceService) InvalidateShowOnline(userID string) {
	p.showMu.Lock()
	defer p.showMu.Unlock()
	delete(p.showCache, userID)
}

// Start — presence flusher'ni ishga tushiradi (service.Start orqali).
func (p *PresenceService) Start() {
	go p.flusher()
}

// Stop — flusher'ni to'xtatadi (idempotent: ikki marta chaqirish xavfsiz).
func (p *PresenceService) Stop() {
	p.stopOnce.Do(func() { close(p.stopCh) })
}

// MarkOnline — client ulanganda user holatini yangilaydi.
// Agar birinchi qurilma bo'lsa — hammaga online deb e'lon qilinadi.
func (p *PresenceService) MarkOnline(c *ws.Client) {
	deviceCount := p.hub.UserDeviceCount(c.UserID)
	payload := p.snapshot(c.UserID)
	if payload == nil {
		payload = &ws.UserPayload{
			ID:          c.UserID,
			Username:    c.Username,
			DisplayName: c.DisplayName,
			AvatarURL:   c.AvatarURL,
			Verified:    c.Verified,
		}
	}
	wasOnline := false
	p.mu.Lock()
	prev, ok := p.snapshots[c.UserID]
	if ok && prev.Online {
		wasOnline = true
	}
	payload.Online = true
	payload.Devices = deviceCount
	payload.LastSeen = time.Now().UnixMilli()
	p.snapshots[c.UserID] = payload
	p.mu.Unlock()

	if !wasOnline {
		p.queueAnnounce(c.UserID)
	}
}

// MarkOffline — client uzilganda. User boshqa qurilmasi qolsa — shunchaki
// devices soni kamayadi; aks holda offline e'lon qilinadi.
// Copy-on-write: saqlangan pointer mutatsiya qilinmaydi (reader'lar eski
// nusxani lock'siz o'qiyotgan bo'lishi mumkin — race oldini).
func (p *PresenceService) MarkOffline(c *ws.Client) {
	deviceCount := p.hub.UserDeviceCount(c.UserID)
	if deviceCount > 0 {
		// Boshqa qurilmalari qolgan — faqat devices soni yangilanadi.
		p.mu.Lock()
		if pl, ok := p.snapshots[c.UserID]; ok {
			cp := clonePayload(pl)
			cp.Devices = deviceCount
			cp.LastSeen = time.Now().UnixMilli()
			p.snapshots[c.UserID] = cp
		}
		p.mu.Unlock()
		payload := p.snapshot(c.UserID)
		if payload.Online {
			p.queueAnnounce(c.UserID)
		}
		return
	}
	p.mu.Lock()
	if pl, ok := p.snapshots[c.UserID]; ok {
		cp := clonePayload(pl)
		cp.Online = false
		cp.Devices = 0
		cp.LastSeen = time.Now().UnixMilli()
		p.snapshots[c.UserID] = cp
	}
	p.mu.Unlock()

	// last_seen DB da saqlanadi (show_last_seen bo'lmasa ham server bilib turadi).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = p.repo.Users.SetLastSeen(ctx, c.UserID, time.Now())

	p.queueAnnounce(c.UserID)
}

// queueAnnounce — user holatini batchnavbatga qo'shadi; flusher barchasini
// bitta presence xabarida hammaga yuboradi (150ms ichida).
func (p *PresenceService) queueAnnounce(userID string) {
	pl := p.snapshot(userID)
	if pl == nil {
		return
	}
	p.mu.Lock()
	p.pending[userID] = pl
	p.mu.Unlock()
	select {
	case p.flushCh <- struct{}{}:
	default:
	}
}

// flusher — to'plangan presence o'zgarishlarini muntazam yuborib turadi.
func (p *PresenceService) flusher() {
	tick := time.NewTicker(p.flushTick)
	defer tick.Stop()
	for {
		select {
		case <-p.flushCh:
			p.flushPresence()
		case <-tick.C:
			p.flushPresence()
		case <-p.stopCh:
			return
		}
	}
}

// flushPresence — barcha kutilgan presence o'zgarishlarini yuboradi.
// Maxfiylik: show_online_status=false subject'lar offline ko'rinadi;
// bloklangan juftliklar bir-birining holatini ko'rmaydi (BlocksAny).
func (p *PresenceService) flushPresence() {
	p.mu.Lock()
	if len(p.pending) == 0 {
		p.mu.Unlock()
		return
	}
	users := make([]*ws.UserPayload, 0, len(p.pending))
	for _, pl := range p.pending {
		users = append(users, pl)
	}
	p.pending = make(map[string]*ws.UserPayload)
	blocks := p.blocks
	p.mu.Unlock()
	for _, pl := range users {
		if !p.showOnline(pl.ID) {
			cp := clonePayload(pl)
			cp.Online = false
			cp.Devices = 0
			*pl = *cp
		}
	}
	if blocks != nil {
		p.hub.AnnouncePresenceFiltered(users, func(recipientID, subjectID string) bool {
			if recipientID == subjectID {
				return false
			}
			return blocks.BlocksAny(recipientID, subjectID)
		})
		return
	}
	p.hub.AnnouncePresenceBatch(users)
}

// OnlineUsers — presence ro'yxatini qaytaradi.
func (p *PresenceService) OnlineUsers() []*ws.UserPayload {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*ws.UserPayload, 0, len(p.snapshots))
	for _, pl := range p.snapshots {
		if !pl.Online {
			continue
		}
		out = append(out, clonePayload(pl))
	}
	return out
}

// OnlineUsersFor — bitta viewer uchun filtrlangan ro'yxat (sendReady):
// bloklangan juftliklar chiqariladi, show_online_status=false user'lar
// offline ko'rinadi.
func (p *PresenceService) OnlineUsersFor(viewerID string) []*ws.UserPayload {
	p.mu.RLock()
	blocks := p.blocks
	list := make([]*ws.UserPayload, 0, len(p.snapshots))
	for _, pl := range p.snapshots {
		if !pl.Online {
			continue
		}
		list = append(list, clonePayload(pl))
	}
	p.mu.RUnlock()
	out := list[:0]
	for _, pl := range list {
		if pl.ID != viewerID && blocks != nil && blocks.BlocksAny(viewerID, pl.ID) {
			continue
		}
		if !p.showOnline(pl.ID) {
			pl.Online = false
			pl.Devices = 0
		}
		out = append(out, pl)
	}
	return out
}

// UserSnapshot — user metadata (message render uchun). Cache'da bo'lmasa
// DB'dan yuklaydi. Har doim CLONE qaytaradi — caller mutatsiyasi yoki keyingi
// MarkOnline/Offline yozuvlari bilan race bo'lmaydi.
func (p *PresenceService) UserSnapshot(userID string) *ws.UserPayload {
	p.mu.RLock()
	pl := p.snapshots[userID]
	p.mu.RUnlock()
	if pl != nil {
		return clonePayload(pl)
	}
	u, err := p.repo.Users.ByID(context.Background(), userID)
	if err != nil {
		slog.Warn("user snapshot miss", "user_id", userID, "err", err)
		return &ws.UserPayload{ID: userID}
	}
	payload := &ws.UserPayload{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarURL,
		Verified:    u.Verified,
		Online:      false,
	}
	p.mu.Lock()
	if existing, ok := p.snapshots[userID]; ok {
		// Poygada boshqa goroutine yozgan bo'lsa — uni ezmaymiz.
		cp := clonePayload(existing)
		p.mu.Unlock()
		return cp
	}
	p.snapshots[userID] = payload
	p.mu.Unlock()
	return clonePayload(payload)
}

func (p *PresenceService) snapshot(userID string) *ws.UserPayload {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return clonePayload(p.snapshots[userID])
}

func clonePayload(p *ws.UserPayload) *ws.UserPayload {
	if p == nil {
		return &ws.UserPayload{}
	}
	c := *p
	return &c
}

// SetSnapshotMetadata — client ulanganda username/display_name ni yangilaydi.
// Copy-on-write (race oldini).
func (p *PresenceService) SetSnapshotMetadata(c *ws.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pl, ok := p.snapshots[c.UserID]
	if !ok {
		pl = &ws.UserPayload{ID: c.UserID}
	} else {
		pl = clonePayload(pl)
	}
	pl.Username = c.Username
	pl.DisplayName = c.DisplayName
	pl.AvatarURL = c.AvatarURL
	pl.Verified = c.Verified
	p.snapshots[c.UserID] = pl
}
