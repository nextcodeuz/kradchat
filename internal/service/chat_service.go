package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/model"
	"kradchat/internal/repository"
	"kradchat/internal/security"
	"kradchat/internal/ws"
)

var (
	ErrBlocked         = errors.New("you are blocked")
	ErrMessageNotFound = errors.New("message not found")
	ErrNotOwnMessage   = errors.New("you can only delete your own messages")
	ErrNotOwnFile      = errors.New("file belongs to another user")
)

type ChatService struct {
	cfg      *config.Config
	repo     *repository.Repositories
	hub      *ws.Hub
	presence *PresenceService
	blocks   *BlockService

	dedup *security.DedupStore

	// async batch insert (faqat favqulodda fallback — asosiy yo'l sync persist).
	mu        sync.Mutex
	batch     []*model.Message
	flushCh   chan struct{}
	stopCh    chan struct{}
	stopOnce  sync.Once
	flushTick time.Duration
}

func NewChatService(cfg *config.Config, repo *repository.Repositories, hub *ws.Hub, presence *PresenceService, blocks *BlockService) *ChatService {
	c := &ChatService{
		cfg:       cfg,
		repo:      repo,
		hub:       hub,
		presence:  presence,
		blocks:    blocks,
		dedup:     security.NewDedupStore(10*time.Minute, 200000),
		flushCh:   make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		flushTick: 300 * time.Millisecond,
	}
	go c.flusher()
	return c
}

func (c *ChatService) Stop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
	c.flush()
}

// HandleMessage — WS orqali kelgan xabarni qayta ishlaydi.
// Qaytaradi: message (success), duplicate (bool), err (nil bo'lsa broadcast bo'ldi).
func (c *ChatService) HandleMessage(ctx context.Context, from *ws.Client, in model.MessageInput) (*model.Message, bool, error) {
	if !security.ValidateRoom(in.Room) && !security.ValidateDMRoom(in.Room) {
		return nil, false, security.ErrRoomInvalid
	}
	// Bloklash: DM xonasida ikki tomonning birortasi ikkinchisini bloklagan
	// bo'lsa xabar yuborilmaydi (bloklash ikki tomonlama to'xtatadi).
	if security.ValidateDMRoom(in.Room) {
		if peer := security.DMRoomPeer(in.Room, from.UserID); peer != "" && c.blocks.BlocksAny(peer, from.UserID) {
			return nil, false, ErrBlocked
		}
	}
	if !security.ValidateClientMsgID(in.ClientMsgID) {
		return nil, false, security.ErrClientMsgID
	}
	if in.Type == "" {
		in.Type = model.MsgTypeText
	}
	if !security.ValidateMessageType(in.Type) {
		return nil, false, security.ErrMessageType
	}
	// Dedup: in-memory + DB UNIQUE.
	if c.dedup.Seen(from.UserID + ":" + in.ClientMsgID) {
		return nil, true, nil
	}

	content := in.Content
	switch in.Type {
	case model.MsgTypeText, model.MsgTypeEmoji:
		if err := security.ValidateContent(content); err != nil {
			return nil, false, err
		}
		content = security.Clean(content)
	case model.MsgTypeImage, model.MsgTypeVideo, model.MsgTypeFile:
		// Fayl xabarlarida Content — asl fayl nomi (ko'rsatish uchun).
		// Server UUID'li nomni saqlamaydi; original nom client'dan keladi.
		content = security.CleanFileName(in.Content)
		if !security.ValidateFileURL(in.FileURL) {
			return nil, false, security.ErrFileURL
		}
		// PreviewURL ham faqat mahalliy /static/uploads yo'li bo'lishi kerak
		// (tashqi URL xabar meta-ma'lumotlariga tushmasligi uchun).
		if !security.ValidateFileURL(in.PreviewURL) {
			return nil, false, security.ErrFileURL
		}
		// Egalik: begona user fayliga havola qo'yib bo'lmaydi (IDOR).
		// Registry'da topilmasa — yuboruvchi egasi sifatida yoziladi.
		if err := c.claimFile(ctx, from.UserID, in.FileURL, in.PreviewURL, content, in.FileSize); err != nil {
			return nil, false, err
		}
	default:
		return nil, false, security.ErrMessageType
	}

	if len(in.Mentions) > 10 {
		in.Mentions = in.Mentions[:10]
	}
	// Mention gigiyenasi: UUID format + o'zini mention qilish filtri.
	// (Mavjudlik tekshiruvi har xabarda N so'rov bo'lgani uchun ataylab
	// qilinmaydi — Conversations/Search yo'li keshlaydi; bu yerda faqat
	// format tozalanadi, shunda soxta ID'larga notify urinishi kamayadi.)
	in.Mentions = filterValidMentions(in.Mentions, from.UserID)

	// Fayl hajmi client'dan keladi — chegaralaymiz (spoofing oldi).
	if in.FileSize < 0 || in.FileSize > c.cfg.Upload.MaxFileSize {
		return nil, false, security.ErrFileTooBig
	}

	// Reply validatsiyasi: original xabar mavjud va o'sha xonada bo'lishi kerak.
	if in.ReplyTo > 0 {
		orig, err := c.repo.Messages.ByID(ctx, in.ReplyTo)
		if err != nil || orig == nil || orig.Room != in.Room {
			return nil, false, ErrMessageNotFound
		}
	}

	msg := &model.Message{
		UserID:      from.UserID,
		Room:        in.Room,
		Content:     content,
		Type:        in.Type,
		FileURL:     in.FileURL,
		PreviewURL:  in.PreviewURL,
		FileSize:    in.FileSize,
		ClientMsgID: in.ClientMsgID,
		ReplyTo:     in.ReplyTo,
		Mentions:    in.Mentions,
		CreatedAt:   time.Now(),
	}

	// Persist-then-broadcast: avval DB'ga yozamiz (3s timeout), keyin real ID
	// bilan broadcast qilamiz. Crash'da "ko'ringan lekin saqlanmagan" phantom
	// xabar bo'lmaydi; history race va ID=0 oynasi yopiladi. DB kechikishi
	// odatda 5-20ms — <100ms latency byudjeti ichida.
	persistCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	insertErr := c.repo.Messages.Insert(persistCtx, msg)
	cancel()
	if insertErr != nil {
		if errors.Is(insertErr, repository.ErrConflict) {
			return nil, true, nil
		}
		// DB vaqtincha o'lik bo'lsa — async batch'ga topshirib best-effort
		// broadcast qilamiz (availability > consistency, lekin bu yo'l
		// faqat xatolikda ishlaydi, normal yo'l sync).
		slog.Warn("sync insert failed, fallback to batch", "err", insertErr)
		c.broadcastMessage(msg)
		c.enqueue(msg)
		return msg, false, nil
	}

	c.broadcastMessage(msg)

	return msg, false, nil
}

