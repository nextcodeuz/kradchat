package service

import (
	"context"
	"errors"
	"strings"

	"kradchat/internal/config"
	"kradchat/internal/model"
	"kradchat/internal/repository"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const issuer = "KRADCHAT"

type TwoFAService struct {
	cfg  *config.Config
	repo *repository.Repositories
}

func NewTwoFAService(cfg *config.Config, repo *repository.Repositories) *TwoFAService {
	return &TwoFAService{cfg: cfg, repo: repo}
}

// Setup yangi TOTP secret yaratadi va QR URL qaytaradi (hali yoqilmaydi).
// Parol talab qilinadi — o'g'irlangan access token bilan 2FA egallab olish
// bloklanadi. 2FA allaqachon yoqilgan bo'lsa secret aylantirilmaydi.
func (t *TwoFAService) Setup(ctx context.Context, userID, username, password string) (*model.TwoFASetupResponse, error) {
	u, err := t.repo.Users.ByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.TwoFAEnabled {
		return nil, ErrTwoFAAlreadyEnabled
	}
	if !bcryptCompare(u.PasswordHash, password) {
		return nil, ErrPasswordMismatch
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: username,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, err
	}
	secret := key.Secret()
	otpauth := key.URL()
	qr, err := qrDataURL(otpauth)
	if err != nil {
		return nil, err
	}
	// Secret key'ni DB ga saqlaymiz (enable qilishgacha).
	if err := t.repo.TwoFA.SetEnabled(ctx, userID, false, secret); err != nil {
		return nil, err
	}
	return &model.TwoFASetupResponse{
		Secret:    secret,
		OTPAuth:   otpauth,
		QRDataURL: qr,
	}, nil
}

// Enable kodni ikki marta tekshirib yoqadi (xatolik ehtimolini kamaytiradi).
// Parol talab qilinadi — Setup'ni ham parolsiz chaqirib bo'lmagani uchun
// attack zanjiri (stolen token -> secret rotation -> takeover) uziladi.
func (t *TwoFAService) Enable(ctx context.Context, userID, code, password string) error {
	u, err := t.repo.Users.ByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.TwoFAEnabled {
		return ErrTwoFAAlreadyEnabled
	}
	if !bcryptCompare(u.PasswordHash, password) {
		return ErrPasswordMismatch
	}
	secret, err := t.repo.TwoFA.GetSecret(ctx, userID)
	if err != nil {
		return err
	}
	if secret == "" {
		return errors.New("no pending 2FA setup; call /twofa/setup first")
	}
	if !validateTOTP(code, secret) {
		return ErrTwoFAInvalid
	}
	return t.repo.TwoFA.SetEnabled(ctx, userID, true, secret)
}

// Disable parol + kod tekshirib o'chiradi (avval parol — kod-oracle oldini).
func (t *TwoFAService) Disable(ctx context.Context, userID, password, code string) error {
	u, err := t.repo.Users.ByID(ctx, userID)
	if err != nil {
		return err
	}
	if !u.TwoFAEnabled {
		return nil
	}
	if !bcryptCompare(u.PasswordHash, password) {
		return ErrPasswordMismatch
	}
	if !validateTOTP(code, u.TwoFASecret) {
		return ErrTwoFAInvalid
	}
	return t.repo.TwoFA.SetEnabled(ctx, userID, false, "")
}

// validateTOTP — RFC 6238 tasdiqlash (server taymer, ±1 step tolerance).
func validateTOTP(code, secret string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	ok, err := totp.ValidateCustom(code, secret, timeNow(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && ok
}

// LogPasswordChange — parol o'zgarishini audit log'ga yozadi.
func (t *TwoFAService) LogPasswordChange(ctx context.Context, userID, ip, userAgent string) error {
	return t.repo.TwoFA.LogPasswordChange(ctx, userID, ip, userAgent)
}
