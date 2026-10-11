package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"kradchat/internal/model"
	"kradchat/internal/repository"
	"kradchat/internal/ws"
)

// --- Qurilmalar (device tracking) ---

// findOrCreateDevice UA+IP dan turini aniqlaydi va device record'ini qaytaradi.
// Aktiv qurilmalar soni maxDevices chegarasiga yetgan bo'lsa yangi qurilma
// yaratilmaydi (UA manipulyatsiyasi orqali cheksiz session yaratish oldi).
func (a *AuthService) findOrCreateDevice(ctx context.Context, userID, ip, userAgent string) (*model.Device, error) {
	fp := deviceFingerprint(userAgent, ip)
	d, err := a.repo.Devices.ByFingerprint(ctx, userID, fp)
	if err == nil && d != nil && d.IsActive {
		d.IP = ip
		d.UserAgent = userAgent
		d.LastSeen = time.Now()
		_ = a.repo.Devices.Update(ctx, d)
		return d, nil
	}
	// Terminate qilingan (inactive) device QAYTA JONLANMAYDI — yangisi
	// yaratiladi (maxDevices chegarasi ichida). Aks holda foydalanuvchi
	// o'chirgan qurilma parolni bilgan odam tomonidan fingerprint takrorlab
	// qayta faollashtirilardi va audit izi yo'qolardi.
	n, err := a.repo.Devices.CountActive(ctx, userID)
	if err != nil {
		return nil, err
	}
	if n >= maxDevices {
		return nil, ErrTooManyDevices
	}
	d = &model.Device{
		UserID:      userID,
		IP:          ip,
		UserAgent:   userAgent,
		DeviceType:  deviceTypeOf(userAgent),
		Browser:     browserOf(userAgent),
		OS:          osOf(userAgent),
		Fingerprint: fp,
		IsActive:    true,
		LastSeen:    time.Now(),
	}
	if err := a.repo.Devices.Create(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// findOrCreateDeviceByID — 2FA login jarayonida oldindan yaratilgan device'ni
// qayta ishlatadi, yo'q bo'lsa yangisini yaratadi.
func (a *AuthService) findOrCreateDeviceByID(ctx context.Context, userID, deviceID, ip, userAgent string) (*model.Device, error) {
	if deviceID != "" {
		if d, err := a.repo.Devices.ByID(ctx, deviceID); err == nil && d != nil && d.UserID == userID && d.IsActive {
			d.IP = ip
			d.UserAgent = userAgent
			d.LastSeen = time.Now()
			_ = a.repo.Devices.Update(ctx, d)
			return d, nil
		}
		// Inactive (terminate qilingan) device qayta jonlanmaydi.
	}
	return a.findOrCreateDevice(ctx, userID, ip, userAgent)
}

func deviceFingerprint(ua, ip string) string {
	// UA yolg'iz kam entropiyali (bir xil brauzerli qurilmalar to'qnashadi),
	// shuning uchun IP (/24) ham aralashtiramiz. IP o'zgarsa yangi device
	// paydo bo'lishi mumkin — bu audit uchun xavfsizroq (birlashishdan ko'ra).
	netPart := ip
	if i := strings.LastIndex(ip, "."); i > 0 {
		netPart = ip[:i]
	} else if i := strings.LastIndex(ip, ":"); i > 0 {
		netPart = ip[:i]
	}
	sum := sha256.Sum256([]byte(ua + "|" + netPart))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:32]
}

// Devices — foydalanuvchining qurilmalari ro'yxati.
func (a *AuthService) Devices(ctx context.Context, userID string) ([]*model.Device, error) {
	return a.repo.Devices.ByUser(ctx, userID)
}

// TerminateDevice — boshqa qurilmani uzadi (sessionlar revoke + WS kick).
func (a *AuthService) TerminateDevice(ctx context.Context, userID, deviceID string, hub *ws.Hub) error {
	if err := a.repo.Devices.Deactivate(ctx, deviceID, userID); err != nil {
		return err
	}
	if err := a.repo.Sessions.RevokeDevice(ctx, deviceID); err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	if hub != nil {
		if c := hub.ClientByDeviceID(deviceID); c != nil {
			c.Send(&ws.ServerMsg{Type: ws.STypeKick, Reason: "terminated_from_settings"})
			go c.Close()
		}
	}
	return nil
}

func deviceTypeOf(ua string) string {
	u := strings.ToLower(ua)
	switch {
	case strings.Contains(u, "iphone") || strings.Contains(u, "android") || strings.Contains(u, "ipad") || strings.Contains(u, "mobile"):
		return "mobile"
	case strings.Contains(u, "ipad") || strings.Contains(u, "tablet"):
		return "tablet"
	default:
		return "desktop"
	}
}

func browserOf(ua string) string {
	u := strings.ToLower(ua)
	switch {
	case strings.Contains(u, "edg/"):
		return "Edge"
	case strings.Contains(u, "chrome"):
		return "Chrome"
	case strings.Contains(u, "firefox"):
		return "Firefox"
	case strings.Contains(u, "safari"):
		return "Safari"
	case strings.Contains(u, "opera"):
		return "Opera"
	default:
		return "Unknown"
	}
}

func osOf(ua string) string {
	u := strings.ToLower(ua)
	switch {
	case strings.Contains(u, "windows"):
		return "Windows"
	case strings.Contains(u, "mac os") || strings.Contains(u, "macintosh"):
		return "macOS"
	case strings.Contains(u, "android"):
		return "Android"
	case strings.Contains(u, "iphone") || strings.Contains(u, "ios"):
		return "iOS"
	case strings.Contains(u, "linux"):
		return "Linux"
	default:
		return "Unknown"
	}
}
