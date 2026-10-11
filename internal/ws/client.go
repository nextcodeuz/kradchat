package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait             = 10 * time.Second
	pongWait              = 60 * time.Second
	pingPeriod            = 54 * time.Second
	sendBuffer            = 256
	defaultMaxMessageSize = 64 * 1024
)

// maxMessageSize — WS freym limiti (config WS_MAX_MESSAGE_SIZE orqali).
// Testlar parallel ishlashi uchun atomic emas, faqat startup'da o'rnatiladi.
var maxMessageSize = int64(defaultMaxMessageSize)

// SetMaxMessageSize — server startup'da config'dan chaqiriladi.
func SetMaxMessageSize(n int64) {
	if n > 0 {
		maxMessageSize = n
	}
}

// Conn — websocket ulanishining minimal interfeysi (test uchun almashtiriladi).
type Conn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(messageType int, data []byte) error
	Close() error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetReadLimit(limit int64)
	SetPongHandler(h func(string) error)
}

var _ Conn = (*websocket.Conn)(nil)

type Client struct {
	conn Conn
	send chan []byte

	mu       sync.RWMutex
	rooms    map[string]*Room
	closed   bool
	closeOne sync.Once
	done     chan struct{}

	UserID      string
	Username    string
	DisplayName string
	AvatarURL   string
	Verified    bool
	DeviceID    string
	NotifyOn    bool

	// onMessage — kiruvchi xabarlar uchun callback (ws_handler tomonidan ulanadi).
	onMessage func(c *Client, raw []byte)
}

func NewClient(conn Conn, deviceID string) *Client {
	return &Client{
		conn:     conn,
		send:     make(chan []byte, sendBuffer),
		rooms:    make(map[string]*Room),
		done:     make(chan struct{}),
		DeviceID: deviceID,
		NotifyOn: true,
	}
}

func (c *Client) sendRaw(payload []byte) {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return
	}
	select {
	case c.send <- payload:
	default:
		// slow consumer — buffer to'lgan, ulanishni tozalaymiz
		c.Close()
	}
}

func (c *Client) Send(msg *ServerMsg) {
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	c.sendRaw(b)
}

// SetOnMessage — kiruvchi xabarlar uchun callback o'rnatadi.
func (c *Client) SetOnMessage(fn func(c *Client, raw []byte)) {
	c.onMessage = fn
}

func (c *Client) SendError(code, msg string) {
	c.Send(&ServerMsg{Type: STypeError, Code: code, Error: msg})
}

func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.closeOne.Do(func() { close(c.done) })
	c.mu.Unlock()
	c.conn.Close()
}

// Done — ulanish yopilganda yopiladigan kanal.
// request context'idan foydalanish mumkin emas: Go'da u ServeHTTP
// qaytishi bilanoq bekor qilinadi (hijacked ulanishlarda ham).
func (c *Client) Done() <-chan struct{} {
	return c.done
}

func (c *Client) Closed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// readPump — server->client ga yuboriladigan xabarlarni o'qiydi.
// Ulanish yopilganda tozalash Done() kanali orqali tashqaridan kuzatiladi
// (yagona mexanizm — ikki callback chalkashligi yo'q).
func (c *Client) readPump() {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("ws readPump panic", "user", c.UserID, "err", rec)
		}
		c.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		if c.onMessage == nil {
			continue
		}
		// Handler xatosi bitta xabarni tashlab o'tishga olib keladi,
		// ulanish buzilmaydi.
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("ws message handler panic", "user", c.UserID, "err", rec)
				}
			}()
			c.onMessage(c, raw)
		}()
	}
}

// writePump — send kanalidagi xabarlarni socketga yozadi.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("ws writePump panic", "user", c.UserID, "err", rec)
		}
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case payload, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Start — read va write pompalarini ishga tushiradi.
func (c *Client) Start() {
	go c.writePump()
	go c.readPump()
}
