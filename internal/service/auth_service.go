package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/model"
	"kradchat/internal/repository"
	"kradchat/internal/security"
	"kradchat/internal/ws"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials  = errors.New("invalid username or password")
	ErrUserExists          = errors.New("username already taken")
	ErrTwoFARequired       = errors.New("two factor authentication required")
	ErrTwoFAInvalid        = errors.New("invalid two factor code")
	ErrTwoFALocked         = errors.New("too many failed two factor attempts; sign in again")
	ErrTwoFAAlreadyEnabled = errors.New("two factor authentication is already enabled")
	ErrPasswordMismatch    = errors.New("current password is incorrect")
	ErrSamePassword        = errors.New("new password must differ from current")
	ErrBadToken            = errors.New("invalid or expired token")
	ErrSessionExpired      = errors.New("session expired")
	ErrUserNotFound        = errors.New("user not found")
	ErrTooManyDevices      = errors.New("too many devices")
	ErrRateLimited         = errors.New("too many attempts; try again later")
)

// max2FAAttempts — bitta pending sessiya uchun ruxsat etilgan noto'g'ri kod
// urinishlari soni. Chegara oshgach sessiya o'chiriladi (brute-force himoya).
const max2FAAttempts = 5

// maxDevices — bitta foydalanuvchiga ruxsat etilgan aktiv qurilmalar soni.
const maxDevices = 10

// refreshReuseGrace — rotation poygasiga bag'rikenglik oynasi.
// Revoke qilingan refresh shu oyna ichida qayta ishlatilsa — poyga deb
// emas, O'G'IRLIK deb qaraladi: butun session oilasi revoke qilinib,
// keyin yangi juftlik beriladi (reuse-detection: hujumchi ham, jabrlanuvchi
// ham eski sessiyalardan mahrum bo'ladi, kimligi aniq bo'lmagani uchun).
const refreshReuseGrace = 30 * time.Second

// loginLimiter — per-username login throttle (parol brute-force/spray).
// Global per-IP limit (router) yetmaydi: hujum IP'lar bo'ylab taqsimlanishi
// mumkin. Bitta akkauntga daqiqasiga 10 urinish — legitimga yetadi.
var loginLimiter = security.NewRateLimiter(10, time.Minute)

type AuthService struct {
	cfg  *config.Config
	repo *repository.Repositories
}

func NewAuthService(cfg *config.Config, repo *repository.Repositories) *AuthService {
	return &AuthService{cfg: cfg, repo: repo}
}

type Claims struct {
	UserID   string `json:"sub"`
	Username string `json:"name"`
	DeviceID string `json:"dev"`
	jwt.RegisteredClaims
}

func (a *AuthService) Register(ctx context.Context, req model.RegisterRequest, ip, userAgent string) (*model.User, *AuthResult, error) {
	// Trim/lower-case avval: " bob " kabi input "bob" ga aylangach
	// validatsiya qilinadi, aks holda " ab " (4 belgi) sanitizatsiyadan
	// keyin 2 belgili bo'lib min-uzunlik cheklovini chetlab o'tardi.
	username := security.SanitizeUsername(req.Username)
	if err := security.ValidateUsername(username); err != nil {
		return nil, nil, err
	}
	if err := security.ValidatePassword(req.Password); err != nil {
		return nil, nil, err
	}
	display := security.SanitizeDisplayName(req.DisplayName)
	if display == "" {
		display = username
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), a.cfg.Security.BcryptCost)
	if err != nil {
		return nil, nil, err
	}

	u := &model.User{
		Username:     username,
		DisplayName:  display,
		PasswordHash: string(hash),
	}
	if err := a.repo.Users.Create(ctx, u); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, nil, ErrUserExists
		}
		return nil, nil, err
	}

	u, err = a.repo.Users.ByID(ctx, u.ID)
	if err != nil {
		return nil, nil, err
	}

	device, err := a.findOrCreateDevice(ctx, u.ID, ip, userAgent)
	if err != nil {
		return nil, nil, err
	}
	res, err := a.issueTokens(ctx, u, device.ID, ip, userAgent)
	return u, res, err
}