// History xona tarixini qaytaradi.
func (c *ChatService) History(ctx context.Context, room string, before int64, limit int) ([]*model.Message, error) {
	return c.repo.Messages.History(ctx, room, before, limit)
}

// DedupCheck — reconnect'dan keyin client_msg_id takrorlanishini tekshiradi.
func (c *ChatService) DedupCheck(ctx context.Context, clientMsgID string) (*model.Message, error) {
	return c.repo.Messages.ByClientID(ctx, clientMsgID)
}

// SkipBlockedBy — broadcast filtri: sender'ni bloklagan oluvchilar
// o'tkazib yuboriladi (xabar/typing/o'chirish e'lonlari uchun bir xil).
// blocks nil bo'lsa hech kim chetlatilmaydi.
func (c *ChatService) SkipBlockedBy(senderID string) func(string) bool {
	if c.blocks == nil {
		return func(string) bool { return false }
	}
	return c.blocks.skipFilter(senderID)
}

// BlockedByViewer — viewer muallifni bloklaganmi (tarix filtri uchun).
func (c *ChatService) BlockedByViewer(viewerID, authorID string) bool {
	if c.blocks == nil || viewerID == "" || viewerID == authorID {
		return false
	}
	return c.blocks.Blocks(viewerID, authorID)
}

func (c *ChatService) broadcastMessage(m *model.Message) {
	payload := c.toMsgPayload(context.Background(), m)
	// Bloklash: m.UserID (sender) ni bloklagan oluvchilarga xabar yuborilmaydi.
	c.hub.BroadcastToRoomFiltered(m.Room, &ws.ServerMsg{Type: ws.STypeMsg, Message: payload, Room: m.Room}, c.blocks.skipFilter(m.UserID))
	c.notifyMentions(m)
	// DM xonasida suhbatdoshga realtime bildirishnoma (DM fix).
	if security.ValidateDMRoom(m.Room) {
		if peer := security.DMRoomPeer(m.Room, m.UserID); peer != "" {
			c.notifyDM(peer, m)
		}
	}
	// Reply qilingan xabar muallifiga bildirishnoma.
	if m.ReplyTo > 0 {
		go c.notifyReply(m)
	}
}

