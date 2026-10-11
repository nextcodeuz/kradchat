package model

import "time"

type User struct {
	ID                   string    `json:"id"`
	Username             string    `json:"username"`
	DisplayName          string    `json:"display_name"`
	PasswordHash         string    `json:"-"`
	AvatarURL            string    `json:"avatar_url,omitempty"`
	HideUsername         bool      `json:"hide_username"`
	ShowLastSeen         bool      `json:"show_last_seen"`
	ShowOnlineStatus     bool      `json:"show_online_status"`
	NotificationsEnabled bool      `json:"notifications_enabled"`
	Verified             bool      `json:"verified"`
	TwoFAEnabled         bool      `json:"twofa_enabled"`
	TwoFASecret          string    `json:"-"`
	LastSeen             time.Time `json:"last_seen"`
	CreatedAt            time.Time `json:"created_at"`
}

func (u User) PublicID() string {
	if u.HideUsername {
		return u.ID
	}
	return u.Username
}

// PublicUser — boshqa foydalanuvchilar ko'radigan ommaviy profil.
// Maxfiylik sozlamalari (hide_username, show_last_seen, show_online_status)
// qo'llanilgan holda to'ldiriladi.
type PublicUser struct {
	ID          string `json:"id"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Online      bool   `json:"online"`
	Devices     int    `json:"devices,omitempty"`
	LastSeen    int64  `json:"last_seen,omitempty"`
	Verified    bool   `json:"verified"`
	// Blocked — joriy foydalanuvchi bu foydalanuvchini bloklagan.
	Blocked bool `json:"blocked"`
	// BlockedMe — bu foydalanuvchi joriy foydalanuvchini bloklagan.
	BlockedMe bool `json:"blocked_me"`
}
