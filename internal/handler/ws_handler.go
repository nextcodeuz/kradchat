package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/middleware"
	"kradchat/internal/model"
	"kradchat/internal/security"
	"kradchat/internal/service"
	"kradchat/internal/ws"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// CheckOrigin har so'rovda h.checkOrigin orqali o'rnatiladi
	// (xavfsizlik: faqat ruxsat etilgan Origin'lar).
}

type WSHandler struct {
	svc  *service.Services
	hub  *ws.Hub
	auth *service.AuthService

	msgRate  *security.RateLimiter
	typing   *security.RateLimiter
	histRate *security.RateLimiter
	delRate  *security.RateLimiter
	bgRate   *security.RateLimiter
	// frameRate — noto'g'ri/noma'lum freymlar limiteri (F6): bad_message /
	// unknown_type javoblari har freymga qaytarilmasin (kichik amplifikatsiya).
	frameRate   *security.RateLimiter
	checkOrigin func(*http.Request) bool
}

func NewWSHandler(cfg *config.Config, svc *service.Services, hub *ws.Hub, auth *service.AuthService, checkOrigin func(*http.Request) bool) *WSHandler {
	if checkOrigin == nil {
		checkOrigin = func(*http.Request) bool { return true }
	}
	// WS_MSG_BURST endi haqiqatdan ishlaydi (avval dead setting edi).
	ws.SetMaxMessageSize(cfg.Security.MaxMessageSize)
	return &WSHandler{
		svc:         svc,
		hub:         hub,
		auth:        auth,
		msgRate:     security.NewRateLimiterBurst(float64(cfg.Security.WsMsgPerMin), time.Minute, float64(cfg.Security.WsMsgBurst)),
		typing:      security.NewRateLimiter(20, time.Minute),
		histRate:    security.NewRateLimiter(30, time.Minute),
		delRate:     security.NewRateLimiter(30, time.Minute),
		bgRate:      security.NewRateLimiter(20, time.Minute),
		frameRate:   security.NewRateLimiter(20, time.Minute),
		checkOrigin: checkOrigin,
	}
}

// SweepLimiters — WS rate limiter xotirasi cheksiz o'smasligi uchun.
func (h *WSHandler) SweepLimiters() {
	h.msgRate.GCSweep()
	h.typing.GCSweep()
	h.histRate.GCSweep()
	h.delRate.GCSweep()
	h.bgRate.GCSweep()
	h.frameRate.GCSweep()
}

// Serve — /ws endpoint. Token query parametrda.
func (h *WSHandler) Serve(w http.ResponseWriter, r *http.Request) {
	if !h.checkOrigin(r) {
		middleware.WriteError(w, http.StatusForbidden, "forbidden", "origin not allowed")
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		middleware.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing token")
		return
	}
	claims, err := h.auth.ValidateAccess(token)
	if err != nil {
		middleware.WriteError(w, http.StatusUnauthorized, "invalid_token", "invalid token")
		return
	}

	u, err := h.svc.Auth.Me(r.Context(), claims.UserID)
	if err != nil {
		middleware.WriteError(w, http.StatusUnauthorized, "invalid_token", "user not found")
		return
	}

	// Revocation: o'chirilgan device yoki parol o'zgarishidan oldingi token
	// bilan qayta ulanish rad etiladi (15 daqiqalik oyna yopiladi).
	var issuedAt time.Time
	if claims.IssuedAt != nil {
		issuedAt = claims.IssuedAt.Time
	}
	if err := h.auth.CheckWSAccess(r.Context(), claims.UserID, claims.DeviceID, issuedAt); err != nil {
		middleware.WriteError(w, http.StatusUnauthorized, "invalid_token", "session revoked")
		return
	}

	uptr := upgrader
	uptr.CheckOrigin = h.checkOrigin
	conn, err := uptr.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("ws upgrade", "err", err)
		return
	}

	client := ws.NewClient(conn, claims.DeviceID)
	client.UserID = u.ID
	client.Username = u.Username
	client.DisplayName = u.DisplayName
	client.AvatarURL = u.AvatarURL
	client.Verified = u.Verified
	client.NotifyOn = u.NotificationsEnabled

	h.svc.Presence.SetSnapshotMetadata(client)
	client.SetOnMessage(func(c *ws.Client, raw []byte) {
		h.dispatch(c, raw)
	})

	h.hub.Join(client, model.RoomGlobal)
	h.svc.Presence.MarkOnline(client)
	client.Start()

	// ready + history + online list + bg list
	h.sendReady(client)
	// Tozalash faqat ulanish HAQIQATDA yopilganda ishlaydi.
	// r.Context().Done() dan foydalanish xato — Go'da request context
	// ServeHTTP qaytishi bilanoq bekor qilinadi (hijacked ulanishlarda ham),
	// shuning uchun WS darhol o'chib ketardi.
	go func() {
		<-client.Done()
		h.cleanup(client)
	}()
}