// dummyHash — foydalanuvchi topilmaganda ham bcrypt hisobini bajarib,
// login vaqtini tenglashtirish uchun (username enumeration timing himoyasi).
const dummyHash = "$2a$10$M7icKnBgaHpDxhkdy3m7fe35VQURQe8tMZP4G.y6AeC.usdfR2rVG"

func (a *AuthService) Login(ctx context.Context, req model.LoginRequest, ip, userAgent string) (*model.LoginResponse, error) {
	username := security.SanitizeUsername(req.Username)
	// Per-account throttle: parol taxminlash bitta akkauntga qaratilganda
	// IP limiti (120/min) to'xtatmaydi. Muvaffaqiyatsiz login ham token
	// sarflaydi — hujumchi 10/min dan tez tekshira olmaydi.
	if !loginLimiter.Allow("login:" + username) {
		return nil, ErrRateLimited
	}
	u, err := a.repo.Users.ByUsername(ctx, username)
	if err != nil {
		// Vaqtni tenglashtiramiz: noto'g'ri username ham xuddi
		// noto'g'ri parol kabi vaqt oladi.
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(req.Password))
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	if u.TwoFAEnabled {
		// 2FA yoqilgan bo'lsa device'ni OLDIN yaratmaymiz — aks holda parolni
		// bilgan (lekin TOTP'siz) hujumchi maxDevices'ni to'ldirib legitim
		// user'ga DoS qiladi. Device verify bosqichida yaratiladi.
		pendingID := uuid.NewString()
		a.storePending(u.ID, "", ip, userAgent, pendingID)
		return &model.LoginResponse{
			Status:         "2fa_required",
			PendingSession: pendingID,
		}, nil
	}

	// Device ni tayyorlaymiz (fingerprint asosida qayta ishlatish).
	device, err := a.findOrCreateDevice(ctx, u.ID, ip, userAgent)
	if err != nil {
		return nil, err
	}

	res, err := a.issueTokens(ctx, u, device.ID, ip, userAgent)
	if err != nil {
		return nil, err
	}
	return &model.LoginResponse{Status: "ok", Auth: res.ToModel(u)}, nil
}

// TwoFAVerify 2FA kodi tasdiqlangandan keyin token beradi.
// Noto'g'ri kod urinishlari pending başına max2FAAttempts, akkaunt başına
// maxUser2FAFails gacha cheklangan (yangi pending olib limitni chetlab
// o'tish bloklanadi). Bir kod ikki marta ishlatilmaydi (replay himoyasi).
func (a *AuthService) TwoFAVerify(ctx context.Context, pendingID, code string) (*AuthResult, error) {
	entry, ok := a.peekPending(pendingID)
	if !ok {
		return nil, ErrBadToken
	}
	secret, err := a.repo.TwoFA.GetSecret(ctx, entry.UserID)
	if err != nil {
		a.deletePending(pendingID)
		return nil, ErrTwoFAInvalid
	}
	if secret == "" {
		a.deletePending(pendingID)
		return nil, ErrTwoFAInvalid
	}
	if !validateTOTP(code, secret) {
		// Noto'g'ri kod — pending ham, akkaunt ham HAR DOIM sanaladi
		// (short-circuit bo'lmasligi uchun alohida). Biri to'lsa sessiya
		// o'chiriladi (qayta login talab qilinadi).
		userLocked := !a.noteUser2FAFail(entry.UserID)
		if a.incPendingAttempts(pendingID) >= max2FAAttempts || userLocked {
			a.deletePending(pendingID)
			return nil, ErrTwoFALocked
		}
		return nil, ErrTwoFAInvalid
	}
	if a.checkTOTPReplay(entry.UserID, code) {
		return nil, ErrTwoFAInvalid
	}
	a.markTOTPUsed(entry.UserID, code)
	a.resetUser2FAFail(entry.UserID)
	a.deletePending(pendingID)
	u, err := a.repo.Users.ByID(ctx, entry.UserID)
	if err != nil {
		return nil, err
	}
	device, err := a.findOrCreateDeviceByID(ctx, u.ID, entry.DeviceID, entry.IP, entry.UserAgent)
	if err != nil {
		return nil, err
	}
	return a.issueTokens(ctx, u, device.ID, entry.IP, entry.UserAgent)
}

