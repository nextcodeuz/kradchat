package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"kradchat/internal/model"
	"kradchat/internal/repository"
	"kradchat/internal/security"
)

// --- Katalog: qidiruv, suhbatlar, profil ---

// SeedVerified — VERIFIED_USERS ro'yxatidagi usernamllarni startup'da
// verified (tasdiqlangan) deb belgilaydi.
func (a *AuthService) SeedVerified(ctx context.Context) {
	for _, uname := range a.cfg.VerifiedUsers {
		u, err := a.repo.Users.ByUsername(ctx, security.SanitizeUsername(uname))
		if err != nil {
			continue
		}
		if u.Verified {
			continue
		}
		if err := a.repo.Users.SetVerified(ctx, u.ID, true); err != nil {
			slog.Warn("seed verified", "user", uname, "err", err)
			continue
		}
		slog.Info("verified user", "username", uname)
	}
}

// SearchUser — username yoki user ID bo'yicha boshqa foydalanuvchini qidiradi.
// Maxfiylik sozlamalariga rioya qiladi:
//   - hide_username — username ko'rsatilmaydi
//   - show_online_status=false — onlayn holat ko'rsatilmaydi
//   - show_last_seen=false — oxirgi tashrif ko'rsatilmaydi
func (a *AuthService) SearchUser(ctx context.Context, viewerID, q string, presence *PresenceService, blocks *BlockService) (*model.PublicUser, error) {
	q = strings.TrimSpace(q)
	q = strings.TrimPrefix(q, "@")
	if q == "" {
		return nil, ErrUserNotFound
	}
	var (
		u   *model.User
		err error
	)
	if isUUIDString(q) {
		u, err = a.repo.Users.ByID(ctx, q)
	} else {
		u, err = a.repo.Users.ByUsername(ctx, security.SanitizeUsername(q))
	}
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	pub := &model.PublicUser{
		ID:          u.ID,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarURL,
		Verified:    u.Verified,
	}
	if blocks != nil {
		pub.Blocked = blocks.Blocks(viewerID, u.ID)
		pub.BlockedMe = blocks.Blocks(u.ID, viewerID)
	}
	if !u.HideUsername {
		pub.Username = u.Username
	}
	if u.ShowOnlineStatus || u.ShowLastSeen {
		snap := presence.UserSnapshot(u.ID)
		if u.ShowOnlineStatus {
			pub.Online = snap.Online
			pub.Devices = snap.Devices
		}
		if u.ShowLastSeen {
			if snap.LastSeen > 0 {
				pub.LastSeen = snap.LastSeen
			} else if !u.LastSeen.IsZero() {
				pub.LastSeen = u.LastSeen.UnixMilli()
			}
		}
	}
	return pub, nil
}

// Conversations — joriy foydalanuvchining barcha shaxsiy (DM) suhbatlarini
// oxirgi xabari va suhbatdosh ma'lumotlari bilan qaytaradi (Shaxsiy ro'yxati).
func (a *AuthService) Conversations(ctx context.Context, viewerID string, presence *PresenceService, blocks *BlockService) ([]*model.Conversation, error) {
	items, err := a.repo.Messages.Conversations(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	out := make([]*model.Conversation, 0, len(items))
	// N+1 oldini: user'larni bir marta keshlaymiz (bir peer bir marta).
	userCache := make(map[string]*model.User, len(items))
	for _, it := range items {
		peerID := security.DMRoomPeer(it.Room, viewerID)
		if peerID == "" {
			continue
		}
		u, ok := userCache[peerID]
		if !ok {
			u, err = a.repo.Users.ByID(ctx, peerID)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					continue
				}
				return nil, err
			}
			userCache[peerID] = u
		}
		c := &model.Conversation{
			Room:        it.Room,
			PeerID:      peerID,
			DisplayName: u.DisplayName,
			AvatarURL:   u.AvatarURL,
			Verified:    u.Verified,
			LastContent: it.Content,
			LastType:    it.Type,
			LastTime:    it.Time.UnixMilli(),
		}
		if blocks != nil {
			c.Blocked = blocks.Blocks(viewerID, peerID)
			c.BlockedMe = blocks.Blocks(peerID, viewerID)
		}
		if !u.HideUsername {
			c.Username = u.Username
		}
		if u.ShowOnlineStatus || u.ShowLastSeen {
			snap := presence.UserSnapshot(peerID)
			if u.ShowOnlineStatus {
				c.Online = snap.Online
				c.Devices = snap.Devices
			}
			if u.ShowLastSeen {
				if snap.LastSeen > 0 {
					c.LastSeen = snap.LastSeen
				} else if !u.LastSeen.IsZero() {
					c.LastSeen = u.LastSeen.UnixMilli()
				}
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// isUUIDString — oddiy UUID format tekshiruvi (36 belgi, defislar).
func isUUIDString(s string) bool {
	if len(s) != 36 {
		return false
	}
	for _, c := range s {
		if c == '-' {
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// UpdateMe — profil sozlamalarini yangilaydi.
func (a *AuthService) UpdateMe(ctx context.Context, userID string, req model.UpdateMeRequest) (*model.User, error) {
	u, err := a.repo.Users.ByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if req.DisplayName != nil {
		u.DisplayName = security.SanitizeDisplayName(*req.DisplayName)
		if u.DisplayName == "" {
			u.DisplayName = u.Username
		}
	}
	if req.AvatarURL != nil {
		if !security.ValidateAvatarURL(*req.AvatarURL) {
			return nil, security.ErrFileURL
		}
		u.AvatarURL = *req.AvatarURL
	}
	if req.HideUsername != nil {
		u.HideUsername = *req.HideUsername
	}
	if req.ShowLastSeen != nil {
		u.ShowLastSeen = *req.ShowLastSeen
	}
	if req.ShowOnlineStatus != nil {
		u.ShowOnlineStatus = *req.ShowOnlineStatus
	}
	if req.NotificationsEnabled != nil {
		u.NotificationsEnabled = *req.NotificationsEnabled
	}
	if err := a.repo.Users.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}
