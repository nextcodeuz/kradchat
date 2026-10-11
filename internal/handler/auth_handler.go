package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"kradchat/internal/middleware"
	"kradchat/internal/model"
	"kradchat/internal/repository"
	"kradchat/internal/security"
	"kradchat/internal/service"
	"kradchat/internal/ws"
)

type AuthHandler struct {
	svc *service.Services
	hub *ws.Hub
}

func NewAuthHandler(svc *service.Services, hub *ws.Hub) *AuthHandler {
	return &AuthHandler{svc: svc, hub: hub}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req model.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	u, res, err := h.svc.Auth.Register(r.Context(), req, middleware.ClientIP(r), r.UserAgent())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, res.ToModel(u))
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req model.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	resp, err := h.svc.Auth.Login(r.Context(), req, middleware.ClientIP(r), r.UserAgent())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) TwoFAVerify(w http.ResponseWriter, r *http.Request) {
	var req model.TwoFAVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	res, err := h.svc.Auth.TwoFAVerify(r.Context(), req.PendingSession, req.Code)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	user, err := h.svc.Auth.Me(r.Context(), res.UserID)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, res.ToModel(user))
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req model.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	res, err := h.svc.Auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	user, err := h.svc.Auth.Me(r.Context(), res.UserID)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, res.ToModel(user))
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req model.RefreshRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	// Logout'da WS ulanish ham uziladi (server tomondan kick) — aks holda
	// chiqqan foydalanuvchi eski socket orqali chatda qolardi.
	if err := h.svc.Auth.Logout(r.Context(), req.RefreshToken, middleware.GetUserID(r), h.hub); err != nil {
		slog.Warn("logout", "err", err)
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *AuthHandler) Password(w http.ResponseWriter, r *http.Request) {
	var req model.PasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	userID := middleware.GetUserID(r)
	// parol o'zgarishini tezkor sezish: log + barcha sessionlarni revoke + hub.ForceLogout
	if err := h.svc.Auth.ChangePassword(r.Context(), userID, req.OldPassword, req.NewPassword, h.hub); err != nil {
		writeServiceErr(w, err)
		return
	}
	_ = h.svc.TwoFA.LogPasswordChange(r.Context(), userID, middleware.ClientIP(r), r.UserAgent())
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *AuthHandler) TwoFASetup(w http.ResponseWriter, r *http.Request) {
	var req model.TwoFASetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	userID := middleware.GetUserID(r)
	username := middleware.GetUsername(r)
	resp, err := h.svc.TwoFA.Setup(r.Context(), userID, username, req.Password)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) TwoFAEnable(w http.ResponseWriter, r *http.Request) {
	var req model.TwoFAEnableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := h.svc.TwoFA.Enable(r.Context(), middleware.GetUserID(r), req.Code, req.Password); err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *AuthHandler) TwoFADisable(w http.ResponseWriter, r *http.Request) {
	var req model.TwoFADisableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	if err := h.svc.TwoFA.Disable(r.Context(), middleware.GetUserID(r), req.Password, req.Code); err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	u, err := h.svc.Auth.Me(r.Context(), middleware.GetUserID(r))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, u)
}

// SearchUser — username yoki ID bo'yicha foydalanuvchi qidirish.
func (h *AuthHandler) SearchUser(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "q parametri kerak")
		return
	}
	u, err := h.svc.Auth.SearchUser(r.Context(), middleware.GetUserID(r), q, h.svc.Presence, h.svc.Blocks)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			middleware.WriteError(w, http.StatusNotFound, "not_found", "Foydalanuvchi topilmadi")
			return
		}
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, u)
}

// Conversations — Shaxsiy (DM) suhbatlar ro'yxati.
func (h *AuthHandler) Conversations(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Auth.Conversations(r.Context(), middleware.GetUserID(r), h.svc.Presence, h.svc.Blocks)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"conversations": list})
}

func (h *AuthHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	var req model.UpdateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	u, err := h.svc.Auth.UpdateMe(r.Context(), middleware.GetUserID(r), req)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	// show_online_status o'zgargan bo'lishi mumkin — presence kesh tozalanadi.
	h.svc.Presence.InvalidateShowOnline(middleware.GetUserID(r))
	middleware.WriteJSON(w, http.StatusOK, u)
}

