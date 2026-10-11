package handler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/database"
	"kradchat/internal/repository"
	"kradchat/internal/service"
	"kradchat/internal/ws"
)

type fakeConn struct{}

var _ ws.Conn = (*fakeConn)(nil)

func (fakeConn) ReadMessage() (int, []byte, error)   { return 0, nil, nil }
func (fakeConn) WriteMessage(_ int, _ []byte) error  { return nil }
func (fakeConn) Close() error                        { return nil }
func (fakeConn) SetReadDeadline(_ time.Time) error   { return nil }
func (fakeConn) SetWriteDeadline(_ time.Time) error  { return nil }
func (fakeConn) SetReadLimit(_ int64)                {}
func (fakeConn) SetPongHandler(_ func(string) error) {}

var _ ws.Conn = (*fakeConn)(nil)

func wsTestServices(t *testing.T) (*config.Config, *service.Services, *ws.Hub) {
	t.Helper()
	cfg := &config.Config{
		Database: config.Database{
			Driver:   "sqlite",
			Path:     filepath.Join(t.TempDir(), "ws_test.db"),
			MaxConns: 10,
		},
		JWT: config.JWT{
			Secret:        strings.Repeat("y", 32),
			AccessExpire:  15 * time.Minute,
			RefreshExpire: 24 * time.Hour,
			Issuer:        "kradchat-test",
		},
		Security: config.Security{
			BcryptCost:        4,
			PendingSessionTTL: 15 * time.Minute,
			WsMsgPerMin:       60,
		},
		Background: config.Background{
			Dir:  t.TempDir(),
			Poll: time.Minute,
		},
	}
	ctx := context.Background()
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(ctx, db, cfg.Database.DialectName()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo, err := repository.New(db, cfg)
	if err != nil {
		t.Fatalf("repos: %v", err)
	}
	hub := ws.NewHub()
	svc := service.New(cfg, repo, hub)
	return cfg, svc, hub
}

// TestCleanupMarksOffline — ulanish yopilganda user offline bo'lishi kerak.
// Regression: eski tartib MarkOffline -> Disconnect edi, shunda oxirgi qurilma
// uzilganda ham UserDeviceCount hali o'zini sanardi va foydalanuvchi abadiy
// online bo'lib qolar edi ("kim ochsa, kirmasa ham online ko'rinadi").
func TestCleanupMarksOffline(t *testing.T) {
	cfg, svc, hub := wsTestServices(t)
	auth := svc.Auth
	h := NewWSHandler(cfg, svc, hub, auth, nil)

	client := ws.NewClient(fakeConn{}, "dev-1")
	client.UserID = "u1"
	client.Username = "alice"
	client.DisplayName = "Alice"

	hub.Join(client, "global")
	svc.Presence.SetSnapshotMetadata(client)
	svc.Presence.MarkOnline(client)

	if got := len(svc.Presence.OnlineUsers()); got != 1 {
		t.Fatalf("after MarkOnline expected 1 online, got %d", got)
	}

	// cleanup ham MarkOffline ham Disconnect bajaradi.
	h.cleanup(client)

	if got := len(svc.Presence.OnlineUsers()); got != 0 {
		t.Fatalf("after cleanup expected 0 online, got %d — user stuck online forever", got)
	}
}
