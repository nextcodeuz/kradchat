package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/database"
	"kradchat/internal/model"
	"kradchat/internal/repository"
	"kradchat/internal/ws"

	"github.com/pquerna/otp/totp"
)

func twoFATestCfg(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Database: config.Database{
			Driver:   "sqlite",
			Path:     filepath.Join(t.TempDir(), "krad_test.db"),
			MaxConns: 10,
		},
		JWT: config.JWT{
			Secret:        strings.Repeat("x", 32),
			AccessExpire:  15 * time.Minute,
			RefreshExpire: 24 * time.Hour,
			Issuer:        "kradchat-test",
		},
		Security: config.Security{
			BcryptCost:        4,
			PendingSessionTTL: 15 * time.Minute,
		},
		Background: config.Background{
			Dir:  t.TempDir(),
			Poll: time.Minute,
		},
	}
}

func newTestServices(t *testing.T) *Services {
	t.Helper()
	cfg := twoFATestCfg(t)
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
	return New(cfg, repo, nil)
}

// TestTwoFALoginWrongCodeKeepsPending — noto'g'ri 2FA kodi pending sessiyani
// iste'mol qilmasligini tekshiradi (regression: eski consumePending birinchi
// noto'g'ri koddan keyin sessiyani o'chirib, "invalid token" bilan to'xtatardi).
func TestTwoFALoginWrongCodeKeepsPending(t *testing.T) {
	svc := newTestServices(t)
	ctx := context.Background()

	u, _, err := svc.Auth.Register(ctx, model.RegisterRequest{
		Username:    "alice",
		Password:    "Test1234!",
		DisplayName: "Alice",
	}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	setup, err := svc.TwoFA.Setup(ctx, u.ID, u.Username, "Test1234!")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if err := svc.TwoFA.Enable(ctx, u.ID, code, "Test1234!"); err != nil {
		t.Fatalf("enable: %v", err)
	}

	login, err := svc.Auth.Login(ctx, model.LoginRequest{Username: u.Username, Password: "Test1234!"}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if login.Status != "2fa_required" || login.PendingSession == "" {
		t.Fatalf("expected 2fa_required with pending session, got %+v", login)
	}
	pending := login.PendingSession

	if _, err := svc.Auth.TwoFAVerify(ctx, pending, "000000"); !errors.Is(err, ErrTwoFAInvalid) {
		t.Fatalf("first wrong code: expected ErrTwoFAInvalid, got %v", err)
	}
	if _, err := svc.Auth.TwoFAVerify(ctx, pending, "111111"); !errors.Is(err, ErrTwoFAInvalid) {
		t.Fatalf("second wrong code: expected ErrTwoFAInvalid (pending must survive), got %v", err)
	}

	res, err := svc.Auth.TwoFAVerify(ctx, pending, code)
	if err != nil {
		t.Fatalf("correct code: expected success, got %v", err)
	}
	if res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatal("expected issued tokens")
	}

	// Sessiya muvaffaqiyatli tasdiqlangach iste'mol qilingan bo'lishi kerak.
	if _, err := svc.Auth.TwoFAVerify(ctx, pending, code); !errors.Is(err, ErrBadToken) {
		t.Fatalf("reused pending: expected ErrBadToken, got %v", err)
	}
}

// TestTwoFASetupRequiresPassword — 2FA Setup parolsiz ishlamasligi va
// noto'g'ri parol bilan rad etilishini tekshiradi (stolen-token takeover oldi).
func TestTwoFASetupRequiresPassword(t *testing.T) {
	svc := newTestServices(t)
	ctx := context.Background()

	u, _, err := svc.Auth.Register(ctx, model.RegisterRequest{
		Username: "bob", Password: "Test1234!", DisplayName: "Bob",
	}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := svc.TwoFA.Setup(ctx, u.ID, u.Username, "wrong-pass"); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("wrong password: expected ErrPasswordMismatch, got %v", err)
	}
	setup, err := svc.TwoFA.Setup(ctx, u.ID, u.Username, "Test1234!")
	if err != nil {
		t.Fatalf("correct password: expected success, got %v", err)
	}

	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if err := svc.TwoFA.Enable(ctx, u.ID, code, "Test1234!"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// 2FA yoqilganidan keyin Setup secretni aylantirmasligi kerak.
	if _, err := svc.TwoFA.Setup(ctx, u.ID, u.Username, "Test1234!"); !errors.Is(err, ErrTwoFAAlreadyEnabled) {
		t.Fatalf("setup while enabled: expected ErrTwoFAAlreadyEnabled, got %v", err)
	}
}

// enable2FAForTest — test user uchun 2FA yoqib secret qaytaradi.
func enable2FAForTest(t *testing.T, svc *Services, u *model.User) string {
	t.Helper()
	ctx := context.Background()
	setup, err := svc.TwoFA.Setup(ctx, u.ID, u.Username, "Test1234!")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if err := svc.TwoFA.Enable(ctx, u.ID, code, "Test1234!"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	return setup.Secret
}

// TestTwoFAUserLevelLockout — H3 regressiya: yangi pendingID olib
// pending başına limitni chetlab o'tib bo'lmaydi; akkaunt bo'yicha jami
// limit to'lgach yangi pending'dagi BIRINCHI noto'g'ri kod ham lock beradi.
func TestTwoFAUserLevelLockout(t *testing.T) {
	svc := newTestServices(t)
	ctx := context.Background()

	u, _, err := svc.Auth.Register(ctx, model.RegisterRequest{
		Username: "dave", Password: "Test1234!", DisplayName: "Dave",
	}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	enable2FAForTest(t, svc, u)

	// 4 sikl × 5 urinish = 20 fail (har sikl yangi pending bilan).
	for c := 0; c < 4; c++ {
		login, err := svc.Auth.Login(ctx, model.LoginRequest{Username: u.Username, Password: "Test1234!"}, "127.0.0.1", "test-agent")
		if err != nil {
			t.Fatalf("cycle %d login: %v", c, err)
		}
		for i := 0; i < max2FAAttempts; i++ {
			_, err := svc.Auth.TwoFAVerify(ctx, login.PendingSession, "000000")
			if !errors.Is(err, ErrTwoFAInvalid) && !errors.Is(err, ErrTwoFALocked) {
				t.Fatalf("cycle %d attempt %d: got %v", c, i, err)
			}
		}
	}
	// Akkaunt limiti to'lgan — yangi pending'dagi birinchi xato ham lock.
	login, err := svc.Auth.Login(ctx, model.LoginRequest{Username: u.Username, Password: "Test1234!"}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("final login: %v", err)
	}
	if _, err := svc.Auth.TwoFAVerify(ctx, login.PendingSession, "000000"); !errors.Is(err, ErrTwoFALocked) {
		t.Fatalf("user-level lockout: expected ErrTwoFALocked, got %v", err)
	}
}

// TestTOTPReplayRejected — M3 regressiya: bir kod ikki marta o'tmaydi.
func TestTOTPReplayRejected(t *testing.T) {
	svc := newTestServices(t)
	ctx := context.Background()

	u, _, err := svc.Auth.Register(ctx, model.RegisterRequest{
		Username: "erin", Password: "Test1234!", DisplayName: "Erin",
	}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	secret := enable2FAForTest(t, svc, u)
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}

	login, err := svc.Auth.Login(ctx, model.LoginRequest{Username: u.Username, Password: "Test1234!"}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := svc.Auth.TwoFAVerify(ctx, login.PendingSession, code); err != nil {
		t.Fatalf("first use: %v", err)
	}
	// Xuddi shu kod, yangi pending — replay sifatida rad etilishi kerak.
	login2, err := svc.Auth.Login(ctx, model.LoginRequest{Username: u.Username, Password: "Test1234!"}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("login2: %v", err)
	}
	if _, err := svc.Auth.TwoFAVerify(ctx, login2.PendingSession, code); !errors.Is(err, ErrTwoFAInvalid) {
		t.Fatalf("replay: expected ErrTwoFAInvalid, got %v", err)
	}
}

// TestLoginPerUserThrottle — H4 regressiya: bitta akkauntga 10/min dan
// ortiq login urinish rad etiladi (parol brute-force/spray sekinlashadi).
func TestLoginPerUserThrottle(t *testing.T) {
	svc := newTestServices(t)
	ctx := context.Background()

	// Faqat limiter tekshiriladi — user mavjud bo'lmasa ham token sarflanadi.
	probed := 0
	limited := false
	for i := 0; i < 15; i++ {
		_, err := svc.Auth.Login(ctx, model.LoginRequest{Username: "throttleprobe", Password: "wrong"}, "127.0.0.1", "test-agent")
		if errors.Is(err, ErrRateLimited) {
			limited = true
			break
		}
		probed++
	}
	if !limited {
		t.Fatalf("expected ErrRateLimited within 15 attempts, got %d invalid_credentials", probed)
	}
	if probed > 11 {
		t.Fatalf("too many attempts before limit: %d", probed)
	}
}

// presenceFakeConn — MarkOnline testi uchun minimal ws.Conn.
type presenceFakeConn struct{}

func (presenceFakeConn) ReadMessage() (int, []byte, error)   { return 0, nil, nil }
func (presenceFakeConn) WriteMessage(_ int, _ []byte) error  { return nil }
func (presenceFakeConn) Close() error                        { return nil }
func (presenceFakeConn) SetReadDeadline(_ time.Time) error   { return nil }
func (presenceFakeConn) SetWriteDeadline(_ time.Time) error  { return nil }
func (presenceFakeConn) SetReadLimit(_ int64)                {}
func (presenceFakeConn) SetPongHandler(_ func(string) error) {}

// markOnlineAs — test user'ni presence'ga kiritadi.
func markOnlineAs(t *testing.T, svc *Services, hub *ws.Hub, u *model.User) {
	t.Helper()
	c := ws.NewClient(presenceFakeConn{}, "dev-"+u.ID)
	c.UserID = u.ID
	c.Username = u.Username
	c.DisplayName = u.DisplayName
	hub.Join(c, model.RoomGlobal)
	svc.Presence.SetSnapshotMetadata(c)
	svc.Presence.MarkOnline(c)
}

func onlineIDs(list []*ws.UserPayload) map[string]bool {
	m := map[string]bool{}
	for _, p := range list {
		if p.Online {
			m[p.ID] = true
		}
	}
	return m
}

// TestPresenceRespectsBlocks — F2: bloklangan juftlik bir-birini online
// ro'yxatda ko'rmaydi (OnlineUsersFor filtri).
func TestPresenceRespectsBlocks(t *testing.T) {
	cfg := twoFATestCfg(t)
	ctx := context.Background()
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(ctx, db, cfg.Database.DialectName()); err != nil {
		t.Fatal(err)
	}
	repo, err := repository.New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub()
	svc := New(cfg, repo, hub)

	mk := func(name string) *model.User {
		u, _, err := svc.Auth.Register(ctx, model.RegisterRequest{
			Username: name, Password: "Test1234!", DisplayName: name,
		}, "127.0.0.1", "test-agent")
		if err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
		return u
	}
	alice := mk("alicep")
	bob := mk("bobp")
	markOnlineAs(t, svc, hub, alice)
	markOnlineAs(t, svc, hub, bob)

	if got := onlineIDs(svc.Presence.OnlineUsersFor(alice.ID)); !got[alice.ID] || !got[bob.ID] {
		t.Fatalf("blokdan oldin ikkalasi ko'rinishi kerak: %v", got)
	}
	if err := svc.Blocks.Block(ctx, alice.ID, bob.ID); err != nil {
		t.Fatalf("block: %v", err)
	}
	if got := onlineIDs(svc.Presence.OnlineUsersFor(alice.ID)); got[bob.ID] {
		t.Fatalf("alice bob'ni ko'rmasligi kerak: %v", got)
	}
	if got := onlineIDs(svc.Presence.OnlineUsersFor(bob.ID)); got[alice.ID] {
		t.Fatalf("bob alice'ni ko'rmasligi kerak: %v", got)
	}
	// Xom ro'yxat o'zgarmagan (faqat viewer filtri).
	if got := onlineIDs(svc.Presence.OnlineUsers()); !got[alice.ID] || !got[bob.ID] {
		t.Fatalf("xom ro'yxat to'liq bo'lishi kerak: %v", got)
	}
}

// TestPresenceRespectsShowOnline — F2: show_online_status=false user
// boshqalarga offline ko'rinadi.
func TestPresenceRespectsShowOnline(t *testing.T) {
	cfg := twoFATestCfg(t)
	ctx := context.Background()
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(ctx, db, cfg.Database.DialectName()); err != nil {
		t.Fatal(err)
	}
	repo, err := repository.New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub()
	svc := New(cfg, repo, hub)

	mk := func(name string) *model.User {
		u, _, err := svc.Auth.Register(ctx, model.RegisterRequest{
			Username: name, Password: "Test1234!", DisplayName: name,
		}, "127.0.0.1", "test-agent")
		if err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
		return u
	}
	carol := mk("carolp")
	dave := mk("davep")
	markOnlineAs(t, svc, hub, carol)
	markOnlineAs(t, svc, hub, dave)

	off := false
	if _, err := svc.Auth.UpdateMe(ctx, dave.ID, model.UpdateMeRequest{ShowOnlineStatus: &off}); err != nil {
		t.Fatalf("updateme: %v", err)
	}
	svc.Presence.InvalidateShowOnline(dave.ID)

	got := onlineIDs(svc.Presence.OnlineUsersFor(carol.ID))
	if !got[carol.ID] {
		t.Fatalf("carol o'zi ko'rinishi kerak: %v", got)
	}
	if got[dave.ID] {
		t.Fatalf("dave offline ko'rinishi kerak: %v", got)
	}
}

// TestTwoFALoginLockout — cheklangan miqdordagi noto'g'ri kodlardan keyin
// pending sessiya o'chib, qayta login talab qilinishini tekshiradi (brute-force oldi).
func TestTwoFALoginLockout(t *testing.T) {
	svc := newTestServices(t)
	ctx := context.Background()

	u, _, err := svc.Auth.Register(ctx, model.RegisterRequest{
		Username: "carol", Password: "Test1234!", DisplayName: "Carol",
	}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	setup, err := svc.TwoFA.Setup(ctx, u.ID, u.Username, "Test1234!")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if err := svc.TwoFA.Enable(ctx, u.ID, code, "Test1234!"); err != nil {
		t.Fatalf("enable: %v", err)
	}

	login, err := svc.Auth.Login(ctx, model.LoginRequest{Username: u.Username, Password: "Test1234!"}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	pending := login.PendingSession

	for i := 0; i < max2FAAttempts-1; i++ {
		if _, err := svc.Auth.TwoFAVerify(ctx, pending, "000000"); !errors.Is(err, ErrTwoFAInvalid) {
			t.Fatalf("attempt %d: expected ErrTwoFAInvalid, got %v", i, err)
		}
	}
	// Limitga yetdik — sessiya o'chiriladi va lockout qaytariladi.
	if _, err := svc.Auth.TwoFAVerify(ctx, pending, "000000"); !errors.Is(err, ErrTwoFALocked) {
		t.Fatalf("lockout attempt: expected ErrTwoFALocked, got %v", err)
	}
	// Endi sessiya yo'q — to'g'ri kod ham qabul qilinmaydi, qayta login kerak.
	if _, err := svc.Auth.TwoFAVerify(ctx, pending, code); !errors.Is(err, ErrBadToken) {
		t.Fatalf("after lockout: expected ErrBadToken, got %v", err)
	}
}