// MeAvatar — profil rasmini yuklaydi. Body: raw rasm baytlari.
// Rasm `uploads/avatars/` papkasiga saqlanib, avatar_url yangilanadi.
func (h *AuthHandler) MeAvatar(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 6*1024*1024))
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "read_error", "failed to read body")
		return
	}
	url, err := h.svc.Upload.SaveAvatar(middleware.GetUserID(r), data)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	u, err := h.svc.Auth.UpdateMe(r.Context(), middleware.GetUserID(r), model.UpdateMeRequest{AvatarURL: &url})
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, u)
}

func (h *AuthHandler) Devices(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Auth.Devices(r.Context(), middleware.GetUserID(r))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]any{"devices": list})
}

func (h *AuthHandler) TerminateDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("id")
	userID := middleware.GetUserID(r)
	if err := h.svc.Auth.TerminateDevice(r.Context(), userID, deviceID, h.hub); err != nil {
		writeServiceErr(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeServiceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, security.ErrUsernameTooShort), errors.Is(err, security.ErrUsernameTooLong),
		errors.Is(err, security.ErrUsernameInvalid), errors.Is(err, security.ErrPasswordTooShort),
		errors.Is(err, security.ErrPasswordTooLong), errors.Is(err, security.ErrPasswordWeak),
		errors.Is(err, security.ErrRoomInvalid), errors.Is(err, security.ErrContentTooLong),
		errors.Is(err, security.ErrBadID), errors.Is(err, security.ErrClientMsgID),
		errors.Is(err, security.ErrMessageType), errors.Is(err, security.ErrFileURL):
		middleware.WriteError(w, http.StatusBadRequest, "validation", err.Error())
	case errors.Is(err, service.ErrUserExists):
		middleware.WriteError(w, http.StatusConflict, "user_exists", "username already taken")
	case errors.Is(err, service.ErrInvalidCredentials), errors.Is(err, service.ErrPasswordMismatch):
		middleware.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
	case errors.Is(err, service.ErrNotOwnFile):
		middleware.WriteError(w, http.StatusForbidden, "not_own_file", err.Error())
	case errors.Is(err, service.ErrSamePassword):
		middleware.WriteError(w, http.StatusBadRequest, "validation", err.Error())
	case errors.Is(err, service.ErrTwoFAInvalid):
		middleware.WriteError(w, http.StatusUnauthorized, "invalid_2fa", "invalid two-factor code")
	case errors.Is(err, service.ErrTwoFALocked):
		middleware.WriteError(w, http.StatusTooManyRequests, "2fa_locked", err.Error())
	case errors.Is(err, service.ErrTwoFAAlreadyEnabled):
		middleware.WriteError(w, http.StatusConflict, "twofa_enabled", err.Error())
	case errors.Is(err, service.ErrTooManyDevices):
		middleware.WriteError(w, http.StatusTooManyRequests, "too_many_devices", err.Error())
	case errors.Is(err, service.ErrRateLimited):
		middleware.WriteError(w, http.StatusTooManyRequests, "rate_limited", err.Error())
	case errors.Is(err, service.ErrTooManyUploads):
		middleware.WriteError(w, http.StatusTooManyRequests, "too_many_uploads", err.Error())
	case errors.Is(err, service.ErrBadToken), errors.Is(err, service.ErrSessionExpired):
		middleware.WriteError(w, http.StatusUnauthorized, "invalid_token", "invalid or expired token")
	case errors.Is(err, repository.ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, repository.ErrConflict):
		middleware.WriteError(w, http.StatusConflict, "conflict", "resource conflict")
	case errors.Is(err, service.ErrFileTooBig), errors.Is(err, security.ErrFileTooBig):
		middleware.WriteError(w, http.StatusRequestEntityTooLarge, "file_too_large", "file too large")
	case errors.Is(err, service.ErrBadFilename):
		middleware.WriteError(w, http.StatusBadRequest, "bad_filename", err.Error())
	case errors.Is(err, service.ErrInvalidFile):
		middleware.WriteError(w, http.StatusBadRequest, "invalid_file", "invalid file type")
	case errors.Is(err, service.ErrChunkOrder):
		middleware.WriteError(w, http.StatusBadRequest, "chunk_error", "chunk out of order")
	default:
		slog.Error("handler error", "err", err)
		middleware.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
