package model

import "time"

type Session struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	DeviceID    string     `json:"device_id"`
	RefreshHash string     `json:"-"`
	IP          string     `json:"ip"`
	UserAgent   string     `json:"user_agent"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type PendingSession struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
}

func (s Session) Valid() bool {
	return s.RevokedAt == nil && time.Now().Before(s.ExpiresAt)
}
