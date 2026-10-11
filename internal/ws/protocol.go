package ws

// WebSocket protokol xabar turlari (client<->server).

type ClientMsg struct {
	Type        string   `json:"type"`
	Token       string   `json:"token,omitempty"`
	Room        string   `json:"room,omitempty"`
	Content     string   `json:"content,omitempty"`
	ClientMsgID string   `json:"client_msg_id,omitempty"`
	MsgType     string   `json:"msg_type,omitempty"`
	FileURL     string   `json:"file_url,omitempty"`
	PreviewURL  string   `json:"preview_url,omitempty"`
	FileSize    int64    `json:"file_size,omitempty"`
	ReplyTo     int64    `json:"reply_to,omitempty"`
	Mentions    []string `json:"mentions,omitempty"`
	Background  string   `json:"background_id,omitempty"`
	MessageID   int64    `json:"message_id,omitempty"`
	On          *bool    `json:"on,omitempty"`
	Cursor      int64    `json:"cursor,omitempty"`
}

const (
	// client -> server
	CTypeAuth    = "auth"
	CTypeMsg     = "msg"
	CTypeTyping  = "typing"
	CTypeBG      = "bg"
	CTypeHistory = "history"
	CTypeDelete  = "delete"

	// server -> client
	STypeReady       = "ready"
	STypeMsg         = "msg"
	STypeHistory     = "history"
	STypeOnline      = "online"
	STypePresence    = "presence"
	STypeTyping      = "typing"
	STypeBGList      = "bg_list"
	STypeBGSet       = "bg_set"
	STypeNotif       = "notif"
	STypeDeleted     = "deleted"
	STypeAck         = "ack"
	STypeForceLogout = "force_logout"
	STypeKick        = "kick"
	STypeError       = "error"
	STypeOK          = "ok"
)

// ServerMsg — server->client universal wrapper.
type ServerMsg struct {
	Type        string         `json:"type"`
	Message     *MsgPayload    `json:"message,omitempty"`
	Messages    []*MsgPayload  `json:"messages,omitempty"`
	Cursor      int64          `json:"cursor,omitempty"`
	Room        string         `json:"room,omitempty"`
	MessageID   int64          `json:"message_id,omitempty"`
	ClientMsgID string         `json:"client_msg_id,omitempty"`
	User        *UserPayload   `json:"user,omitempty"`
	Users       []*UserPayload `json:"users,omitempty"`
	UserID      string         `json:"user_id,omitempty"`
	Devices     int            `json:"devices,omitempty"`
	Online      bool           `json:"online,omitempty"`
	Background  string         `json:"background_id,omitempty"`
	Backgrounds []*BGPayload   `json:"backgrounds,omitempty"`
	Notify      *NotifPayload  `json:"notify,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	Code        string         `json:"code,omitempty"`
	Error       string         `json:"error,omitempty"`
	TypingOn    bool           `json:"on,omitempty"`
}

type MsgPayload struct {
	ID          int64    `json:"id"`
	UserID      string   `json:"user_id"`
	Username    string   `json:"username,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	AvatarURL   string   `json:"avatar_url,omitempty"`
	Room        string   `json:"room"`
	Content     string   `json:"content"`
	Type        string   `json:"type"`
	FileURL     string   `json:"file_url,omitempty"`
	PreviewURL  string   `json:"preview_url,omitempty"`
	FileSize    int64    `json:"file_size,omitempty"`
	ClientMsgID string   `json:"client_msg_id"`
	ReplyTo     int64    `json:"reply_to,omitempty"`
	Mentions    []string `json:"mentions,omitempty"`
	Verified    bool     `json:"verified"`
	CreatedAt   int64    `json:"created_at"` // unix ms

	// Reply quote (reply qilingan original xabarning ko'chirmasi).
	ReplyUserID   string `json:"reply_user_id,omitempty"`
	ReplyUsername string `json:"reply_username,omitempty"`
	ReplyDispName string `json:"reply_display_name,omitempty"`
	ReplyContent  string `json:"reply_content,omitempty"`
	ReplyType     string `json:"reply_type,omitempty"`
	ReplyDeleted  bool   `json:"reply_deleted,omitempty"`
}

type UserPayload struct {
	ID          string `json:"id"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Devices     int    `json:"devices,omitempty"`
	Online      bool   `json:"online"`
	LastSeen    int64  `json:"last_seen,omitempty"`
	Verified    bool   `json:"verified"`
	// ShowOnline — ichki marshrut bayrog'i ( Preferences ): false bo'lsa
	// online holat e'lon qilinmaydi. JSON'ga chiqmaydi.
	ShowOnline bool `json:"-"`
}

type BGPayload struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	URL      string `json:"url"`
}

type NotifPayload struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Room   string `json:"room,omitempty"`
	UserID string `json:"user_id,omitempty"`
}
