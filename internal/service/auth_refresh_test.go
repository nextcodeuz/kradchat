package service

import (
	"context"
	"testing"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/model"
	"kradchat/internal/repository"
)

// --- Refresh grace test stub'lari ---

type graceSessionRepo struct {
	sess     *model.Session
	nuked    []string
	revokeID string
}

func (s *graceSessionRepo) Create(_ context.Context, _ *model.Session) error { return nil }
func (s *graceSessionRepo) ByRefreshHash(_ context.Context, _ string) (*model.Session, error) {
	if s.sess == nil {
		return nil, repository.ErrNotFound
	}
	cp := *s.sess
	return &cp, nil
}
func (s *graceSessionRepo) Revoke(_ context.Context, id string) error {
	s.revokeID = id
	return nil
}
func (s *graceSessionRepo) RevokeAllForUser(_ context.Context, userID string) error {
	s.nuked = append(s.nuked, userID)
	return nil
}
func (s *graceSessionRepo) RevokeDevice(_ context.Context, _ string) error { return nil }

type graceUserRepo struct{ u *model.User }

func (g *graceUserRepo) Create(_ context.Context, _ *model.User) error { return nil }
func (g *graceUserRepo) ByUsername(_ context.Context, _ string) (*model.User, error) {
	return nil, repository.ErrNotFound
}
func (g *graceUserRepo) ByID(_ context.Context, _ string) (*model.User, error) {
	if g.u == nil {
		return nil, repository.ErrNotFound
	}
	return g.u, nil
}
func (g *graceUserRepo) Update(_ context.Context, _ *model.User) error { return nil }
func (g *graceUserRepo) UpdatePassword(_ context.Context, _, _ string) error {
	return nil
}
func (g *graceUserRepo) SetLastSeen(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (g *graceUserRepo) SetVerified(_ context.Context, _ string, _ bool) error {
	return nil
}

type graceDeviceRepo struct{}

func (graceDeviceRepo) Create(_ context.Context, _ *model.Device) error { return nil }
func (graceDeviceRepo) Update(_ context.Context, _ *model.Device) error { return nil }
func (graceDeviceRepo) ByID(_ context.Context, id string) (*model.Device, error) {
	return &model.Device{ID: id, UserID: "u1", IsActive: true}, nil
}
func (graceDeviceRepo) ByUser(_ context.Context, _ string) ([]*model.Device, error) {
	return nil, nil
}
func (graceDeviceRepo) ByFingerprint(_ context.Context, _, _ string) (*model.Device, error) {
	return nil, repository.ErrNotFound
}
func (graceDeviceRepo) Deactivate(_ context.Context, _, _ string) error { return nil }
func (graceDeviceRepo) CountActive(_ context.Context, _ string) (int, error) {
	return 0, nil
}

type graceTwoFARepo struct{}

func (graceTwoFARepo) SetEnabled(_ context.Context, _ string, _ bool, _ string) error {
	return nil
}
func (graceTwoFARepo) GetSecret(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (graceTwoFARepo) LogPasswordChange(_ context.Context, _, _, _ string) error {
	return nil
}
func (graceTwoFARepo) LatestPasswordChange(_ context.Context, _ string) (time.Time, error) {
	return time.Time{}, nil
}

func graceAuth(t *testing.T, sess *model.Session) (*AuthService, *graceSessionRepo) {
	t.Helper()
	srepo := &graceSessionRepo{sess: sess}
	repo := &repository.Repositories{
		Sessions: srepo,
		Users:    &graceUserRepo{u: &model.User{ID: "u1", Username: "t1x"}},
		Devices:  graceDeviceRepo{},
		TwoFA:    graceTwoFARepo{},
	}
	cfg := &config.Config{}
	cfg.JWT.RefreshExpire = 7 * 24 * time.Hour
	cfg.JWT.AccessExpire = 15 * time.Minute
	cfg.JWT.Secret = "0123456789abcdef0123456789abcdef"
	cfg.JWT.Issuer = "kradchat"
	return NewAuthService(cfg, repo), srepo
}

// Yangi revoke qilingan token qayta ishlatilsa (poyga yoki o'g'irlik —
// ajratib bo'lmaydi): oila revoke qilinadi + yangi juftlik beriladi.
// Hujum sessiyalari o'ladi, so'rov egasi ishlashda davom etadi.
func TestRefreshGraceReuseNukesButMints(t *testing.T) {
	recent := time.Now().Add(-5 * time.Second)
	sess := &model.Session{
		ID: "s1", UserID: "u1", DeviceID: "d1",
		RefreshHash: refreshHash("tok"),
		ExpiresAt:   time.Now().Add(time.Hour),
		RevokedAt:   &recent,
	}
	a, srepo := graceAuth(t, sess)
	res, err := a.Refresh(context.Background(), "tok")
	if err != nil {
		t.Fatalf("grace refresh: %v", err)
	}
	if res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatal("empty tokens")
	}
	if len(srepo.nuked) != 1 {
		t.Fatal("grace reuse must revoke family (theft indistinguishable from race)")
	}
}

// Eski revoke (o'g'irlik) — rad + nuke.
func TestRefreshOldReuseNukes(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	sess := &model.Session{
		ID: "s1", UserID: "u1", DeviceID: "d1",
		RefreshHash: refreshHash("tok"),
		ExpiresAt:   time.Now().Add(time.Hour),
		RevokedAt:   &old,
	}
	a, srepo := graceAuth(t, sess)
	if _, err := a.Refresh(context.Background(), "tok"); err != ErrSessionExpired {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	if len(srepo.nuked) != 1 {
		t.Fatal("old reuse must nuke sessions")
	}
}