func (a *AuthService) Refresh(ctx context.Context, refreshToken string) (*AuthResult, error) {
	hash := refreshHash(refreshToken)
	sess, err := a.repo.Sessions.ByRefreshHash(ctx, hash)
	if err != nil {
		return nil, ErrSessionExpired
	}
	if sess.RevokedAt != nil {
		if time.Since(*sess.RevokedAt) < refreshReuseGrace {
			// Revoke qilingan token qayta ishlatildi — o'g'irlik ehtimoli.
			// Oilani to'liq revoke qilib, keyin yangi juftlik beramiz:
			// hujum sessiyalari o'ladi, so'rov egasi yangi sessiya oladi.
			// (Eski xato: grace ichida yangi juftlik berilib, oila saqlanib
			// qolar edi — o'g'irlangan token sezilmasdi.)
			_ = a.repo.Sessions.RevokeAllForUser(ctx, sess.UserID)
			slog.Warn("refresh token reuse detected; family revoked", "user_id", sess.UserID)
		} else {
			// Oyna katta — aniq o'g'irlik: butun session oilasini revoke
			// qilamiz va yangi juftlik BERMAMIZ (qayta login shart).
			_ = a.repo.Sessions.RevokeAllForUser(ctx, sess.UserID)
			slog.Warn("refresh token reuse detected", "user_id", sess.UserID)
			return nil, ErrSessionExpired
		}
		if time.Now().After(sess.ExpiresAt) {
			return nil, ErrSessionExpired
		}
	} else if !sess.Valid() {
		return nil, ErrSessionExpired
	}
	u, err := a.repo.Users.ByID(ctx, sess.UserID)
	if err != nil {
		return nil, ErrSessionExpired
	}
	// O'chirilgan device'dagi session — qayta jonlanmaydi.
	if sess.DeviceID != "" {
		if d, derr := a.repo.Devices.ByID(ctx, sess.DeviceID); derr != nil || (d != nil && !d.IsActive) {
			_ = a.repo.Sessions.Revoke(ctx, sess.ID)
			return nil, ErrSessionExpired
		}
	}
	// Rotation: AVVAL yangi session yaratamiz, KEYIN eskisini revoke qilamiz.
	// Oradagi crash'da user logout bo'lib qolmaydi. Eski revoke muvaffaqiyatsiz
	// bo'lsa — yangisini ham revoke qilib xatolik qaytaramiz (ikkita valid
	// token qolmasligi uchun rollback).
	res, newSess, err := a.issueTokensWithSession(ctx, u, sess.DeviceID, sess.IP, sess.UserAgent)
	if err != nil {
		return nil, err
	}
	if err := a.repo.Sessions.Revoke(ctx, sess.ID); err != nil {
		slog.Warn("refresh rotation revoke", "err", err)
		_ = a.repo.Sessions.Revoke(ctx, newSess.ID)
		return nil, err
	}
	return res, nil
}

// Logout refresh session'ni bekor qiladi va foydalanuvchining barcha WS
// ulanishlarini uzadi — aks holda "chiqqan" foydalanuvchi eski socket orqali
// chatdan foydalanishda davom etardi (WS auth faqat ulanishda tekshiriladi).
func (a *AuthService) Logout(ctx context.Context, refreshToken, userID string, hub *ws.Hub) error {
	if refreshToken != "" {
		if sess, err := a.repo.Sessions.ByRefreshHash(ctx, refreshHash(refreshToken)); err == nil {
			if err := a.repo.Sessions.Revoke(ctx, sess.ID); err != nil {
				slog.Warn("logout revoke", "err", err)
			}
		}
	}
	if hub != nil && userID != "" {
		hub.ForceLogout(userID, "logged_out")
	}
	return nil
}

