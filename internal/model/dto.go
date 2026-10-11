package model

// API DTOlar (JSON body)

type RegisterRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type TwoFAVerifyRequest struct {
	PendingSession string `json:"pending_session"`
	Code           string `json:"code"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type PasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type TwoFADisableRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

type TwoFAEnableRequest struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

// TwoFASetupRequest — 2FA sozlash uchun parol talab qilinadi (stolen token
// bilan 2FA egallab olish bloklanadi).
type TwoFASetupRequest struct {
	Password string `json:"password"`
}

type UpdateMeRequest struct {
	DisplayName          *string `json:"display_name"`
	AvatarURL            *string `json:"avatar_url"`
	HideUsername         *bool   `json:"hide_username"`
	ShowLastSeen         *bool   `json:"show_last_seen"`
	ShowOnlineStatus     *bool   `json:"show_online_status"`
	NotificationsEnabled *bool   `json:"notifications_enabled"`
}

type AuthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         User   `json:"user"`
}

type LoginResponse struct {
	Status         string        `json:"status"` // "ok" | "2fa_required"
	PendingSession string        `json:"pending_session,omitempty"`
	Auth           *AuthResponse `json:"auth,omitempty"`
}

type UploadResponse struct {
	URL        string `json:"url"`
	PreviewURL string `json:"preview_url"`
	Size       int64  `json:"size"`
	Mime       string `json:"mime"`
	UploadID   string `json:"upload_id"`
	Complete   bool   `json:"complete"`
	Progress   int    `json:"progress"`
}

type ErrorResponse struct {
	Error  string `json:"error"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

type TwoFASetupResponse struct {
	Secret    string `json:"secret"`
	OTPAuth   string `json:"otpauth_uri"`
	QRDataURL string `json:"qr_data_url"`
}
