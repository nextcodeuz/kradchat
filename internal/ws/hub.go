package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"kradchat/internal/model"
)

// Hub — barcha xonalar va user->client bog'lanishlarni boshqaradi.
// Redis siz, to'liq in-memory, goroutine xavfsiz.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]*Room

	// users[userID] -> client'lar (multi-device)
	users map[string]map[*Client]struct{}
	// byDevice[deviceID] -> client (kick uchun O(1))
	byDevice map[string]*Client

	closed bool
}

func NewHub() *Hub {
	return &Hub{
		rooms:    make(map[string]*Room),
		users:    make(map[string]map[*Client]struct{}),
		byDevice: make(map[string]*Client),
	}
}

func (h *Hub) Room(name string) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.getOrCreate(name)
}

func (h *Hub) getOrCreate(name string) *Room {
	if r, ok := h.rooms[name]; ok {
		return r
	}
	r := newRoom(name)
	h.rooms[name] = r
	return r
}

// Join user'ni xonaga qo'shadi va users indexini yangilaydi.
func (h *Hub) Join(c *Client, roomName string) {
	// Lock tartibi: avval client snapshot, keyin hub — Disconnect bilan bir xil.
	// (c.rooms client mutex bilan himoyalangan.)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	r := h.getOrCreate(roomName)
	r.add(c)
	c.mu.Lock()
	if c.rooms == nil {
		c.rooms = make(map[string]*Room)
	}
	c.rooms[roomName] = r
	c.mu.Unlock()

	um, ok := h.users[c.UserID]
	if !ok {
		um = make(map[*Client]struct{})
		h.users[c.UserID] = um
	}
	um[c] = struct{}{}
	if c.DeviceID != "" {
		h.byDevice[c.DeviceID] = c
	}
	h.mu.Unlock()
}

// Leave user'ni xonadan va agar boshqa client bo'lmasa users indexidan oladi.
func (h *Hub) Leave(c *Client, roomName string) {
	c.mu.Lock()
	r, ok := c.rooms[roomName]
	if ok {
		delete(c.rooms, roomName)
	}
	c.mu.Unlock()

	h.mu.Lock()
	if ok {
		if hr, exists := h.rooms[roomName]; exists {
			_ = hr
			r.remove(c)
			h.cleanupRoomLocked(roomName)
		} else {
			r.remove(c)
		}
	}
	if um, ok := h.users[c.UserID]; ok {
		delete(um, c)
		if len(um) == 0 {
			delete(h.users, c.UserID)
		}
	}
	if c.DeviceID != "" {
		if cur, ok := h.byDevice[c.DeviceID]; ok && cur == c {
			delete(h.byDevice, c.DeviceID)
		}
	}
	h.mu.Unlock()
}

// Disconnect client'ni barcha xonalardan va user indexidan tozalaydi.
func (h *Hub) Disconnect(c *Client) {
	// c.rooms ni client lock ostida snapshot qilamiz (Join bilan concurrent
	// map read/write panic'i oldini), keyin hub lock ostida tozalaymiz.
	c.mu.Lock()
	rooms := make(map[string]*Room, len(c.rooms))
	for name, r := range c.rooms {
		rooms[name] = r
	}
	c.rooms = make(map[string]*Room)
	c.mu.Unlock()

	h.mu.Lock()
	for name, r := range rooms {
		if hr, ok := h.rooms[name]; ok {
			hr.remove(c)
			_ = r
			h.cleanupRoomLocked(name)
		} else {
			r.remove(c)
		}
	}
	if um, ok := h.users[c.UserID]; ok {
		delete(um, c)
		if len(um) == 0 {
			delete(h.users, c.UserID)
		}
	}
	if c.DeviceID != "" {
		if cur, ok := h.byDevice[c.DeviceID]; ok && cur == c {
			delete(h.byDevice, c.DeviceID)
		}
	}
	h.mu.Unlock()
}

// cleanupRoomLocked — bo'sh (0 client) va global bo'lmagan xonalarni o'chiradi.
// Hub.rooms cheksiz o'sishi (memory leak) oldini oladi. Caller hub.mu.Lock
// ushlab turishi shart.
func (h *Hub) cleanupRoomLocked(roomName string) {
	if roomName == model.RoomGlobal {
		return
	}
	if r, ok := h.rooms[roomName]; ok && r.size() == 0 {
		delete(h.rooms, roomName)
	}
}

// UserDeviceCount — user'ning hozir ulangan qurilmalari soni.
func (h *Hub) UserDeviceCount(userID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.users[userID])
}

// OnlineUsers — barcha onlayn userlar ro'yxati (unique userID).
func (h *Hub) OnlineUsers() []*UserPayload {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*UserPayload, 0, len(h.users))
	seen := make(map[string]bool, len(h.users))
	for userID, clients := range h.users {
		if seen[userID] {
			continue
		}
		seen[userID] = true
		devices := len(clients)
		up := &UserPayload{ID: userID, Devices: devices, Online: true}
		for c := range clients {
			if up.Username == "" {
				up.Username = c.Username
				up.DisplayName = c.DisplayName
				up.AvatarURL = c.AvatarURL
			}
			break
		}
		out = append(out, up)
	}
	return out
}