func (h *WSHandler) dispatch(c *ws.Client, raw []byte) {
	var msg ws.ClientMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		// Noto'g'ri freymlar ham limitlanadi — aks holda bitta ulanishdan
		// 64KB'lik axlat yog'dirib har biriga error javob oldirish mumkin.
		if h.frameRate.Allow("f:" + c.UserID) {
			c.SendError("bad_message", "invalid JSON")
		}
		return
	}

	// typing juda tez-tez keladi — uni log qilmaymiz, qolganlari ko'rinadi.
	if msg.Type != ws.CTypeTyping {
		slog.Info("ws frame",
			"user", c.Username, "type", msg.Type,
			"room", msg.Room, "content_len", len(msg.Content))
	}

	// size limit gorilla tomondan o'rnatiladi (readPump SetReadLimit).
	switch msg.Type {
	case ws.CTypeMsg:
		h.handleMsg(c, msg)
	case ws.CTypeTyping:
		h.handleTyping(c, msg)
	case ws.CTypeBG:
		h.handleBG(c, msg)
	case ws.CTypeHistory:
		h.handleHistory(c, msg)
	case ws.CTypeDelete:
		h.handleDelete(c, msg)
	case ws.CTypeAuth:
		if h.frameRate.Allow("f:" + c.UserID) {
			c.SendError("already_authed", "connection already authenticated")
		}
	default:
		if h.frameRate.Allow("f:" + c.UserID) {
			c.SendError("unknown_type", "unknown message type")
		}
	}
}

func (h *WSHandler) handleMsg(c *ws.Client, msg ws.ClientMsg) {
	if !h.msgRate.Allow("u:" + c.UserID) {
		c.SendError("rate_limited", "too many messages")
		return
	}
	if !h.checkRoomAccess(c, msg.Room) {
		return
	}
	in := model.MessageInput{
		ClientMsgID: msg.ClientMsgID,
		Room:        msg.Room,
		Content:     msg.Content,
		Type:        msg.MsgType,
		FileURL:     msg.FileURL,
		PreviewURL:  msg.PreviewURL,
		FileSize:    msg.FileSize,
		ReplyTo:     msg.ReplyTo,
		Mentions:    msg.Mentions,
	}
	saved, dup, err := h.svc.Chat.HandleMessage(msgCtx(c), c, in)
	if err != nil {
		c.SendError("rejected", err.Error())
		return
	}
	if dup {
		// dublikat — idempotent javob (client uchun xavfsiz).
		c.Send(&ws.ServerMsg{Type: ws.STypeOK, Code: "duplicate", Room: msg.Room})
		return
	}
	_ = saved
}

func (h *WSHandler) handleTyping(c *ws.Client, msg ws.ClientMsg) {
	if !h.checkRoomAccess(c, msg.Room) {
		return
	}
	if !h.typing.Allow("t:" + c.UserID) {
		return
	}
	on := msg.On != nil && *msg.On
	// F3: typing ham xabar kabi blok filtridan o'tadi — aks holda bloklagan
	// odam bloklangan odamning yozayotganini ko'rib turardi.
	h.hub.BroadcastToRoomFiltered(msg.Room, &ws.ServerMsg{Type: ws.STypeTyping, Room: msg.Room, UserID: c.UserID, TypingOn: on}, h.svc.Chat.SkipBlockedBy(c.UserID))
}

// handleDelete — xabarni o'chirish. Faqat o'z xabaringizni o'chira olasiz
// (server tomondan tekshiriladi — client'ga ishonilmaydi).
func (h *WSHandler) handleDelete(c *ws.Client, msg ws.ClientMsg) {
	if !h.checkRoomAccess(c, msg.Room) {
		return
	}
	if !h.delRate.Allow("d:" + c.UserID) {
		c.SendError("rate_limited", "too many delete requests")
		return
	}
	if err := h.svc.Chat.DeleteMessage(msgCtx(c), c, msg.Room, msg.MessageID, msg.ClientMsgID); err != nil {
		c.SendError("rejected", err.Error())
		return
	}
}