// ChangePassword parolni yangilaydi va barcha sessionlarni bekor qiladi.
// Dastur bu o'zgarishni tezkor sezadi: password_change_log + hub.ForceLogout.
func (a *AuthService) ChangePassword(ctx context.Context, userID, oldPwd, newPwd string, hub *ws.Hub) error {
	if err := security.ValidatePassword(newPwd); err != nil {
		return err
	}
	u, err := a.repo.Users.ByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(oldPwd)); err != nil {
		return ErrPasswordMismatch
	}
	if oldPwd == newPwd {
		return ErrSamePassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPwd), a.cfg.Security.BcryptCost)
	if err != nil {
		return err
	}
	if err := a.repo.Users.UpdatePassword(ctx, userID, string(hash)); err != nil {
		return err
	}
	if err := a.repo.Sessions.RevokeAllForUser(ctx, userID); err != nil {
		slog.Error("revoke sessions", "err", err)
	}
	if hub != nil {
		hub.ForceLogout(userID, "password_changed")
	}
	return nil
}

type AuthResult struct {
	AccessToken  string
	RefreshToken string
	UserID       string
}

func (r *AuthResult) ToModel(u *model.User) *model.AuthResponse {
	return &model.AuthResponse{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		User:         *u,
	}
}

func (a *AuthService) issueTokens(ctx context.Context, u *model.User, deviceID, ip, userAgent string) (*AuthResult, error) {
	res, _, err := a.issueTokensWithSession(ctx, u, deviceID, ip, userAgent)
	return res, err
}

// issueTokensWithSession — token juftligi + DB sessiyasini qaytaradi
// (rotation rollback uchun sessiya ID kerak).
func (a *AuthService) issueTokensWithSession(ctx context.Context, u *model.User, deviceID, ip, userAgent string) (*AuthResult, *model.Session, error) {
	access, err := a.signAccess(u, deviceID)
	if err != nil {
		return nil, nil, err
	}
	refresh, err := a.signRefresh()
	if err != nil {
		return nil, nil, err
	}

	s := &model.Session{
		UserID:      u.ID,
		DeviceID:    deviceID,
		RefreshHash: refreshHash(refresh),
		IP:          ip,
		UserAgent:   userAgent,
		ExpiresAt:   time.Now().Add(a.cfg.JWT.RefreshExpire),
	}
	if err := a.repo.Sessions.Create(ctx, s); err != nil {
		return nil, nil, err
	}
	return &AuthResult{AccessToken: access, RefreshToken: refresh, UserID: u.ID}, s, nil
}

func (a *AuthService) signAccess(u *model.User, deviceID string) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:   u.ID,
		Username: u.Username,
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    a.cfg.JWT.Issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(a.cfg.JWT.AccessExpire)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(a.cfg.JWT.Secret))
}

func (a *AuthService) signRefresh() (string, error) {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (a *AuthService) ValidateAccess(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(a.cfg.JWT.Secret), nil
	}, jwt.WithIssuer(a.cfg.JWT.Issuer), jwt.WithExpirationRequired())
	if err != nil {
		return nil, ErrBadToken
	}
	return claims, nil
}

func refreshHash(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Me — joriy foydalanuvchini qaytaradi.
func (a *AuthService) Me(ctx context.Context, userID string) (*model.User, error) {
	return a.repo.Users.ByID(ctx, userID)
}

// CheckWSAccess — WS ulanishda revocation tekshiruvi:
//  1. Device o'chirilgan (terminate) bo'lsa — rad.
//  2. Access token parol o'zgarishidan OLDIN berilgan bo'lsa — rad
//     (parol o'zgarganda session'lar revoke bo'ladi, lekin stateless JWT
//     15 daqiqa yashashi mumkin edi — shu oyna yopiladi).
func (a *AuthService) CheckWSAccess(ctx context.Context, userID, deviceID string, issuedAt time.Time) error {
	if deviceID != "" {
		if d, err := a.repo.Devices.ByID(ctx, deviceID); err != nil || (d != nil && (!d.IsActive || d.UserID != userID)) {
			return ErrSessionExpired
		}
	}
	if !issuedAt.IsZero() {
		if changed, err := a.repo.TwoFA.LatestPasswordChange(ctx, userID); err == nil && !changed.IsZero() {
			if issuedAt.Before(changed) {
				return ErrSessionExpired
			}
		}
	}
	return nil
}
