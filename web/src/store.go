//go:build js && wasm

package main

// App holati — sessiya, foydalanuvchi, xabarlar, onlayn ro'yxat.

type User struct {
	ID                   string `json:"id"`
	Username             string `json:"username"`
	DisplayName          string `json:"display_name"`
	AvatarURL            string `json:"avatar_url"`
	HideUsername         bool   `json:"hide_username"`
	ShowLastSeen         bool   `json:"show_last_seen"`
	ShowOnlineStatus     bool   `json:"show_online_status"`
	NotificationsEnabled bool   `json:"notifications_enabled"`
	TwoFAEnabled         bool   `json:"twofa_enabled"`
	Verified             bool   `json:"verified"`
	LastSeen             string `json:"last_seen"`
	CreatedAt            string `json:"created_at"`
}

// WS xabarlar payload'lariga mos.
type Msg struct {
	ID          int64    `json:"id"`
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	AvatarURL   string   `json:"avatar_url"`
	Room        string   `json:"room"`
	Content     string   `json:"content"`
	Type        string   `json:"type"`
	FileURL     string   `json:"file_url"`
	PreviewURL  string   `json:"preview_url"`
	FileSize    int64    `json:"file_size"`
	ClientMsgID string   `json:"client_msg_id"`
	ReplyTo     int64    `json:"reply_to"`
	Mentions    []string `json:"mentions"`
	Verified    bool     `json:"verified"`
	CreatedAt   int64    `json:"created_at"`

	// Reply quote (server to'ldiradi).
	ReplyUserID      string `json:"reply_user_id"`
	ReplyUsername    string `json:"reply_username"`
	ReplyDisplayName string `json:"reply_display_name"`
	ReplyContent     string `json:"reply_content"`
	ReplyType        string `json:"reply_type"`
	ReplyDeleted     bool   `json:"reply_deleted"`
}

type OnlineUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Devices     int    `json:"devices"`
	Online      bool   `json:"online"`
	LastSeen    int64  `json:"last_seen"`
	Verified    bool   `json:"verified"`
}

type BG struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	URL      string `json:"url"`
}

type Notif struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Room   string `json:"room"`
	UserID string `json:"user_id"`
}

type ServerMsg struct {
	Type        string        `json:"type"`
	Message     *Msg          `json:"message"`
	Messages    []*Msg        `json:"messages"`
	Cursor      int64         `json:"cursor"`
	Room        string        `json:"room"`
	MessageID   int64         `json:"message_id"`
	ClientMsgID string        `json:"client_msg_id"`
	User        *OnlineUser   `json:"user"`
	Users       []*OnlineUser `json:"users"`
	UserID      string        `json:"user_id"`
	Devices     int           `json:"devices"`
	Online      bool          `json:"online"`
	Background  string        `json:"background_id"`
	Backgrounds []*BG         `json:"backgrounds"`
	Notify      *Notif        `json:"notify"`
	Reason      string        `json:"reason"`
	Code        string        `json:"code"`
	Error       string        `json:"error"`
	TypingOn    bool          `json:"on"`
}

type Device struct {
	ID         string `json:"id"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
	DeviceType string `json:"device_type"`
	Browser    string `json:"browser"`
	OS         string `json:"os"`
	IsActive   bool   `json:"is_active"`
	LastSeen   string `json:"last_seen"`
	CreatedAt  string `json:"created_at"`
}

type Store struct {
	AccessToken  string
	RefreshToken string
	User         *User
	Pending      string // 2FA pending_session

	Messages  []*Msg
	Users     map[string]*OnlineUser // id -> user
	ByUser    map[string]string      // username -> id
	BgList    []*BG
	BgCurrent string

	CurRoom string
}

func NewStore() *Store {
	return &Store{
		Users:   make(map[string]*OnlineUser),
		ByUser:  make(map[string]string),
		CurRoom: "global",
	}
}

var store = NewStore()

// DisplayName — ko'rsatiladigan ism.
func (s *Store) DisplayName(u *OnlineUser) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return "???"
}
