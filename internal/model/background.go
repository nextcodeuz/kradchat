package model

import "time"

type Background struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	FilePath  string    `json:"file_path"`
	URL       string    `json:"url"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type UserBackground struct {
	UserID       string    `json:"user_id"`
	BackgroundID string    `json:"background_id"`
	Room         string    `json:"room"`
	AppliedAt    time.Time `json:"applied_at"`
}
