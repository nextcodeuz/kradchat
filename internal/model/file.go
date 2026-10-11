package model

import "time"

// FileRecord — yuklangan fayl registry yozuvi (egalik, ro'yxat, o'chirish,
// orphan GC uchun). URL'lar `/static/uploads/...` formatida.
type FileRecord struct {
	ID         string    `json:"id"`
	UploadID   string    `json:"upload_id"`
	OwnerID    string    `json:"owner_id"`
	Filename   string    `json:"filename"`
	Mime       string    `json:"mime"`
	Size       int64     `json:"size"`
	URL        string    `json:"url"`
	PreviewURL string    `json:"preview_url,omitempty"`
	Kind       string    `json:"kind"` // chat | avatar
	CreatedAt  time.Time `json:"created_at"`
}
