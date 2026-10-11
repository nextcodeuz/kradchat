package ws

import (
	"sync"
)

// Room — bitta chat xonasi (global va boshqalar).
type Room struct {
	mu      sync.RWMutex
	name    string
	clients map[*Client]struct{}
}

func newRoom(name string) *Room {
	return &Room{name: name, clients: make(map[*Client]struct{})}
}

func (r *Room) Name() string { return r.name }

func (r *Room) add(c *Client) {
	r.mu.Lock()
	r.clients[c] = struct{}{}
	r.mu.Unlock()
}

func (r *Room) remove(c *Client) {
	r.mu.Lock()
	delete(r.clients, c)
	r.mu.Unlock()
}

func (r *Room) size() int {
	r.mu.RLock()
	n := len(r.clients)
	r.mu.RUnlock()
	return n
}

// broadcast JSON marshal qilingan xabarni xonadagi barchaga yuboradi.
// Snapshot olib yuboriladi (broadcastFiltered bilan izchil) — yuborish paytida
// slow-consumer Close chaqirsa ham room lock ushlanmaydi.
func (r *Room) broadcast(payload []byte) {
	r.mu.RLock()
	clients := make([]*Client, 0, len(r.clients))
	for c := range r.clients {
		clients = append(clients, c)
	}
	r.mu.RUnlock()
	for _, c := range clients {
		c.sendRaw(payload)
	}
}

func (r *Room) broadcastFiltered(payload []byte, filter func(*Client) bool) {
	r.mu.RLock()
	clients := make([]*Client, 0, len(r.clients))
	for c := range r.clients {
		clients = append(clients, c)
	}
	r.mu.RUnlock()
	for _, c := range clients {
		if filter(c) {
			c.sendRaw(payload)
		}
	}
}
