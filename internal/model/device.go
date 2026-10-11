package model

import "time"

type Device struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	IP          string    `json:"ip"`
	UserAgent   string    `json:"user_agent"`
	DeviceType  string    `json:"device_type"`
	Browser     string    `json:"browser"`
	OS          string    `json:"os"`
	Fingerprint string    `json:"-"`
	IsActive    bool      `json:"is_active"`
	LastSeen    time.Time `json:"last_seen"`
	CreatedAt   time.Time `json:"created_at"`
}
