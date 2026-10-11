package ws

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// testConn — real TCP orqali test uchun lokal websocket server.
type testServer struct {
	hub *Hub
}

// Hub ichida qatnashuvchi clientlar uchun in-memory test "ulanish".
type fakeConn struct {
	out chan []byte // server -> client xabarlar
}

func (f *fakeConn) WriteMessage(t int, p []byte) error {
	select {
	case f.out <- append([]byte(nil), p...):
	case <-time.After(5 * time.Second):
		return websocket.ErrCloseSent
	}
	return nil
}

func (f *fakeConn) ReadMessage() (int, []byte, error) {
	return websocket.TextMessage, []byte(`{"type":"ping"}`), nil
}
func (f *fakeConn) Close() error                      { close(f.out); return nil }
func (f *fakeConn) SetReadDeadline(time.Time) error   { return nil }
func (f *fakeConn) SetWriteDeadline(time.Time) error  { return nil }
func (f *fakeConn) SetReadLimit(int64)                {}
func (f *fakeConn) SetPongHandler(func(string) error) {}

// newTestClient — hub'ga qo'shilgan client (real websocket siz).
func newTestClient(t *testing.T, h *Hub, userID, username string, connReady chan struct{}) *Client {
	t.Helper()
	conn := &fakeConn{out: make(chan []byte, 64)}
	c := NewClient(conn, "device-"+userID)
	c.UserID = userID
	c.Username = username
	c.DisplayName = username
	c.NotifyOn = true
	h.Join(c, "global")
	return c
}

func drain(c *Client) []byte {
	select {
	case b := <-c.send:
		return b
	case <-time.After(time.Second):
		return nil
	}
}

func TestHubJoinAndOnlineCount(t *testing.T) {
	h := NewHub()
	a := newTestClient(t, h, "u1", "alice", nil)
	b := newTestClient(t, h, "u1", "alice", nil) // multi-device
	c := newTestClient(t, h, "u2", "bob", nil)
	if got := h.UserDeviceCount("u1"); got != 2 {
		t.Errorf("u1 device count = %d, want 2", got)
	}
	online := h.OnlineUsers()
	found := false
	for _, u := range online {
		if u.ID == "u2" {
			found = true
		}
	}
	if !found {
		t.Error("u2 missing from online users")
	}
	h.Leave(b, "global")
	if got := h.UserDeviceCount("u1"); got != 1 {
		t.Errorf("after leave u1 = %d, want 1", got)
	}
	h.Leave(a, "global")
	if got := h.UserDeviceCount("u1"); got != 0 {
		t.Errorf("after all leave u1 = %d, want 0", got)
	}
	h.Leave(c, "global")
}

// TestSlowConsumerDoesNotBlockOthers — buffer to'lgan client broadcast'ni
// bloklamasligi kerak (chat "qotmasligi" sharti).
func TestSlowConsumerDoesNotBlockOthers(t *testing.T) {
	h := NewHub()
	fast := newTestClient(t, h, "f", "fast", nil)
	slow := newTestClient(t, h, "s", "slow", nil)

	// Fast client uchun writePump'ni simulyatsiya qilamiz (send kanalini bo'shatish).
	// Filler soniga teng qabul qilib, hammasini o'zlashtirganini signal qiladi.
	const fillers = sendBuffer + 1
	consumed := make(chan struct{})
	go func() {
		for i := 0; i < fillers; i++ {
			<-fast.send
		}
		close(consumed)
	}()

	// Slow client'ning bufferini to'ldiramiz — u pompasiz, shuning uchun
	// oxirgi filler'da uziladi (slow-consumer himoya).
	for i := 0; i < fillers; i++ {
		h.BroadcastToRoom("global", &ServerMsg{Type: STypeMsg, Reason: "filler"})
	}
	<-consumed
	if !slow.Closed() {
		t.Error("slow consumer should have been closed")
	}

	// Fast client hali tirik va keyingi xabarni oladi (chat qotmaydi).
	if fast.Closed() {
		t.Fatal("fast client should still be open")
	}
	h.BroadcastToRoom("global", &ServerMsg{Type: STypeMsg, Reason: "alive?"})
	b := drain(fast)
	if b == nil {
		t.Fatal("fast client should receive message after slow consumer drop")
	}
	var m ServerMsg
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Type != STypeMsg {
		t.Errorf("type = %s, want msg", m.Type)
	}
}

// TestBroadcastReachesAllRooms — barcha a'zolarga yetib boradi.
func TestBroadcastReachesAllRooms(t *testing.T) {
	h := NewHub()
	c1 := newTestClient(t, h, "a", "anna", nil)
	c2 := newTestClient(t, h, "b", "bob", nil)
	h.BroadcastToRoom("global", &ServerMsg{Type: STypeMsg, Reason: "hi"})
	b1 := drain(c1)
	b2 := drain(c2)
	if b1 == nil || b2 == nil {
		t.Fatal("both should get the message")
	}
}

// TestForceLogoutClosesClients — parol o'zgarganda barcha qurilmalar uziladi.
func TestForceLogoutClosesClients(t *testing.T) {
	h := NewHub()
	a := newTestClient(t, h, "u1", "alice", nil)
	b := newTestClient(t, h, "u1", "alice", nil)
	h.ForceLogout("u1", "password_changed")

	// ForceLogout xabari + 500ms keyin close.
	deadline := time.After(3 * time.Second)
	for {
		if a.Closed() && b.Closed() {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("clients not closed: a=%v b=%v", a.Closed(), b.Closed())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestConcurrentBroadcast — data race yo'q (go test -race bilan).
func TestConcurrentBroadcast(t *testing.T) {
	h := NewHub()
	var clients []*Client
	for i := 0; i < 10; i++ {
		clients = append(clients, newTestClient(t, h, string(rune('a'+i)), "u", nil))
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				h.BroadcastToRoom("global", &ServerMsg{Type: STypeMsg, Reason: "x"})
				h.OnlineUsers()
			}
		}()
	}
	wg.Wait()
	_ = clients
}