// notifyDM — DM xonasidagi suhbatdoshga bildirishnoma yuboradi. Client xonani
// ochiq bo'lsa o'tkazib yuboradi (notify.Room == CurRoom tekshiruvi bilan).
func (c *ChatService) notifyDM(peerID string, m *model.Message) {
	u := c.presence.UserSnapshot(m.UserID)
	title := u.DisplayName
	if title == "" {
		title = "@" + u.Username
	}
	body := m.Content
	if m.Type != model.MsgTypeText {
		body = "Yubordi: " + m.Type
	}
	if len(body) > 160 {
		body = body[:160]
	}
	c.hub.NotifyUser(peerID, &ws.ServerMsg{
		Type: ws.STypeNotif,
		Notify: &ws.NotifPayload{
			Type:   "dm",
			Title:  title,
			Body:   body,
			Room:   m.Room,
			UserID: m.UserID,
		},
	})
}

// notifyReply — reply qilingan xabarning muallifiga bildirishnoma.
// Kechikishsiz (goroutine ichida DB lookup, broadcast bloklanmaydi).
func (c *ChatService) notifyReply(m *model.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	orig, err := c.repo.Messages.ByID(ctx, m.ReplyTo)
	if err != nil || orig == nil || orig.UserID == m.UserID {
		return
	}
	if c.blocks.BlocksAny(orig.UserID, m.UserID) {
		return
	}
	u := c.presence.UserSnapshot(m.UserID)
	title := u.DisplayName
	if title == "" {
		title = "@" + u.Username
	}
	body := "Javob: " + orig.Content
	if len(body) > 160 {
		body = body[:160]
	}
	c.hub.NotifyUser(orig.UserID, &ws.ServerMsg{
		Type: ws.STypeNotif,
		Notify: &ws.NotifPayload{
			Type:   "reply",
			Title:  title,
			Body:   body,
			Room:   m.Room,
			UserID: m.UserID,
		},
	})
}

// DeleteMessage — xabarni o'chirish. Faqat o'z xabaringizni o'chira olasiz.
// Realtime: xona bo'ylab "deleted" xabari yuboriladi, clientlar o'chiradi.
// ID ma'lum bo'lmasa (optimistik node) client_msg_id orqali ham bo'ladi.
func (c *ChatService) DeleteMessage(ctx context.Context, from *ws.Client, room string, msgID int64, clientMsgID string) error {
	if room == "" || (!security.ValidateRoom(room) && !security.ValidateDMRoom(room)) {
		return security.ErrRoomInvalid
	}
	var (
		m   *model.Message
		err error
	)
	switch {
	case msgID > 0:
		m, err = c.repo.Messages.ByID(ctx, msgID)
	case clientMsgID != "":
		m, err = c.repo.Messages.ByClientID(ctx, clientMsgID)
	default:
		return security.ErrBadID
	}
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrMessageNotFound
		}
		return err
	}
	if m == nil {
		return ErrMessageNotFound
	}
	if m.Room != room {
		return ErrMessageNotFound
	}
	if m.UserID != from.UserID {
		return ErrNotOwnMessage
	}
	if err := c.repo.Messages.MarkDeleted(ctx, m.ID); err != nil {
		return err
	}
	c.hub.BroadcastToRoomFiltered(m.Room, &ws.ServerMsg{Type: ws.STypeDeleted, Room: m.Room, MessageID: m.ID}, c.SkipBlockedBy(m.UserID))
	// Fayl xabari o'chsa — boshqa hech kim ishlatmasa diskdan ham o'chiramiz
	// (best-effort: xato xabar o'chishiga ta'sir qilmaydi).
	go c.cleanupMessageFiles(m)
	return nil
}