// ClientByDeviceID — device_id bo'yicha client qidiradi (kick uchun O(1)).
func (h *Hub) ClientByDeviceID(deviceID string) *Client {
	if deviceID == "" {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if c, ok := h.byDevice[deviceID]; ok {
		return c
	}
	// Eski ulanishlar (index yo'q davridan) uchun fallback scan.
	for _, um := range h.users {
		for c := range um {
			if c.DeviceID == deviceID {
				return c
			}
		}
	}
	return nil
}

// ClientsForUser — user'ning barcha clientlari.
func (h *Hub) ClientsForUser(userID string) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	um := h.users[userID]
	out := make([]*Client, 0, len(um))
	for c := range um {
		out = append(out, c)
	}
	return out
}

// NotifyUser — user'ga (barcha qurilmalariga) WS xabar yuboradi.
func (h *Hub) NotifyUser(userID string, msg *ServerMsg) {
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	for _, c := range h.ClientsForUser(userID) {
		c.sendRaw(b)
	}
}

// ForceLogout — user'ning barcha clientlarini uzadi.
func (h *Hub) ForceLogout(userID string, reason string) {
	msg := &ServerMsg{Type: STypeForceLogout, Reason: reason}
	b, _ := json.Marshal(msg)
	for _, c := range h.ClientsForUser(userID) {
		c.sendRaw(b)
		time.AfterFunc(500*time.Millisecond, func() {
			c.Close()
		})
	}
	slog.Info("force_logout", "user_id", userID, "reason", reason)
}

// BroadcastToRoom — xonadagi barchaga yuboradi.
func (h *Hub) BroadcastToRoom(roomName string, msg *ServerMsg) {
	h.broadcastToRoom(roomName, msg, nil)
}

// BroadcastToRoomFiltered — xonadagi clientlarga yuboradi, lekin
// skip(userID) true qaytargan clientlar o'tkazib yuboriladi.
// (Bloklash filtrasi uchun: oluvchi sender'ni bloklagan bo'lsa skip=true.)
func (h *Hub) BroadcastToRoomFiltered(roomName string, msg *ServerMsg, skip func(userID string) bool) {
	h.broadcastToRoom(roomName, msg, skip)
}

func (h *Hub) broadcastToRoom(roomName string, msg *ServerMsg, skip func(userID string) bool) {
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	r := h.rooms[roomName]
	h.mu.RUnlock()
	if r == nil {
		slog.Debug("broadcast: room not found", "room", roomName)
		return
	}
	if skip == nil {
		r.broadcast(b)
		return
	}
	r.broadcastFiltered(b, func(c *Client) bool {
		return !skip(c.UserID)
	})
}

// AnnouncePresence — bitta user'ning online/offline o'zgarishini e'lon qiladi.
// Har bir ulangan client global xonada turadi, shuning uchun faqat global
// xonaga yuboriladi (barcha xonalarni aylanib chiqish O(rooms) edi — massiv
// ulanishlarda chat qotib qolardi).
func (h *Hub) AnnouncePresence(userID string, payload *UserPayload) {
	h.AnnouncePresenceBatch([]*UserPayload{payload})
}

// AnnouncePresenceBatch — bir nechta user'ning holatini bitta xabarda yuboradi.
// Massiv ulanish (minglab kishi bir vaqtda kirish) paytida O(N²) yuborishni
// O(N) ga tushiradi: N ta alohida xabar o'rniga bitta guruhli xabar.
func (h *Hub) AnnouncePresenceBatch(users []*UserPayload) {
	if len(users) == 0 {
		return
	}
	msg := &ServerMsg{Type: STypePresence, Users: users}
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	r := h.rooms[model.RoomGlobal]
	h.mu.RUnlock()
	if r != nil {
		r.broadcast(b)
	}
}

// AnnouncePresenceFiltered — presence'ni har bir subject uchun alohida
// filtrlab yuboradi: skip(recipient, subject) true bo'lsa (masalan, blok)
// o'sha oluvchiga yuborilmaydi. Bloklangan/berkitilgan holat sizmasligi uchun.
func (h *Hub) AnnouncePresenceFiltered(users []*UserPayload, skip func(recipientID, subjectID string) bool) {
	if len(users) == 0 {
		return
	}
	h.mu.RLock()
	r := h.rooms[model.RoomGlobal]
	h.mu.RUnlock()
	if r == nil {
		return
	}
	for _, u := range users {
		msg := &ServerMsg{Type: STypePresence, Users: []*UserPayload{u}}
		b, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		subj := u.ID
		r.broadcastFiltered(b, func(c *Client) bool {
			return !skip(c.UserID, subj)
		})
	}
}
