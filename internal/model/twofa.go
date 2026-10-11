package model

import "time"

type TwoFA struct {
	UserID    string
	Secret    string
	Enabled   bool
	PendingID string
}

type PasswordChange struct {
	ID        int64     `json:"id"`
	UserID    string    `json:"user_id"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}