// claimFile — fayl URL egaligini tekshiradi: registry'da boshqa userga
// tegishli bo'lsa ErrNotOwnFile; yozuv bo'lmasa yuboruvchi nomiga yoziladi.
func (c *ChatService) claimFile(ctx context.Context, userID, fileURL, previewURL, filename string, size int64) error {
	if c.repo == nil || c.repo.Files == nil {
		return nil
	}
	for _, url := range []string{fileURL, previewURL} {
		if url == "" {
			continue
		}
		rec, err := c.repo.Files.FileByURL(ctx, url)
		if err != nil {
			continue // yozuv yo'q — pastda yaratiladi
		}
		if rec != nil && rec.OwnerID != "" && rec.OwnerID != userID {
			return ErrNotOwnFile
		}
	}
	rec, err := c.repo.Files.FileByURL(ctx, fileURL)
	if err != nil || rec == nil {
		// Diskda mavjud bo'lmagan URL ga soxta registry yozuvi yaratmaymiz
		// (M6: taxminiy URL bilan DB shishi). Fayl diskda bo'lsa — yozuv
		// yaratiladi (registry yozish best-effort o'tkazib yuborilgan holat).
		if c.cfg == nil || c.cfg.Upload.Dir == "" {
			return nil
		}
		if p := diskPathForURL(c.cfg.Upload.Dir, fileURL); p == "" {
			return nil
		} else if fi, serr := os.Stat(p); serr != nil || fi.IsDir() {
			return nil
		}
		_ = c.repo.Files.CreateFile(ctx, &model.FileRecord{
			OwnerID:    userID,
			Filename:   filename,
			Size:       size,
			URL:        fileURL,
			PreviewURL: previewURL,
			Kind:       "chat",
		})
	}
	return nil
}

// cleanupMessageFiles — o'chirilgan xabarning fayllarini tozalaydi.
// Faqat boshqa hech kim ishlatmasa: registry yozuv + disk (fayl va preview).
func (c *ChatService) cleanupMessageFiles(m *model.Message) {
	if m == nil || (m.Type != model.MsgTypeImage && m.Type != model.MsgTypeVideo && m.Type != model.MsgTypeFile) {
		return
	}
	if m.FileURL == "" || !security.ValidateFileURL(m.FileURL) {
		return
	}
	if c.repo == nil || c.repo.Files == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if rec, err := c.repo.Files.FileByURL(ctx, m.FileURL); err == nil && rec != nil {
		if _, derr := c.repo.Files.DeleteFile(ctx, rec.ID, m.UserID); derr != nil {
			slog.Warn("delete file record", "url", m.FileURL, "err", derr)
			return
		}
	}
	if ref, err := c.repo.Files.IsReferenced(ctx, m.FileURL, m.ID); err != nil || ref {
		return // hali boshqa xabar/avatar ishlatadi — disk tegilmaydi
	}
	for _, url := range []string{m.FileURL, m.PreviewURL} {
		if p := diskPathForURL(c.cfg.Upload.Dir, url); p != "" {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				slog.Warn("remove deleted message file", "path", p, "err", err)
			}
		}
	}
}

func (c *ChatService) notifyMentions(m *model.Message) {
	if len(m.Mentions) == 0 {
		return
	}
	u := c.presence.UserSnapshot(m.UserID)
	title := "@" + u.Username
	if u.DisplayName != "" {
		title = u.DisplayName
	}
	body := m.Content
	if m.Type != model.MsgTypeText {
		body = "Sent a " + m.Type
	}
	if len(body) > 160 {
		body = body[:160]
	}
	for _, uid := range m.Mentions {
		if uid == m.UserID {
			continue
		}
		// Bloklash: bir tomon bloklagan bo'lsa bildirishnoma yuborilmaydi
		// (bloklangan user global xona orqali @mention bilan aylanib o'tolmasin).
		if c.blocks.BlocksAny(uid, m.UserID) {
			continue
		}
		c.hub.NotifyUser(uid, &ws.ServerMsg{
			Type:   ws.STypeNotif,
			Notify: &ws.NotifPayload{Type: "mention", Title: title, Body: body},
		})
	}
}

func (c *ChatService) enqueue(m *model.Message) {
	c.mu.Lock()
	c.batch = append(c.batch, m)
	needSignal := len(c.batch) == 1
	c.mu.Unlock()
	if needSignal {
		select {
		case c.flushCh <- struct{}{}:
		default:
		}
	}
}