func (h *WSHandler) handleBG(c *ws.Client, msg ws.ClientMsg) {
	if !h.bgRate.Allow("b:" + c.UserID) {
		c.SendError("rate_limited", "too many background changes")
		return
	}
	if !security.ValidateRoom(msg.Room) {
		c.SendError("rejected", security.ErrRoomInvalid.Error())
		return
	}
	if msg.Background == "none" || msg.Background == "" {
		// Fonsiz — tanlovni o'chiramiz.
		if err := h.svc.Background.SetBackground(msgCtx(c), c.UserID, "", msg.Room); err != nil {
			c.SendError("rejected", err.Error())
			return
		}
		c.Send(&ws.ServerMsg{Type: ws.STypeBGSet, Room: msg.Room, Background: ""})
		return
	}
	// bgID service'da mavjudligi tekshiriladi (DB lookup).
	if err := h.svc.Background.SetBackground(msgCtx(c), c.UserID, msg.Background, msg.Room); err != nil {
		c.SendError("rejected", err.Error())
		return
	}
	c.Send(&ws.ServerMsg{Type: ws.STypeBGSet, Room: msg.Room, Background: msg.Background})
}

func (h *WSHandler) handleHistory(c *ws.Client, msg ws.ClientMsg) {
	if !h.histRate.Allow("h:" + c.UserID) {
		c.SendError("rate_limited", "too many history requests")
		return
	}
	if !h.checkRoomAccess(c, msg.Room) {
		return
	}
	// Xonaga qo'shilamiz — shu tariqa DM xonasidagi jonli xabarlar
	// ham keladi (global xonaga ulanish allaqachon mavjud).
	h.hub.Join(c, msg.Room)
	limit := 50
	msgs, err := h.svc.Chat.History(msgCtx(c), msg.Room, msg.Cursor, limit)
	if err != nil {
		c.SendError("rejected", err.Error())
		return
	}
	// F4: realtime broadcast filtrlangani bilan tarixdan bloklangan
	// odamning xabarlari qaytmasligi kerak (bir xil kutilma).
	visible := make([]*model.Message, 0, len(msgs))
	for _, m := range msgs {
		if h.svc.Chat.BlockedByViewer(c.UserID, m.UserID) {
			continue
		}
		visible = append(visible, m)
	}
	// F8: reply originallar bitta batch so'rovda (N+1 yo'q).
	payload := h.svc.Chat.ToPayloads(msgCtx(c), visible)
	c.Send(&ws.ServerMsg{Type: ws.STypeHistory, Room: msg.Room, Messages: payload})
}

// checkRoomAccess — xonaga kirish huquqini tekshiradi.
// Oddiy xonalar uchun format validatsiyasi, DM xonalar uchun esa
// faqat ishtirokchilarga ruxsat beriladi.
func (h *WSHandler) checkRoomAccess(c *ws.Client, room string) bool {
	if security.ValidateDMRoom(room) {
		if security.IsDMRoomMember(room, c.UserID) {
			return true
		}
		c.SendError("forbidden", "room access denied")
		return false
	}
	if security.ValidateRoom(room) {
		return true
	}
	c.SendError("rejected", security.ErrRoomInvalid.Error())
	return false
}

func (h *WSHandler) sendReady(c *ws.Client) {
	online := h.svc.Presence.OnlineUsersFor(c.UserID)
	msg := &ws.ServerMsg{
		Type:  ws.STypeReady,
		Room:  model.RoomGlobal,
		Users: online,
	}
	// Foydalanuvchining saqlangan foni (qayta yuklashda tiklanadi).
	if bgID, err := h.svc.Background.UserBackground(context.Background(), c.UserID, model.RoomGlobal); err == nil && bgID != "" {
		msg.Background = bgID
	}
	// Fon katalogi — client BgList to'ldirishi uchun. Aks holda qayta
	// yuklashda BgList bo'sh bo'lib, tanlangan fon URL'si topilmay qolardi.
	msg.Backgrounds = h.svc.Background.ListPayloads(context.Background())
	c.Send(msg)
}

func (h *WSHandler) cleanup(c *ws.Client) {
	// AVVAL Disconnect — MarkOffline'da UserDeviceCount eski (o'zi ham qo'shilgan)
	// hisoblanmasligi uchun. Aks holda oxirgi qurilma uzilganda ham count>=1 bo'lib,
	// foydalanuvchi abadiy online bo'lib qolardi.
	h.hub.Disconnect(c)
	h.svc.Presence.MarkOffline(c)
}

// msgCtx — WS dispatch uchun timeout'li kontekst (sekin DB bitta client'ning
// keyingi freymlarini cheksiz bloklamasligi uchun — head-of-line blocking oldi).
func msgCtx(_ *ws.Client) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	time.AfterFunc(5*time.Second, cancel)
	return ctx
}
