package model

import "time"

const (
	MsgTypeText   = "text"
	MsgTypeImage  = "image"
	MsgTypeVideo  = "video"
	MsgTypeFile   = "file"
	MsgTypeEmoji  = "emoji"
	MsgTypeSystem = "system"
	RoomGlobal    = "global"
)

type Message struct {
	ID          int64      `json:"id"`
	UserID      string     `json:"user_id"`
	Username    string     `json:"username,omitempty"`
	DisplayName string     `json:"display_name,omitempty"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	Room        string     `json:"room"`
	Content     string     `json:"content"`
	Type        string     `json:"type"`
	FileURL     string     `json:"file_url,omitempty"`
	PreviewURL  string     `json:"preview_url,omitempty"`
	FileSize    int64      `json:"file_size,omitempty"`
	ClientMsgID string     `json:"client_msg_id"`
	ReplyTo     int64      `json:"reply_to,omitempty"`
	Mentions    []string   `json:"mentions,omitempty"`
	Verified    bool       `json:"verified"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type MessageInput struct {
	ClientMsgID string   `json:"client_msg_id"`
	Room        string   `json:"room"`
	Content     string   `json:"content"`
	Type        string   `json:"type"`
	FileURL     string   `json:"file_url"`
	PreviewURL  string   `json:"preview_url"`
	FileSize    int64    `json:"file_size"`
	ReplyTo     int64    `json:"reply_to"`
	Mentions    []string `json:"mentions"`
}

// Conversation — Shaxsiy (DM) suhbatlar ro'yxatidagi bitta yozuv.
type Conversation struct {
	Room        string `json:"room"`
	PeerID      string `json:"peer_id"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Online      bool   `json:"online"`
	Devices     int    `json:"devices,omitempty"`
	LastSeen    int64  `json:"last_seen,omitempty"`
	Verified    bool   `json:"verified"`
	Blocked     bool   `json:"blocked"`
	BlockedMe   bool   `json:"blocked_me"`
	LastContent string `json:"last_content"`
	LastType    string `json:"last_type"`
	LastTime    int64  `json:"last_time"`
}