func (c *ChatService) flush() {
	c.mu.Lock()
	batch := c.batch
	c.batch = nil
	c.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.repo.Messages.BulkInsert(ctx, batch); err != nil {
		slog.Error("bulk insert messages", "n", len(batch), "err", err)
		// retry bitta-bitta, har biriga yangi timeout — bitta eskirgan ctx
		// hammasini soxta fail qilmasligi uchun.
		for _, m := range batch {
			if m.ID > 0 {
				continue // BulkInsert ichida saqlangan
			}
			rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
			rerr := c.repo.Messages.Insert(rctx, m)
			rcancel()
			if rerr != nil && !errors.Is(rerr, repository.ErrConflict) {
				slog.Warn("insert message failed", "client_msg_id", m.ClientMsgID, "err", rerr)
			}
		}
	}
	// Haqiqiy xabar ID'si saqlangach yuboruvchiga "ack" yuboriladi — client
	// optimistik node'iga id o'rnatadi (o'chirish/reply uchun kerak). Broadcast
	// xabar insert'dan OLDIN ketgani uchun ID'si 0 bo'lardi.
	for _, m := range batch {
		if m.ID > 0 && m.ClientMsgID != "" {
			c.hub.NotifyUser(m.UserID, &ws.ServerMsg{
				Type:        ws.STypeAck,
				Room:        m.Room,
				MessageID:   m.ID,
				ClientMsgID: m.ClientMsgID,
			})
		}
	}
}

func (c *ChatService) flusher() {
	tick := time.NewTicker(c.flushTick)
	defer tick.Stop()
	for {
		select {
		case <-c.flushCh:
			c.flush()
		case <-tick.C:
			c.flush()
		case <-c.stopCh:
			return
		}
	}
}

// ToPayload — model.Message ni WS payload'ga o'giradi (handler uchun export).
func (c *ChatService) ToPayload(m *model.Message) *ws.MsgPayload {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.toMsgPayload(ctx, m)
}

// ToPayloads — tarix uchun batch: reply originallar BITTA so'rovda olinadi
// (F8: 50 reply'li tarix endi 50+ DB roundtrip emas, 1).
func (c *ChatService) ToPayloads(ctx context.Context, msgs []*model.Message) []*ws.MsgPayload {
	var ids []int64
	seen := make(map[int64]bool)
	for _, m := range msgs {
		if m != nil && m.ReplyTo > 0 && !seen[m.ReplyTo] {
			seen[m.ReplyTo] = true
			ids = append(ids, m.ReplyTo)
		}
	}
	origs := make(map[int64]*model.Message)
	if len(ids) > 0 && c.repo != nil && c.repo.Messages != nil {
		if got, err := c.repo.Messages.ByIDs(ctx, ids); err == nil {
			origs = got
		} else {
			slog.Warn("batch reply fetch", "err", err)
		}
	}
	out := make([]*ws.MsgPayload, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, c.toMsgPayloadBatch(ctx, m, origs))
	}
	return out
}

// filterValidMentions — UUID format + self-mention filtri, duplikatsiz.
func filterValidMentions(ids []string, selfID string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || id == selfID {
			continue
		}
		if !security.ValidateUserID(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (c *ChatService) toMsgPayload(ctx context.Context, m *model.Message) *ws.MsgPayload {
	return c.toMsgPayloadBatch(ctx, m, nil)
}

// toMsgPayloadBatch — origs berilsa reply shu xaritadan olinadi (batch),
// berilmasa yakka ByID (realtime yagona xabar yo'li).
func (c *ChatService) toMsgPayloadBatch(ctx context.Context, m *model.Message, origs map[int64]*model.Message) *ws.MsgPayload {
	u := c.presence.UserSnapshot(m.UserID)
	p := &ws.MsgPayload{
		ID:          m.ID,
		UserID:      m.UserID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		AvatarURL:   u.AvatarURL,
		Room:        m.Room,
		Content:     m.Content,
		Type:        m.Type,
		FileURL:     m.FileURL,
		PreviewURL:  m.PreviewURL,
		FileSize:    m.FileSize,
		ClientMsgID: m.ClientMsgID,
		ReplyTo:     m.ReplyTo,
		Mentions:    m.Mentions,
		Verified:    u.Verified,
		CreatedAt:   m.CreatedAt.UnixMilli(),
	}
	// Reply quote: original xabar o'chirilgan bo'lsa "Xabar o'chirilgan".
	if m.ReplyTo > 0 {
		var orig *model.Message
		if origs != nil {
			orig = origs[m.ReplyTo] // batch: topilmagan = o'chirilgan
		} else if c.repo != nil && c.repo.Messages != nil {
			orig, _ = c.repo.Messages.ByID(ctx, m.ReplyTo)
		}
		if orig == nil {
			p.ReplyDeleted = true
			return p
		}
		origU := c.presence.UserSnapshot(orig.UserID)
		p.ReplyUserID = orig.UserID
		p.ReplyUsername = origU.Username
		p.ReplyDispName = origU.DisplayName
		p.ReplyContent = orig.Content
		p.ReplyType = orig.Type
	}
	return p
}
