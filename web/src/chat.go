//go:build js && wasm

package main

import (
	"html"
	"sort"
	"strconv"
	"strings"
	"syscall/js"
	"time"
)

// Chat ekrani — xabar yozish, emoji, upload, onlayn ro'yxat.

// renderChat — umumiy (global) chat ekranini ochadi.
func renderChat(app js.Value) {
	renderChatRoom(app, "global", "# global", "Hamma uchun ochiq xona")
}

// syncHash — joriy xonani URL hash'ga yozadi (reload'da tiklanadi, share bo'ladi).
// DM id'lari hash'da ko'rinadi — bu dizayn tanlovi (share uchun kerak).
func syncHash(room string) {
	defer func() { _ = recover() }()
	h := js.Global().Get("location").Get("hash").String()
	want := "#/chat/" + room
	if h != want {
		js.Global().Get("location").Set("hash", want)
	}
}

// roomFromHash — reload'da hash'dan xonani tiklaydi (#/chat/<room>).
func roomFromHash() string {
	defer func() { _ = recover() }()
	h := js.Global().Get("location").Get("hash").String()
	const p = "#/chat/"
	if strings.HasPrefix(h, p) {
		room := strings.TrimPrefix(h, p)
		if room == "global" || isDMRoom(room) {
			return room
		}
	}
	return "global"
}

// safeAvatarURL — avatarni CSS url('...') ga qo'yishdan oldin validatsiya.
// Faqat mahalliy /static/uploads yoki data: ruxsat — quote-break/deface oldi.
func safeAvatarURL(u string) string {
	return safeStaticURL(u, true)
}

// safeFileURL — xabar fayllarini img/video/havolaga qo'yishdan oldin validatsiya.
// Faqat mahalliy /static/uploads ruxsat (tashqi URL XSS/phishing manbai).
func safeFileURL(u string) string {
	return safeStaticURL(u, false)
}

func safeStaticURL(u string, allowData bool) string {
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "/static/uploads/") && !strings.Contains(u, "'") &&
		!strings.Contains(u, `"`) && !strings.Contains(u, "\\") && !strings.Contains(u, "..") {
		return u
	}
	if allowData && strings.HasPrefix(u, "data:image/") {
		return u
	}
	return ""
}

// renderChatRoom — berilgan xona uchun chat ekranini ochadi.
// room="global" — umumiy chat; "dm:<id>:<id>" — shaxsiy xona.
func renderChatRoom(app js.Value, room, title, sub string) {
	store.CurRoom = room
	store.Messages = nil
	syncHash(room)
	HTML(app, chatLayout(room, title, sub))
	applyChatBackground()
	wireChat(app)
	wireMobileBar(app)
	renderOnline()
	renderMessages()
	refreshMe()
	// Xona tarixini so'raymiz (server bu xonaga qo'shib, jonli xabarlarni
	// ham yuboradi). Ulanish ochilmasa sendWS hech narsa qilmaydi — WS
	// onopen'dagi history so'rovi buni hal qiladi.
	sendWS(map[string]any{"type": "history", "room": room, "cursor": 0})
	// DM xonasida suhbatdosh bizni bloklaganmi — yozish maydoni o'chiriladi.
	if pid := peerIDForDM(room); pid != "" {
		go checkPeerBlocked(pid)
	}
}

// checkPeerBlocked — DM suhbatdoshi bizni bloklagan bo'lsa, yozishni o'chiradi
// va banner ko'rsatadi. Xona orada o'zgarib ketgan bo'lsa hech narsa qilmaydi.
func checkPeerBlocked(peerID string) {
	var u searchUser
	q := js.Global().Get("encodeURIComponent").Invoke(peerID).String()
	if err := fetchJSONInto("GET", "/api/users/search?q="+q, nil, &u); err != nil || u.ID == "" {
		return
	}
	store.ByUser[u.Username] = u.ID
	if !u.BlockedMe || peerIDForDM(store.CurRoom) != peerID {
		return
	}
	lock := ByID("composer-lock")
	if lock.IsNull() || lock.IsUndefined() {
		return
	}
	SetText(lock, "🔒 Sizni bloklagan — bu xonaga xabar yoza olmaysiz")
	RemoveClass(lock, "hidden")
	input := ByID("msg-input")
	if !input.IsNull() && !input.IsUndefined() {
		input.Call("setAttribute", "contenteditable", "false")
		AddClass(input, "locked")
	}
	btn := ByID("btn-send")
	if !btn.IsNull() && !btn.IsUndefined() {
		Attr(btn, "disabled", "true")
	}
	AddClass(ByID("btn-emoji"), "btn-disabled")
	AddClass(ByID("btn-attach"), "btn-disabled")
}

func chatLayout(room, title, sub string) string {
	me := store.User
	name := ""
	sub0 := ""
	if me != nil {
		name = me.DisplayName
		if name == "" {
			name = "@" + me.Username
		}
		sub0 = "id: " + me.ID
	}
	// XSS: name/title/sub begona display_name'dan kelishi mumkin (openDM) —
	// innerHTML interpolatsiyadan oldin escape qilinadi (screens.go dagi namuna).
	name = html.EscapeString(name)
	sub0 = html.EscapeString(sub0)
	title = html.EscapeString(title)
	sub = html.EscapeString(sub)
	// Shaxsiy xonada orqaga tugmasi ko'rsatiladi.
	backBtn := ""
	if room != "global" {
		backBtn = `<button class="icon-btn" id="btn-back-chat" title="Umumiy chatga qaytish">←</button>`
	}
	return `<div class="layout layout-chat">
  ` + mobileBar() + `
  <aside class="sidebar glass">
    <div class="brand">KRAD<span>CHAT</span></div>
    <div class="me-card">
      <div class="me-avatar" id="me-avatar"></div>
      <div class="me-meta">
        <div class="me-name" id="me-name">` + name + `</div>
        <div class="me-sub" id="me-sub">` + sub0 + `</div>
      </div>
      <button class="icon-btn" id="btn-logout" title="Chiqish" aria-label="Chiqish"><svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor" aria-hidden="true"><path d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z"/></svg></button>
    </div>
    <nav class="side-nav">
      <button class="nav-item active" id="nav-chat">💬 Umumiy chat</button>
      <button class="nav-item" id="nav-search">🔍 Foydalanuvchi qidirish</button>
      <button class="nav-item" id="nav-files">📁 Fayllarim</button>
      <button class="nav-item" id="nav-backgrounds">🖼 Orqa fonlar</button>
      <button class="nav-item" id="nav-devices">📱 Qurilmalar</button>
      <button class="nav-item" id="nav-settings">⚙️ Sozlamalar</button>
    </nav>
    <div class="online-panel">
      <div class="online-title" id="online-title">Onlayn — 0</div>
      <div class="online-list" id="online-list"></div>
    </div>
  </aside>
  <main class="chat-main">
    <header class="chat-header">
      <div class="chat-head-row">
        <button class="icon-btn head-menu" id="btn-menu-chat" title="Menyu" aria-label="Menyu">☰</button>
        ` + backBtn + `
        <div>
          <div class="room-title" id="room-title">` + title + `</div>
          <div class="room-sub" id="room-sub">` + sub + `</div>
        </div>
      </div>
    </header>
    <div class="messages" id="messages"></div>
    <div class="emoji-panel hidden" id="emoji-panel">
      <input id="emoji-search" type="text" placeholder="Emoji qidirish...">
      <div class="emoji-grid" id="emoji-grid"></div>
    </div>
    <div class="composer">
      <div class="composer-lock hidden" id="composer-lock"></div>
      <div class="typing-bar hidden" id="typing-bar"></div>
      <div class="reply-bar hidden" id="reply-bar"></div>
      <div class="upload-progress hidden" id="upload-progress"></div>
      <div class="composer-row">
        <button class="icon-btn" id="btn-emoji" title="Emoji">😊</button>
        <button class="icon-btn" id="btn-attach" title="Fayl yuborish">📎</button>
        <input id="file-input" type="file" hidden>
        <div class="msg-input" id="msg-input" contenteditable="true" role="textbox" aria-label="Xabar yozing" tabindex="0" data-placeholder="Xabar yozing..."></div>
        <button class="btn-send" id="btn-send" title="Yuborish">➤</button>
      </div>
    </div>
    ` + convFabHTML() + `
  </main>
</div>`
}

var lastTypingSent time.Time

func wireChat(app js.Value) {
	On(ByID("btn-send"), "click", func(_ js.Value, _ []js.Value) { sendComposer() })
	input := ByID("msg-input")
	On(input, "keydown", func(_ js.Value, args []js.Value) {
		if len(args) == 0 {
			return
		}
		ev := args[0]
		if ev.Get("key").String() == "Enter" && !ev.Get("shiftKey").Bool() {
			ev.Call("preventDefault")
			sendComposer()
		}
	})
	On(input, "input", func(_ js.Value, _ []js.Value) {
		text := input.Get("textContent").String()
		if text != "" {
			RemoveClass(input, "empty")
			throttledTyping(true)
		} else {
			AddClass(input, "empty")
			throttledTyping(false)
		}
	})
	On(ByID("btn-emoji"), "click", func(_ js.Value, _ []js.Value) {
		toggleEmoji()
	})
	On(ByID("btn-attach"), "click", func(_ js.Value, _ []js.Value) {
		ByID("file-input").Call("click")
	})
	On(ByID("file-input"), "change", func(_ js.Value, args []js.Value) {
		files := ByID("file-input").Get("files")
		uploadPickedFiles(files)
		// Input'ni reset — bir xil faylni qayta tanlashda change ishga tushadi.
		ByID("file-input").Set("value", "")
	})
	// Drag&drop + paste: fayl tashlansa/yopishtirilsa darhol upload.
	wireFileDrop(input)
	On(ByID("nav-chat"), "click", func(_ js.Value, _ []js.Value) { renderChat(app) })
	On(ByID("nav-search"), "click", func(_ js.Value, _ []js.Value) { renderSearch(app) })
	On(ByID("nav-files"), "click", func(_ js.Value, _ []js.Value) { renderFiles(app) })
	On(ByID("nav-backgrounds"), "click", func(_ js.Value, _ []js.Value) { renderBackgrounds(app) })
	On(ByID("nav-devices"), "click", func(_ js.Value, _ []js.Value) { renderDevices(app) })
	On(ByID("nav-settings"), "click", func(_ js.Value, _ []js.Value) { renderSettings(app) })
	On(ByID("btn-logout"), "click", func(_ js.Value, _ []js.Value) { doLogout() })
	back := ByID("btn-back-chat")
	if !back.IsNull() && !back.IsUndefined() {
		On(back, "click", func(_ js.Value, _ []js.Value) { renderChat(app) })
	}

	// Emoji search
	On(ByID("emoji-search"), "input", func(_ js.Value, _ []js.Value) {
		q := GetVal(ByID("emoji-search"))
		renderEmojiGrid(q)
	})

	// Reply bar (tanlangan xabarga javob) — har safar yangidan quramiz.
	renderReplyBar()

	// Shaxsiy suhbatlar "+" menyusi.
	wireConvFab(app)

	// Avto-scroll pastga
	scrollLock = true
	ScrollBottom(ByID("messages"))
}

// ===== Reply composer =====

// replyDraft — joriy javob tanlovi (reply bar'da ko'rsatiladi).
type replyDraft struct {
	msgID    int64
	userID   string
	username string
	name     string
	content  string
	typ      string
}

var replyDraftVal replyDraft

// setReplyDraft — xabarga javob tanlash.
func setReplyDraft(m *Msg) {
	replyDraftVal = replyDraft{
		msgID:    m.ID,
		userID:   m.UserID,
		username: m.Username,
		name:     displayOf(m),
		content:  m.Content,
		typ:      m.Type,
	}
	renderReplyBar()
	input := ByID("msg-input")
	if !input.IsNull() && !input.IsUndefined() {
		input.Call("focus")
	}
}

func cancelReply() {
	replyDraftVal = replyDraft{}
	renderReplyBar()
}

// renderReplyBar — reply bar'ni joriy tanlov bilan quradi (XSS xavfsiz: textContent).
func renderReplyBar() {
	bar := ByID("reply-bar")
	if bar.IsNull() || bar.IsUndefined() {
		return
	}
	HTML(bar, "")
	if replyDraftVal.msgID == 0 {
		AddClass(bar, "hidden")
		return
	}
	RemoveClass(bar, "hidden")
	icon := El("span", "reply-icon")
	SetText(icon, "↩")
	nameEl := El("span", "reply-name")
	SetText(nameEl, replyDraftVal.name)
	prevEl := El("span", "reply-preview")
	prev := replyDraftVal.content
	if prev == "" {
		prev = mediaLabelType(replyDraftVal.typ)
	}
	SetText(prevEl, html.UnescapeString(prev))
	closeBtn := El("button", "icon-btn reply-close")
	SetText(closeBtn, "✕")
	Attr(closeBtn, "title", "Javobni bekor qilish")
	On(closeBtn, "click", func(_ js.Value, _ []js.Value) { cancelReply() })
	Append(bar, icon)
	Append(bar, nameEl)
	Append(bar, prevEl)
	Append(bar, closeBtn)
}

// ===== Xabarlar =====

func renderMessages() {
	list := ByID("messages")
	if list.IsNull() || list.IsUndefined() {
		return
	}
	HTML(list, "")
	for _, m := range store.Messages {
		appendMsgNode(list, m, false)
	}
	// Scroll faqat bir marta, hamma node qurilgandan keyin.
	// Har appendMsgNode'da chaqirilsa 500+ marta layout thrash bo'lardi.
	ScrollBottom(list)
}

// optimisticNodes — yuborilgan (optimistik) xabarlar node'lari. Server echo'da
// hammasini qidirmaslik uchun (Q() har xabar uchun butun DOM'ni skanerlar edi —
// tez-tez keladigan chatda sekinlashuv manbai). Bu yerda bevosita map'dan.
var optimisticNodes = map[string]js.Value{}

// inbox — kiruvchi xabarlar navbati. DOM'ga bitta-bitta emas, batchelanib
// yoziladi (setTimeout ~80ms). Portlovchan trafikda (yuzlab xabar birdan) WASM
// single-thread muzlab qolmasligi uchun: hamma xabar bitta DOM batch'ida.
var inbox []*Msg
var inboxPending bool

func onMessage(m *Msg) {
	if m == nil {
		return
	}
	// Boshqa xonadan kelgan xabar — faqat user cache'ini yangilaymiz.
	if m.Room != store.CurRoom {
		if store.Users[m.UserID] == nil {
			store.Users[m.UserID] = &OnlineUser{ID: m.UserID, Username: m.Username, DisplayName: m.DisplayName, AvatarURL: m.AvatarURL, Online: true, Verified: m.Verified}
			store.ByUser[m.Username] = m.UserID
		}
		return
	}
	queueIncoming(m)
}

// queueIncoming — xabarni navbatga qo'shadi va batchni yuboradi.
func queueIncoming(m *Msg) {
	inbox = append(inbox, m)
	if inboxPending {
		return
	}
	inboxPending = true
	var fn js.Func
	fn = js.FuncOf(func(js.Value, []js.Value) any {
		flushInbox()
		fn.Release()
		return nil
	})
	js.Global().Call("setTimeout", fn, 80)
}

// flushInbox — navbatdagi barcha xabarlarni bitta DOM batch'ida render qiladi.
func flushInbox() {
	inboxPending = false
	if len(inbox) == 0 {
		return
	}
	msgs := inbox
	inbox = nil
	list := ByID("messages")
	if list.IsNull() || list.IsUndefined() {
		return
	}
	nearBottom := isNearBottom(list, 200)
	for _, m := range msgs {
		// Optimistik node bormi? — almashtiramiz.
		var existing js.Value
		if m.ClientMsgID != "" {
			if n, ok := optimisticNodes[m.ClientMsgID]; ok {
				existing = n
			}
		}
		if !existing.IsNull() && !existing.IsUndefined() {
			existing.Get("dataset").Set("mid", strconv.FormatInt(m.ID, 10))
			updateMsgNode(existing, m)
			continue
		}
		appendMsgNode(list, m, false)
		if node := list.Get("lastElementChild"); !node.IsNull() && !node.IsUndefined() {
			AddClass(node, "msg-fresh") // jonli kelgan xabar kirish animatsiyasi
		}
		if store.Users[m.UserID] == nil {
			store.Users[m.UserID] = &OnlineUser{ID: m.UserID, Username: m.Username, DisplayName: m.DisplayName, AvatarURL: m.AvatarURL, Online: true, Verified: m.Verified}
			store.ByUser[m.Username] = m.UserID
		}
		// Xotira chegarasi — cheksiz o'sish bloklanadi.
		store.Messages = append(store.Messages, m)
		if len(store.Messages) > 500 {
			store.Messages = store.Messages[len(store.Messages)-500:]
		}
	}
	if nearBottom {
		ScrollBottom(list)
	}
}

func onHistory(msgs []*Msg) {
	if msgs == nil || len(msgs) == 0 {
		return
	}
	// Faqat joriy xonaga tegishli xabarlarni olamiz.
	mine := msgs[:0]
	for _, m := range msgs {
		if m.Room == store.CurRoom {
			mine = append(mine, m)
		}
	}
	if len(mine) == 0 {
		return
	}
	msgs = mine
	// Eski (history) xabarlarni mavjud xabarlar bilan birlashtiramiz:
	// takrorlanmaslik uchun id va client_msg_id bo'yicha dedup.
	seen := map[int64]bool{}
	byCmid := map[string]bool{}
	for _, m := range store.Messages {
		if m.ID > 0 {
			seen[m.ID] = true
		}
		if m.ClientMsgID != "" {
			byCmid[m.ClientMsgID] = true
		}
	}
	merged := make([]*Msg, 0, len(msgs)+len(store.Messages))
	merged = append(merged, msgs...)
	for _, m := range store.Messages {
		if (m.ID > 0 && seen[m.ID]) || (m.ClientMsgID != "" && byCmid[m.ClientMsgID]) {
			continue
		}
		merged = append(merged, m)
	}
	// Vaqt bo'yicha tartiblash (eski -> yangi).
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].CreatedAt == merged[j].CreatedAt {
			return merged[i].ID < merged[j].ID
		}
		return merged[i].CreatedAt < merged[j].CreatedAt
	})
	if len(merged) > 500 {
		merged = merged[len(merged)-500:]
	}
	store.Messages = merged
	renderMessages()
}

func appendMsgNode(list js.Value, m *Msg, optimistic bool) js.Value {
	row := El("div", "msg")
	if optimistic {
		SetData(row, "cmid", m.ClientMsgID)
		SetData(row, "optimistic", "1")
	} else if m.ClientMsgID != "" {
		SetData(row, "cmid", m.ClientMsgID)
	}
	if m.ID > 0 {
		SetData(row, "mid", strconv.FormatInt(m.ID, 10))
	}
	if m.UserID == store.User.ID {
		AddClass(row, "mine")
	}

	avatar := El("div", "msg-avatar")
	if av := safeAvatarURL(m.AvatarURL); av != "" {
		avatar.Get("style").Set("backgroundImage", "url('"+av+"')")
	} else {
		SetText(avatar, avatarLetter(m.Username, m.DisplayName))
	}

	body := El("div", "msg-body")
	head := El("div", "msg-head")
	nameEl := El("span", "msg-name")
	SetText(nameEl, displayOf(m))
	if m.Verified {
		badge := El("span", "verified-badge")
		SetText(badge, "✓")
		Attr(badge, "title", "Tasdiqlangan foydalanuvchi")
		Append(nameEl, badge)
	}
	timeEl := El("span", "msg-time")
	SetText(timeEl, fmtTime(m.CreatedAt))

	// Harakat tugmalari: javob + (o'z xabari bo'lsa) o'chirish.
	actions := El("span", "msg-actions")
	if m.Type != "deleted" {
		replyBtn := El("button", "msg-act-btn")
		SetText(replyBtn, "↩")
		Attr(replyBtn, "title", "Javob berish")
		On(replyBtn, "click", func(_ js.Value, _ []js.Value) { setReplyDraft(m) })
		Append(actions, replyBtn)
		if m.UserID == store.User.ID {
			delBtn := El("button", "msg-act-btn danger")
			SetText(delBtn, "🗑")
			Attr(delBtn, "title", "O'chirish")
			On(delBtn, "click", func(_ js.Value, _ []js.Value) { confirmDeleteMessage(m) })
			Append(actions, delBtn)
		}
	}
	Append(head, nameEl)
	Append(head, timeEl)
	Append(head, actions)

	content := El("div", "msg-content")
	buildMsgContent(content, m)

	Append(body, head)
	if m.ReplyTo > 0 {
		quote := El("div", "msg-quote")
		buildReplyQuote(quote, m)
		Append(body, quote)
	}
	Append(body, content)

	uid := m.UserID
	uname := m.Username
	udisp := m.DisplayName
	uav := m.AvatarURL
	AddClass(avatar, "clickable")
	On(avatar, "click", func(_ js.Value, _ []js.Value) {
		openProfile(uid, uname, udisp, uav)
	})
	AddClass(nameEl, "clickable")
	On(nameEl, "click", func(_ js.Value, _ []js.Value) {
		openProfile(uid, uname, udisp, uav)
	})

	Append(row, avatar)
	Append(row, body)
	Append(list, row)

	return row
}

// buildReplyQuote — reply qilingan xabarning ko'chirmasini quradi.
// Bosilganda original xabarga scroll qiladi.
func buildReplyQuote(quote js.Value, m *Msg) {
	if m.ReplyDeleted {
		nm := El("span", "msg-quote-name")
		SetText(nm, "Xabar o'chirilgan")
		Append(quote, nm)
		return
	}
	nm := El("span", "msg-quote-name")
	nmText := m.ReplyDisplayName
	if nmText == "" {
		nmText = "@" + m.ReplyUsername
	}
	if nmText == "" {
		nmText = "Javob"
	}
	SetText(nm, "↩ "+nmText)
	txt := El("span", "msg-quote-text")
	qt := m.ReplyContent
	if qt == "" {
		qt = mediaLabelType(m.ReplyType)
	}
	SetText(txt, html.UnescapeString(qt))
	Append(quote, nm)
	Append(quote, txt)
	AddClass(quote, "clickable")
	On(quote, "click", func(_ js.Value, _ []js.Value) { scrollToMessage(m.ReplyTo) })
}

// mediaLabelType — xabar turiga qarab media nomini qaytaradi.
func mediaLabelType(t string) string {
	switch t {
	case "image":
		return "📷 Rasm"
	case "video":
		return "🎬 Video"
	case "file":
		return "📎 Fayl"
	default:
		return t
	}
}

// scrollToMessage — ID bo'yicha xabarga scroll qiladi (reply quote bosilganda).
func scrollToMessage(id int64) {
	if id <= 0 {
		return
	}
	row := Q(`.msg[data-mid="` + strconv.FormatInt(id, 10) + `"]`)
	if row.IsNull() || row.IsUndefined() {
		showToast(&Notif{Type: "info", Title: "Xabar topilmadi", Body: "Bu xabar tarixdan tashqarida"})
		return
	}
	row.Call("scrollIntoView", map[string]any{"behavior": "smooth", "block": "center"})
	AddClass(row, "msg-flash")
	var fn js.Func
	fn = js.FuncOf(func(js.Value, []js.Value) any {
		RemoveClass(row, "msg-flash")
		fn.Release()
		return nil
	})
	js.Global().Call("setTimeout", fn, 1500)
}

// confirmDeleteMessage — o'z xabaringizni o'chirish (server tasdiqlaydi).
func confirmDeleteMessage(m *Msg) {
	ok := js.Global().Get("window").Call("confirm", "Xabarni o'chirasizmi?").Bool()
	if !ok {
		return
	}
	payload := map[string]any{"type": "delete", "room": store.CurRoom}
	if m.ID > 0 {
		payload["message_id"] = m.ID
	} else {
		// Ack hali kelmagan (optimistik) — client_msg_id orqali.
		payload["client_msg_id"] = m.ClientMsgID
	}
	sendWS(payload)
}

// onMessageDeleted — server "deleted" xabari: xabarni o'chirilgan qilib ko'rsatadi.
func onMessageDeleted(m ServerMsg) {
	for _, mm := range store.Messages {
		if mm.ID == m.MessageID {
			mm.Type = "deleted"
			mm.Content = ""
			mm.FileURL = ""
			mm.PreviewURL = ""
		}
	}
	if m.Room != store.CurRoom {
		return
	}
	row := Q(`.msg[data-mid="` + strconv.FormatInt(m.MessageID, 10) + `"]`)
	if row.IsNull() || row.IsUndefined() {
		return
	}
	AddClass(row, "deleted")
	content := Q(`.msg[data-mid="` + strconv.FormatInt(m.MessageID, 10) + `"] .msg-content`)
	if !content.IsNull() && !content.IsUndefined() {
		HTML(content, "")
		buildMsgContent(content, &Msg{Type: "deleted"})
	}
	actions := Q(`.msg[data-mid="` + strconv.FormatInt(m.MessageID, 10) + `"] .msg-actions`)
	if !actions.IsNull() && !actions.IsUndefined() {
		HTML(actions, "")
	}
}

// onMessageAck — server async insert'dan so'ng haqiqiy xabar ID'sini yuboradi.
// Optimistik node'ga id o'rnatiladi (o'chirish uchun kerak).
func onMessageAck(m ServerMsg) {
	if m.MessageID <= 0 || m.ClientMsgID == "" {
		return
	}
	for _, mm := range store.Messages {
		if mm.ClientMsgID == m.ClientMsgID {
			mm.ID = m.MessageID
		}
	}
	if m.Room != store.CurRoom {
		return
	}
	row := Q(`.msg[data-cmid="` + cssEscape(m.ClientMsgID) + `"]`)
	if row.IsNull() || row.IsUndefined() {
		return
	}
	row.Get("dataset").Set("mid", strconv.FormatInt(m.MessageID, 10))
}

// buildMsgContent — xabar turiga qarab content qismini quradi.
func buildMsgContent(content js.Value, m *Msg) {
	switch m.Type {
	case "deleted":
		AddClass(content, "deleted-text")
		SetText(content, "Xabar o'chirilgan")
	case "text", "emoji":
		if m.Type == "emoji" {
			AddClass(content, "emoji-large")
		}
		appendRichText(content, m.Content)
	case "file", "image", "video":
		renderFileMsg(content, m)
	}
}

func updateMsgNode(node js.Value, m *Msg) {
	node.Get("dataset").Set("cmid", m.ClientMsgID)
	content := Q(`[data-cmid="` + cssEscape(m.ClientMsgID) + `"] .msg-content`)
	if !content.IsNull() && !content.IsUndefined() {
		HTML(content, "")
		buildMsgContent(content, m)
	}
	// Reply quote'ni server ko'chirmasi bilan yangilaymiz (optimistik draft
	// to'liq bo'lmasligi mumkin).
	if m.ReplyTo > 0 {
		quote := Q(`[data-cmid="` + cssEscape(m.ClientMsgID) + `"] .msg-quote`)
		if !quote.IsNull() && !quote.IsUndefined() {
			HTML(quote, "")
			buildReplyQuote(quote, m)
		}
	}
	t := Q(`[data-cmid="` + cssEscape(m.ClientMsgID) + `"] .msg-time`)
	if !t.IsNull() && !t.IsUndefined() {
		SetText(t, fmtTime(m.CreatedAt))
	}
	node.Get("dataset").Set("optimistic", "")
	delete(optimisticNodes, m.ClientMsgID)
}

func displayOf(m *Msg) string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	if m.Username != "" {
		return "@" + m.Username
	}
	return "???"
}

func avatarLetter(username, display string) string {
	s := username
	if display != "" {
		s = display
	}
	if s == "" {
		return "?"
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0]))
}

// knownCodeLangs — eski server bloklarida (data-lang'siz) birinchi qator til
// nomi ekanini aniqlash uchun.
var knownCodeLangs = map[string]bool{
	"go": true, "golang": true, "js": true, "jsx": true, "ts": true, "tsx": true,
	"javascript": true, "typescript": true, "python": true, "py": true,
	"java": true, "c": true, "cpp": true, "c++": true, "c#": true, "cs": true,
	"rust": true, "rs": true, "sh": true, "bash": true, "shell": true, "zsh": true,
	"php": true, "ruby": true, "rb": true, "swift": true, "kotlin": true, "kt": true,
	"dart": true, "lua": true, "html": true, "css": true, "json": true, "xml": true,
	"sql": true, "yaml": true, "yml": true, "toml": true, "ini": true, "cfg": true,
	"markdown": true, "md": true, "text": true, "txt": true, "plain": true, "plaintext": true,
	"dockerfile": true, "diff": true, "log": true, "perl": true, "r": true,
	"matlab": true, "scala": true, "groovy": true, "haskell": true, "hs": true,
	"elixir": true, "erlang": true, "clojure": true, "nim": true, "zig": true,
	"v": true, "powershell": true, "ps1": true, "bat": true, "cmd": true,
}

// parseCodeLangAttr — server <pre><code ...> ochilish tegidan data-lang ni oladi.
func parseCodeLangAttr(tag string) string {
	i := strings.Index(tag, "data-lang=")
	if i < 0 {
		return ""
	}
	rest := tag[i+len("data-lang="):]
	if len(rest) > 0 && rest[0] == '"' {
		if j := strings.IndexByte(rest[1:], '"'); j >= 0 {
			return html.UnescapeString(rest[1 : 1+j])
		}
	}
	return ""
}

// sniffCodeLang — eski saqlangan bloklarda (data-lang'siz) til nomi birinchi
// qatorga qo'shilib qolgan bo'lishi mumkin: tanilgan til nomi bo'lsa ajratadi.
func sniffCodeLang(text string) (string, string) {
	nl := strings.IndexByte(text, '\n')
	if nl < 0 {
		return "", text
	}
	first := strings.TrimSpace(text[:nl])
	rest := text[nl+1:]
	if knownCodeLangs[strings.ToLower(first)] && strings.TrimSpace(rest) != "" {
		return first, rest
	}
	return "", text
}

// copyCode — kodni clipboardga ko'chiradi: avval navigator.clipboard
// (zamonaviy), u rad etsa textarea+execCommand fallback. Natija tugmada
// ko'rsatiladi.
func copyCode(text string, btn js.Value) {
	cb := js.Global().Get("navigator").Get("clipboard")
	if cb.Truthy() && cb.Get("writeText").Truthy() {
		var done, fail js.Func
		done = js.FuncOf(func(_ js.Value, _ []js.Value) any {
			done.Release()
			fail.Release()
			copyFeedback(btn, "✓ Nusxalandi")
			return nil
		})
		fail = js.FuncOf(func(_ js.Value, _ []js.Value) any {
			done.Release()
			fail.Release()
			execCopy(text, btn)
			return nil
		})
		p := cb.Call("writeText", text)
		if p.Truthy() && p.Get("then").Truthy() {
			p.Call("then", done, fail)
			return
		}
		done.Release()
		fail.Release()
	}
	execCopy(text, btn)
}

// execCopy — textarea + execCommand orqali nusxalash (eski brauzerlar uchun).
func execCopy(text string, btn js.Value) {
	doc := js.Global().Get("document")
	ta := doc.Call("createElement", "textarea")
	ta.Set("value", text)
	ta.Set("readOnly", true)
	ta.Get("style").Set("position", "fixed")
	ta.Get("style").Set("top", "-9999px")
	ta.Get("style").Set("left", "-9999px")
	ta.Get("style").Set("opacity", "0")
	doc.Get("body").Call("appendChild", ta)
	ta.Call("focus")
	ta.Call("select")
	ok := doc.Call("execCommand", "copy").Bool()
	ta.Call("remove")
	if ok {
		copyFeedback(btn, "✓ Nusxalandi")
	} else {
		SetText(btn, "✗ Nusxalash imkonsiz")
	}
}

// copyFeedback — tugmada qisqa muddatli natija matni ko'rsatadi.
func copyFeedback(btn js.Value, txt string) {
	old := btn.Get("textContent").String()
	SetText(btn, txt)
	go func() {
		time.Sleep(1500 * time.Millisecond)
		SetText(btn, old)
	}()
}

// appendRichText — @username/$id mentionlar va URL'larni ajratib, xavfsiz
// render qiladi (DOM node'lar orqali — innerHTML ishlatilmaydi, XSS yo'q).
func appendRichText(contentEl js.Value, content string) {
	for _, p := range tokenizeContentFull(content) {
		switch p.kind {
		case "mention":
			span := El("span", "mention")
			SetText(span, p.text)
			tok := p.text
			On(span, "click", func(_ js.Value, _ []js.Value) { openMentionProfile(tok) })
			Append(contentEl, span)
			if p.href != "" {
				a := El("a", "msg-link")
				Attr(a, "href", p.href)
				Attr(a, "target", "_blank")
				Attr(a, "rel", "noopener noreferrer")
				SetText(a, html.UnescapeString(p.text))
				Append(contentEl, a)
			} else {
				Append(contentEl, document.Call("createTextNode", html.UnescapeString(p.text)))
			}
		case "code":
			pre := El("pre", "msg-code")
			head := El("div", "code-head")
			lab := El("span", "code-lang")
			label := p.lang
			if label == "" {
				label = "code"
			}
			SetText(lab, label)
			copyBtn := El("button", "code-copy")
			SetText(copyBtn, "Nusxalash")
			ct := p.text
			On(copyBtn, "click", func(_ js.Value, _ []js.Value) { copyCode(ct, copyBtn) })
			Append(head, lab)
			Append(head, copyBtn)
			cd := El("code", "")
			SetText(cd, p.text)
			Append(pre, head)
			Append(pre, cd)
			Append(contentEl, pre)
		default:
			Append(contentEl, document.Call("createTextNode", html.UnescapeString(p.text)))
		}
	}
}

// openMentionProfile — @username yoki $id mentioni bosilganda user profilini
// ochadi. Username store'da bo'lmasa serverdan qidirib topadi.
func openMentionProfile(tok string) {
	if strings.HasPrefix(tok, "$") {
		id := strings.TrimPrefix(tok, "$")
		if su, ok := store.Users[id]; ok {
			openProfile(id, su.Username, su.DisplayName, su.AvatarURL)
		} else {
			openProfile(id, "", "", "")
		}
		return
	}
	username := strings.TrimPrefix(tok, "@")
	if id, ok := store.ByUser[username]; ok {
		if su, ok := store.Users[id]; ok {
			openProfile(id, su.Username, su.DisplayName, su.AvatarURL)
		} else {
			openProfile(id, "", "", "")
		}
		return
	}
	uid := username
	go func() {
		var usr searchUser
		q := js.Global().Get("encodeURIComponent").Invoke(uid).String()
		if err := fetchJSONInto("GET", "/api/users/search?q="+q, nil, &usr); err != nil || usr.ID == "" {
			showToast(&Notif{Type: "error", Title: "Topilmadi", Body: "@" + uid + " foydalanuvchisi topilmadi"})
			return
		}
		store.ByUser[usr.Username] = usr.ID
		openProfile(usr.ID, usr.Username, usr.DisplayName, usr.AvatarURL)
	}()
}

type contentTok struct {
	kind string // "text" | "mention" | "link" | "code"
	text string
	href string
	lang string // code tokeni uchun til nomi (fence qatoridan yoki data-lang)
}

// codeFenceEnd — i: ochuvchi fence (``` yoki ”') nuqtasi. Yopuvchi fence
// qatorini topib, (keyingi boshlanish indeksi, til nomi, blok ichidagi matn)ni
// qaytaradi. Yopilishsiz blok (ochiq qolgan) — end=-1.
func codeFenceEnd(content string, i int) (int, string, string) {
	marker := content[i : i+3]
	nl := strings.IndexByte(content[i:], '\n')
	if nl < 0 {
		return -1, "", ""
	}
	lang := strings.TrimSpace(content[i+3 : i+nl])
	bodyStart := i + nl + 1
	j := bodyStart
	for {
		if j >= len(content) {
			return -1, "", ""
		}
		lineEnd := strings.IndexByte(content[j:], '\n')
		var end int
		if lineEnd < 0 {
			end = len(content)
		} else {
			end = j + lineEnd
		}
		if strings.HasPrefix(content[j:end], marker) {
			code := strings.TrimSuffix(content[bodyStart:j], "\n")
			after := end + 1
			if after > len(content) {
				after = len(content)
			}
			return after, lang, code
		}
		if lineEnd < 0 {
			return -1, "", ""
		}
		j = end + 1
	}
}

// tokenizeContentFull — matnni mention, URL va ```code``` bloklariga bo'ladi.
// URL: https://, http:// yoki www. — faqat so'z chegarasida (foohttps://...).
func tokenizeContentFull(content string) []contentTok {
	var out []contentTok
	var cur strings.Builder
	i := 0
	for i < len(content) {
		c := content[i]
		// ``` yoki ''' bilan ochilgan kod bloki — faqat qator boshida
		// (yoki butun matn boshida). ''' ham qo'llab-quvvatlanadi.
		if (i == 0 || content[i-1] == '\n') && (c == '`' || c == '\'') && i+3 <= len(content) &&
			(content[i:i+3] == "```" || content[i:i+3] == "'''") {
			if cur.Len() > 0 {
				out = append(out, contentTok{kind: "text", text: cur.String()})
				cur.Reset()
			}
			if end, lang, code := codeFenceEnd(content, i); end >= 0 {
				out = append(out, contentTok{kind: "code", text: code, lang: lang})
				i = end
				continue
			}
			// Yopuvchi fence topilmadi — oddiy matn sifatida davom etamiz.
			cur.WriteByte(c)
			i++
			continue
		}
		// Server saqlagan HTML code-bloki: <pre><code data-lang="x">...</code></pre>
		// (security.Clean ichida eskap qilingan). Qaytadan pre/code DOM'ga.
		if strings.HasPrefix(content[i:], "<pre><code") {
			if cur.Len() > 0 {
				out = append(out, contentTok{kind: "text", text: cur.String()})
				cur.Reset()
			}
			// Ochilish tegi: <pre><code ...> — `>` qidiruvni <code dan keyin
			// boshlaymiz (aks holda <pre> ning `>` si adashtiradi).
			preTag := len("<pre><code")
			tagEnd := -1
			if j := strings.IndexByte(content[i+preTag:], '>'); j >= 0 {
				tagEnd = preTag + j
			}
			closeAt := strings.Index(content[i:], "</code></pre>")
			if tagEnd < 0 || closeAt < 0 {
				cur.WriteByte(c)
				i++
				continue
			}
			lang := parseCodeLangAttr(content[i : i+tagEnd])
			inner := content[i+tagEnd+1 : i+closeAt]
			text := html.UnescapeString(inner)
			if lang == "" {
				lang, text = sniffCodeLang(text)
			}
			out = append(out, contentTok{kind: "code", text: text, lang: lang})
			i = i + closeAt + len("</code></pre>")
			continue
		}
		if c == '@' || c == '$' {
			if cur.Len() > 0 {
				out = append(out, contentTok{kind: "text", text: cur.String()})
				cur.Reset()
			}
			j := i + 1
			for j < len(content) {
				ch := content[j]
				if isWordChar(ch) || ch == '-' || ch == '_' {
					j++
				} else {
					break
				}
			}
			if j > i+1 {
				out = append(out, contentTok{kind: "mention", text: content[i:j]})
				i = j
			} else {
				cur.WriteByte(c)
				i++
			}
			continue
		}
		if isURLStart(content, i) && canStartURL(content, i) {
			if cur.Len() > 0 {
				out = append(out, contentTok{kind: "text", text: cur.String()})
				cur.Reset()
			}
			end := urlEnd(content, i)
			full := content[i:end]
			href := ""
			lower := strings.ToLower(full)
			switch {
			case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"):
				href = full
			case strings.HasPrefix(lower, "www."):
				href = "https://" + full
			}
			out = append(out, contentTok{kind: "link", text: full, href: href})
			i = end
			continue
		}
		cur.WriteByte(c)
		i++
	}
	if cur.Len() > 0 {
		out = append(out, contentTok{kind: "text", text: cur.String()})
	}
	return out
}

// isURLStart — s[i:] da URL boshlanishi (https://, http://, www.) bor.
func isURLStart(s string, i int) bool {
	rest := s[i:]
	n := len(rest)
	if n >= 8 && strings.EqualFold(rest[:8], "https://") {
		return true
	}
	if n >= 7 && strings.EqualFold(rest[:7], "http://") {
		return true
	}
	if n >= 4 && strings.EqualFold(rest[:4], "www.") {
		return true
	}
	return false
}

// canStartURL — URL faqat so'z/belgilar chegarasida boshlanishi kerak
// (masalan: foohttps://x.com — havola emas).
func canStartURL(s string, i int) bool {
	if i == 0 {
		return true
	}
	prev := s[i-1]
	return !isWordChar(prev) && prev != '.' && prev != '-' && prev != '_' && prev != '/'
}

// urlEnd — URL'ning tugashini topadi (bo'sh joy/<"` gacha), oxiridagi
// tinish belgilari va muvozanatsiz yopuvchi qavslarni olib tashlaydi.
func urlEnd(s string, start int) int {
	i := start
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '<' || c == '>' ||
			c == '"' || c == '\'' || c == '`' {
			break
		}
		i++
	}
	end := i
	for end > start {
		last := s[end-1]
		switch last {
		case '.', ',', ';', ':', '!', '?', '\'', '"', '`':
			end--
			continue
		case ')':
			if countByte(s[start:end], ')') > countByte(s[start:end], '(') {
				end--
				continue
			}
		case ']':
			if countByte(s[start:end], ']') > countByte(s[start:end], '[') {
				end--
				continue
			}
		case '}':
			if countByte(s[start:end], '}') > countByte(s[start:end], '{') {
				end--
				continue
			}
		}
		break
	}
	return end
}

func countByte(s string, b byte) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			n++
		}
	}
	return n
}

func isWordChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func renderFileMsg(content js.Value, m *Msg) {
	// Rasm — inline ko'rsatiladi (preview'da maydaroq, asli bosilganda...).
	if m.Type == "image" {
		src := m.PreviewURL
		if src == "" {
			src = m.FileURL
		}
		if src = safeFileURL(src); src != "" {
			img := El("img", "file-preview image-msg")
			Attr(img, "src", src)
			Attr(img, "alt", "rasm")
			Attr(img, "loading", "lazy")
			Attr(img, "title", "Kattalashtirish")
			mm := m
			On(img, "click", func(_ js.Value, _ []js.Value) { openViewer(mm) })
			Append(content, img)
		}
		appendFileMeta(content, m)
		return
	}
	if m.Type == "video" {
		if src := safeFileURL(m.FileURL); src != "" {
			vid := El("video", "video-msg")
			Attr(vid, "src", src)
			Attr(vid, "controls", "true")
			Attr(vid, "preload", "metadata")
			Append(content, vid)
		}
		appendFileMeta(content, m)
		return
	}
	// Audio (musiqa) — Telegram-uslub player: play/pause, seek, vaqt,
	// yuklab olish, nom o'zgartirish (type "file" + .mp3/.wav/.ogg).
	if m.Type == "file" && isAudioURL(m.FileURL) {
		Append(content, renderAudioPlayer(m))
		return
	}
	// Fayl kartochkasi (audio/PDF/boshqa yoki preview'li rasm).
	if src := safeFileURL(m.PreviewURL); src != "" {
		img := El("img", "file-preview")
		Attr(img, "src", src)
		Attr(img, "alt", "preview")
		Attr(img, "loading", "lazy")
		Append(content, img)
	} else {
		icon := El("div", "file-icon")
		SetText(icon, "📄")
		Append(content, icon)
	}
	appendFileMeta(content, m)
}

// appendFileMeta — fayl nomi + hajmi + ⋮ (saqlash/rename/kattalashtirish/yuklash).
func appendFileMeta(content js.Value, m *Msg) {
	meta := El("div", "file-meta")
	nameEl := El("div", "file-name")
	name := m.Content
	if name == "" {
		name = fileBaseName(m.FileURL)
	}
	SetText(nameEl, name)
	sizeEl := El("div", "file-size")
	SetText(sizeEl, humanSize(m.FileSize))
	Append(meta, nameEl)
	Append(meta, sizeEl)
	Append(meta, buildKebab(fileMsgKebabItems(m, nameEl)))
	Append(content, meta)
}

func fileBaseName(url string) string {
	idx := strings.LastIndex(url, "/")
	if idx >= 0 && idx < len(url)-1 {
		return url[idx+1:]
	}
	return url
}

// ===== ⋮ (kebab) menyu — xabar fayllari amallari =====
// Tulma bosilganda tanlangan elementlar qatorini havoda (body) ochadi:
// "Fayllarga saqlash", "Nomini o'zgartirish", "Kattalashtirish", "Yuklab olish".
// Fayllardagi tarqoq tugmalar (⬇/✏️/⛶) o'rniga bitta ⋮ tugma — tartibliroq.

type kebabItem struct {
	text string
	fn   func()
}

var kebabOpenMenu = js.Null()
var kebabOpenBtn = js.Null()
var kebabWired bool

// buildKebab — ⋮ tugma, bosilganda body'da fiksirlangan menyu ochiladi.
func buildKebab(items []kebabItem) js.Value {
	wireKebabDismiss()
	wrap := El("span", "kebab-wrap")
	btn := El("button", "icon-btn kebab-btn")
	Attr(btn, "title", "Amallar")
	Attr(btn, "aria-label", "Amallar")
	SetText(btn, "⋮")
	On(btn, "click", func(_ js.Value, ev []js.Value) {
		if len(ev) > 0 {
			ev[0].Call("stopPropagation")
		}
		toggleKebab(items, btn)
	})
	Append(wrap, btn)
	return wrap
}

// toggleKebab — ochiq bo'lsa yopadi, aks holda bosilgan tugma yonida ochadi.
func toggleKebab(items []kebabItem, btn js.Value) {
	if !kebabOpenMenu.IsNull() && !kebabOpenMenu.IsUndefined() && kebabOpenBtn.Equal(btn) {
		closeKebab()
		return
	}
	closeKebab()
	menu := El("div", "kebab-menu")
	// Menyu ustidagi click ochiq qoldiradi (item'larning o'zi stop edi).
	On(menu, "click", func(_ js.Value, ev []js.Value) {
		if len(ev) > 0 {
			ev[0].Call("stopPropagation")
		}
	})
	for _, it := range items {
		it := it
		mi := El("button", "kebab-item")
		SetText(mi, it.text)
		On(mi, "click", func(_ js.Value, ev []js.Value) {
			if len(ev) > 0 {
				ev[0].Call("stopPropagation")
			}
			closeKebab()
			it.fn()
		})
		Append(menu, mi)
	}
	Append(document.Get("body"), menu)
	kebabOpenMenu = menu
	kebabOpenBtn = btn
	AddClass(btn, "active")
	// Joylashuv: tugma ostida, ekran chetiga sig'diriladi.
	rect := btn.Call("getBoundingClientRect")
	mw := menu.Get("offsetWidth").Float()
	mh := menu.Get("offsetHeight").Float()
	vw := js.Global().Get("innerWidth").Float()
	vh := js.Global().Get("innerHeight").Float()
	left := rect.Get("left").Float()
	top := rect.Get("bottom").Float() + 4
	if left+mw > vw-8 {
		left = vw - mw - 8
	}
	if left < 8 {
		left = 8
	}
	if top+mh > vh-8 {
		top = vh - mh - 8
	}
	menu.Get("style").Set("left", strconv.FormatFloat(left, 'f', 0, 64)+"px")
	menu.Get("style").Set("top", strconv.FormatFloat(top, 'f', 0, 64)+"px")
	menu.Get("style").Set("zIndex", "1000")
}

// closeKebab — ochiq menyuni yopadi (body'dan olib tashlaydi).
func closeKebab() {
	if kebabOpenMenu.IsNull() || kebabOpenMenu.IsUndefined() {
		return
	}
	Remove(kebabOpenMenu)
	kebabOpenMenu = js.Null()
	if !kebabOpenBtn.IsNull() && !kebabOpenBtn.IsUndefined() {
		RemoveClass(kebabOpenBtn, "active")
	}
	kebabOpenBtn = js.Null()
}

// wireKebabDismiss — tashqariga bosilganda yoki Esc'da menyuni yopadi (bir marta).
func wireKebabDismiss() {
	if kebabWired {
		return
	}
	kebabWired = true
	docClick := js.FuncOf(func(js.Value, []js.Value) any {
		closeKebab()
		return nil
	})
	document.Call("addEventListener", "click", docClick)
	keptFuncs = append(keptFuncs, docClick)
	esc := js.FuncOf(func(_ js.Value, ev []js.Value) any {
		if len(ev) > 0 && ev[0].Get("key").String() == "Escape" {
			closeKebab()
		}
		return nil
	})
	document.Call("addEventListener", "keydown", esc)
	keptFuncs = append(keptFuncs, esc)
}

// fileMsgKebabItems — xabar fayli uchun ⋮ menyu elementlari.
// "Fayllarga saqlash" faqat boshqa user fayli uchun (o'zniki allaqachon
// Fayllarimda); "Nomini o'zgartirish" faqat o'z fayli uchun.
func fileMsgKebabItems(m *Msg, nameEl js.Value) []kebabItem {
	isOwn := m.UserID != "" && store.User != nil && m.UserID == store.User.ID
	var items []kebabItem
	if !isOwn {
		items = append(items, kebabItem{text: "📂 Fayllarga saqlash", fn: func() { saveFileToMy(m) }})
	}
	if isOwn {
		items = append(items, kebabItem{text: "✏️ Nomini o'zgartirish", fn: func() { startAudioRename(nameEl, m) }})
	}
	if isViewerMedia(m) {
		items = append(items, kebabItem{text: "⛶ Kattalashtirish", fn: func() { openViewer(m) }})
	}
	items = append(items, kebabItem{text: "⬇ Yuklab olish", fn: func() { dlFromURL(m.FileURL) }})
	return items
}

// saveFileToMy — POST /api/files/save: chatdagi faylni Fayllarimga qo'shadi.
func saveFileToMy(m *Msg) {
	if m == nil || safeFileURL(m.FileURL) == "" {
		showToast(&Notif{Type: "error", Title: "Xatolik", Body: "Fayl saqlanmadi"})
		return
	}
	name := m.Content
	if name == "" {
		name = fileBaseName(m.FileURL)
	}
	go func() {
		var out FileItem
		err := fetchJSONInto("POST", "/api/files/save", map[string]any{
			"url":      m.FileURL,
			"filename": name,
		}, &out)
		if err != nil {
			showToast(&Notif{Type: "error", Title: "Xatolik", Body: "Faylni saqlab bo'lmadi"})
			return
		}
		showToast(&Notif{Type: "ok", Title: "Saqladim", Body: "Fayl Fayllarim ro'yxatiga qo'shildi"})
	}()
}

// dlFromURL — faylni yuklab olish (chalg'itadigan havolani yuklamasdan anchor click).
func dlFromURL(url string) {
	if src := safeFileURL(url); src != "" {
		a := El("a", "")
		Attr(a, "href", src)
		Attr(a, "download", fileBaseName(url))
		a.Get("style").Set("display", "none")
		Append(document.Get("body"), a)
		a.Call("click")
		Remove(a)
	}
}

// isAudioURL — fayl URL audio kengaytmada tugaydimi.
// .webm/m4a/aac/flac/opus/weba — musiqiy webm'lar ham player bo'lishi uchun.
// (Video mp4/webm odatda msg_type="video" bo'ladi, shuning uchun bu yerda
// ham ruxsat — audio-element shu fayllarni chala oladi.)
func isAudioURL(url string) bool {
	u := strings.ToLower(url)
	for _, ext := range []string{
		".mp3", ".wav", ".ogg", ".oga", ".opus",
		".m4a", ".aac", ".flac", ".weba", ".wma",
		".webm", ".mp4", ".m4b", ".aif", ".aiff",
	} {
		if strings.HasSuffix(u, ext) {
			return true
		}
	}
	return false
}

// ===== Telegram-uslub audio player =====

// activeAudioEl/Btn — hozir chalinayotgan trek (boshqasi bossa, bu to'xtaydi).
var activeAudioEl js.Value
var activeAudioBtn js.Value

// audioPlayers — har bir render qilingan player uchun registry (key → player).
// Key: m.ID bo'lsa "m<id>", bo'lmasa "c<cmid>". Keyingi/oldingi trekni
// topish va sizing uchun kerak (au DOM'ga ulanmagan, lekin registry tirik).
type audioPlayer struct {
	key   string
	box   js.Value
	au    js.Value
	play  js.Value
	fill  js.Value
	stamp js.Value
	shuff js.Value
	rep   js.Value
	m     *Msg
}

var audioPlayers = map[string]*audioPlayer{}

// audioActiveKey — hozir chalinayotgan player kaliti ("" — hech kim yo'q).
var audioActiveKey string

// shuffleOn — tasodifiy tartib (keyingisini tanlashda random); barcha
// playerlarda umumiyp (bitta tugma bosilsa hammasi yangilanadi).
var shuffleOn bool
var repeatAll bool

// audioKey — Msg uchun yagona player kaliti.
func audioKey(m *Msg) string {
	if m == nil {
		return ""
	}
	if m.ID > 0 {
		return "m" + strconv.FormatInt(m.ID, 10)
	}
	return "c" + m.ClientMsgID
}

// audioPlaylist — joriy xonadagi musiqiy (audio) xabarlar tartibda.
func audioPlaylist() []*Msg {
	var out []*Msg
	for _, m := range store.Messages {
		if m.Type == "deleted" || m.Type != "file" || m.Room != store.CurRoom {
			continue
		}
		if isAudioURL(m.FileURL) {
			out = append(out, m)
		}
	}
	return out
}

// randIdx — [0,n) oraliq tasodifiy indeks (JS Math.random — import kerak emas).
func randIdx(n int) int {
	if n <= 0 {
		return 0
	}
	r := js.Global().Get("Math").Call("random").Float()
	return int(r * float64(n))
}

// renderAudioPlayer — musiqa kartochkasi: play, seek, vaqt, kebab menyu va
// Telegram-uslub boshqaruv qatori (tasodifiy tartib, oldingi, keyingi,
// takrorlash). Trek tugagach avtomatik keyingisiga o'tadi.
func renderAudioPlayer(m *Msg) js.Value {
	box := El("div", "msg-audio")
	if m.ID > 0 {
		SetData(box, "mid", strconv.FormatInt(m.ID, 10))
	}
	if m.ClientMsgID != "" {
		SetData(box, "cmid", m.ClientMsgID)
	}

	play := El("button", "audio-play")
	SetText(play, "▶")
	Attr(play, "title", "Ijro etish")

	meta := El("div", "audio-meta")
	title := El("div", "audio-title")
	name := m.Content
	if name == "" {
		name = fileBaseName(m.FileURL)
	}
	SetText(title, name)
	sub := El("div", "audio-sub")
	performer := displayOf(m)
	if performer == "" {
		performer = m.Username
	}
	SetText(sub, performer+" · "+humanSize(m.FileSize))

	acts := El("div", "audio-acts")
	Append(acts, buildKebab(fileMsgKebabItems(m, title)))

	track := El("div", "audio-track")
	Attr(track, "title", "O'tkazish uchun bosing")
	fill := El("div", "audio-fill")
	Append(track, fill)
	stamp := El("div", "audio-time")
	SetText(stamp, "0:00")

	top := El("div", "audio-row")
	Append(top, play)
	Append(top, meta)
	Append(meta, title)
	Append(meta, sub)
	Append(top, acts)
	Append(box, top)
	Append(box, track)
	Append(box, stamp)

	// Telegram-uslub boshqaruv qatori.
	ctrls := El("div", "audio-ctrls")
	shuff := El("button", "audio-ctrl")
	Attr(shuff, "title", "Tasodifiy tartib")
	Attr(shuff, "aria-label", "Tasodifiy tartib")
	SetText(shuff, "🔀")
	prevBt := El("button", "audio-ctrl")
	Attr(prevBt, "title", "Oldingi trek")
	Attr(prevBt, "aria-label", "Oldingi trek")
	SetText(prevBt, "⏮")
	nxt := El("button", "audio-ctrl")
	Attr(nxt, "title", "Keyingi trek")
	Attr(nxt, "aria-label", "Keyingi trek")
	SetText(nxt, "⏭")
	rep := El("button", "audio-ctrl")
	Attr(rep, "title", "Ro'yxat oxirida boshidan takrorlash")
	Attr(rep, "aria-label", "Takrorlash")
	SetText(rep, "🔁")
	Append(ctrls, shuff)
	Append(ctrls, prevBt)
	Append(ctrls, nxt)
	Append(ctrls, rep)
	Append(box, ctrls)

	var au js.Value
	if src := safeFileURL(m.FileURL); src != "" {
		au = El("audio", "")
		au.Set("src", src)
		au.Set("preload", "metadata")
	} else {
		AddClass(play, "btn-disabled")
		return box
	}

	p := &audioPlayer{
		key:   audioKey(m),
		box:   box,
		au:    au,
		play:  play,
		fill:  fill,
		stamp: stamp,
		shuff: shuff,
		rep:   rep,
		m:     m,
	}
	audioPlayers[p.key] = p

	// Umumiy holat yangi render'da aks etishi uchun.
	if shuffleOn {
		AddClass(shuff, "on")
	}
	if repeatAll {
		AddClass(rep, "on")
	}
	if audioActiveKey == p.key {
		AddClass(box, "playing")
	}

	On(play, "click", func(_ js.Value, _ []js.Value) { playPause(p) })
	On(prevBt, "click", func(_ js.Value, _ []js.Value) { playAudioNext(m, true) })
	On(nxt, "click", func(_ js.Value, _ []js.Value) { playAudioNext(m, false) })
	On(shuff, "click", func(_ js.Value, _ []js.Value) {
		shuffleOn = !shuffleOn
		syncAudioState()
	})
	On(rep, "click", func(_ js.Value, _ []js.Value) {
		repeatAll = !repeatAll
		syncAudioState()
	})
	On(track, "click", func(_ js.Value, args []js.Value) {
		seekAudio(p, track, args)
	})
	// au — DOM'da emas (detached), lekin closure'lar va registry uni
	// tirik saqlaydi. Trek tugagach avtomatik keyingisiga o'tamiz.
	var onTime, onMeta, onEnded js.Func
	onTime = js.FuncOf(func(js.Value, []js.Value) any {
		dur := au.Get("duration").Float()
		cur := au.Get("currentTime").Float()
		if dur == dur && dur > 0 {
			fill.Get("style").Set("width", strconv.FormatFloat(cur/dur*100, 'f', 1, 64)+"%")
			SetText(stamp, fmtAudioTime(cur)+" / "+fmtAudioTime(dur))
		} else {
			SetText(stamp, fmtAudioTime(cur))
		}
		return nil
	})
	onMeta = js.FuncOf(func(js.Value, []js.Value) any {
		dur := au.Get("duration").Float()
		if dur == dur && dur > 0 {
			SetText(stamp, "0:00 / "+fmtAudioTime(dur))
		}
		return nil
	})
	onEnded = js.FuncOf(func(js.Value, []js.Value) any {
		SetText(play, "▶")
		fill.Get("style").Set("width", "0%")
		SetText(stamp, "0:00")
		playAudioNext(m, false)
		return nil
	})
	au.Call("addEventListener", "timeupdate", onTime)
	au.Call("addEventListener", "loadedmetadata", onMeta)
	au.Call("addEventListener", "ended", onEnded)
	return box
}

// playPause — play/pause; boshlaganda avvalgi treklarni to'xtatadi.
func playPause(p *audioPlayer) {
	if p.au.Get("paused").Bool() {
		claimAudio(p.au, p.play)
		p.au.Call("play")
		SetText(p.play, "⏸")
		audioActiveKey = p.key
		syncPlayingClass()
	} else {
		p.au.Call("pause")
		SetText(p.play, "▶")
		if audioActiveKey == p.key {
			audioActiveKey = ""
		}
		syncPlayingClass()
	}
}

// seekAudio — trek'ka bosilgan joyga o'tkazadi.
func seekAudio(p *audioPlayer, track js.Value, args []js.Value) {
	dur := p.au.Get("duration").Float()
	if len(args) == 0 || dur != dur || dur <= 0 {
		return // metadata hali yo'q
	}
	rect := track.Call("getBoundingClientRect")
	x := args[0].Get("clientX").Float() - rect.Get("left").Float()
	ratio := x / rect.Get("width").Float()
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	p.au.Set("currentTime", ratio*dur)
}

// playAudioNext — keyingi (back=false) / oldingi (back=true) trekka o'tadi.
// Shuffle yoqilgan bo'lsa tasodifiy (joriydan boshqa) trek tanlanadi; ro'yxat
// oxirida repeatAll yoqilgan bo'lsa boshiga qaytamiz, aks holda to'xtaymiz.
// O'tish bo'lsa true qaytaradi (oxirida to'xtaganda playing holatini tozalaydi).
func playAudioNext(m *Msg, back bool) bool {
	pl := audioPlaylist()
	if len(pl) < 2 {
		stopPlaying()
		return false
	}
	cur := -1
	for i, t := range pl {
		if audioKey(t) == audioKey(m) {
			cur = i
			break
		}
	}
	if cur < 0 {
		stopPlaying()
		return false
	}
	idx := cur
	if shuffleOn {
		for idx == cur {
			idx = randIdx(len(pl))
		}
	} else if back {
		idx = cur - 1
		if idx < 0 {
			idx = len(pl) - 1
		}
	} else {
		idx = cur + 1
		if idx >= len(pl) {
			if !repeatAll {
				stopPlaying() // ro'yxat tugadi, ixcham to'xtaymiz
				return false
			}
			idx = 0
		}
	}
	playTrackByKey(audioKey(pl[idx]))
	return true
}

// stopPlaying — joriy ijro tugaganda playing holatini tozalaydi (tugma ▶,
// highlight o'chadi, faol kalit bo'sh).
func stopPlaying() {
	if activeAudioEl.IsNull() || activeAudioEl.IsUndefined() {
		audioActiveKey = ""
		syncPlayingClass()
		return
	}
	func() {
		defer func() { _ = recover() }()
		activeAudioEl.Call("pause")
	}()
	if !activeAudioBtn.IsNull() && !activeAudioBtn.IsUndefined() {
		SetText(activeAudioBtn, "▶")
	}
	audioActiveKey = ""
	syncPlayingClass()
}

// playTrackByKey — registry'dagi trekni boshidan ijro qiladi.
func playTrackByKey(key string) {
	p, ok := audioPlayers[key]
	if !ok {
		return
	}
	claimAudio(p.au, p.play)
	p.au.Set("currentTime", 0)
	p.au.Call("play")
	SetText(p.play, "⏸")
	audioActiveKey = p.key
	syncPlayingClass()
}

// syncAudioState — shuffle/repeat tugmalarini barcha playerlarda yangilaydi.
func syncAudioState() {
	for _, p := range audioPlayers {
		if shuffleOn {
			AddClass(p.shuff, "on")
		} else {
			RemoveClass(p.shuff, "on")
		}
		if repeatAll {
			AddClass(p.rep, "on")
		} else {
			RemoveClass(p.rep, "on")
		}
	}
}

// syncPlayingClass — faqat hozir chalinayotgan player "playing" bilan ta'kidlanadi.
func syncPlayingClass() {
	for k, p := range audioPlayers {
		if k == audioActiveKey {
			AddClass(p.box, "playing")
		} else {
			RemoveClass(p.box, "playing")
		}
	}
}

// claimAudio — yangi trek chalinishida eskisini to'xtatadi.
func claimAudio(el, btn js.Value) {
	if !activeAudioEl.IsNull() && !activeAudioEl.IsUndefined() {
		same := false
		func() {
			defer func() { _ = recover() }()
			same = activeAudioEl.Get("src").String() != "" &&
				activeAudioEl.Get("src").String() == el.Get("src").String()
		}()
		if !same {
			activeAudioEl.Call("pause")
			if !activeAudioBtn.IsNull() && !activeAudioBtn.IsUndefined() {
				SetText(activeAudioBtn, "▶")
			}
		}
	}
	activeAudioEl = el
	activeAudioBtn = btn
}

// fmtAudioTime — soniyani m:ss ko'rinishiga.
func fmtAudioTime(sec float64) string {
	if sec != sec || sec < 0 {
		sec = 0
	}
	m := int(sec) / 60
	s := int(sec) % 60
	return strconv.Itoa(m) + ":" + fmtTwoDigits(s)
}

func fmtTwoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// startAudioRename — sarlavhani inline inputga almashtiradi; Enter/blusda saqlaydi.
func startAudioRename(titleEl js.Value, m *Msg) {
	cur := titleEl.Get("textContent").String()
	wireRenameInline(titleEl, cur, func(newName string) {
		go func() {
			var out FileItem
			if err := patchFileByURL(m.FileURL, newName, &out); err != nil {
				showToast(&Notif{Type: "error", Title: "Xatolik", Body: "Nom o'zgarmadi"})
				SetText(titleEl, cur)
				return
			}
			name := out.Filename
			if name == "" {
				name = newName
			}
			m.Content = name
			SetText(titleEl, name)
			showToast(&Notif{Type: "ok", Title: "Saqladim", Body: "Fayl nomi yangilandi"})
		}()
	})
}

// patchFileByURL — PATCH /api/files/by-url {url, filename}.
func patchFileByURL(url, filename string, out *FileItem) error {
	resp, err := fetchJSON("PATCH", "/api/files/by-url", map[string]any{
		"url":      url,
		"filename": filename,
	}, true)
	if err != nil {
		return err
	}
	return unmarshalResponse(resp, out)
}

// wireRenameInline — nom elementini inputga almashtiradi.
// Enter/blusda save(newName) chaqiriladi; Esc bekor qiladi.
func wireRenameInline(nameEl js.Value, current string, save func(string)) {
	input := El("input", "audio-rename-input")
	Attr(input, "type", "text")
	Attr(input, "value", current)
	Attr(input, "maxlength", "100")
	nameEl.Set("textContent", "")
	Append(nameEl, input)
	input.Call("focus")
	input.Call("select")
	done := false
	commit := func(ok bool) {
		if done {
			return
		}
		done = true
		if ok {
			v := strings.TrimSpace(input.Get("value").String())
			if v == "" {
				v = current
			}
			SetText(nameEl, v)
			save(v)
		} else {
			SetText(nameEl, current)
		}
	}
	On(input, "keydown", func(_ js.Value, args []js.Value) {
		if len(args) == 0 {
			return
		}
		switch args[0].Get("key").String() {
		case "Enter":
			commit(true)
		case "Escape":
			commit(false)
		}
	})
	On(input, "blur", func(_ js.Value, _ []js.Value) { commit(true) })
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return strconv.FormatFloat(float64(n)/(1<<30), 'f', 1, 64) + " GB"
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KB"
	default:
		return strconv.FormatInt(n, 10) + " B"
	}
}

func fmtTime(ms int64) string {
	if ms == 0 {
		return ""
	}
	t := time.UnixMilli(ms)
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("02.01 15:04")
}

// ===== Yuborish =====

var msgSeq int64

// newClientMsgID — takrorlanmas dedup kaliti (browser UUID).
func newClientMsgID() string {
	crypto := js.Global().Get("crypto")
	if !crypto.IsUndefined() && !crypto.Get("randomUUID").IsUndefined() {
		v := crypto.Call("randomUUID")
		if !v.IsUndefined() {
			return v.String()
		}
	}
	// Fallback: vaqt + tasodifiy.
	msgSeq++
	return strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.FormatInt(msgSeq, 10) + "-" + strconv.FormatInt(int64(time.Now().UnixNano()%100000), 10)
}

func sendComposer() {
	me := store.User
	if me == nil {
		return
	}
	offline := !wsReady()
	input := ByID("msg-input")
	if input.IsNull() || input.IsUndefined() {
		return
	}
	if input.Get("contentEditable").String() == "false" {
		return
	}
	text := strings.TrimSpace(input.Get("textContent").String())
	if text == "" {
		return
	}
	SetText(input, "")
	AddClass(input, "empty")
	cmid := newClientMsgID()

	// Mentionlar: @username / $id resolve.
	mentions := resolveMentions(text)

	// Reply quote (agar tanlangan bo'lsa).
	replyTo := replyDraftVal.msgID

	// Optimistik render (orqa fon emoji/loyiha uchun).
	opt := &Msg{
		UserID:           me.ID,
		Username:         me.Username,
		DisplayName:      me.DisplayName,
		AvatarURL:        me.AvatarURL,
		Verified:         me.Verified,
		Room:             store.CurRoom,
		Content:          text,
		Type:             "text",
		ClientMsgID:      cmid,
		Mentions:         mentions,
		ReplyTo:          replyTo,
		ReplyUserID:      replyDraftVal.userID,
		ReplyUsername:    replyDraftVal.username,
		ReplyDisplayName: replyDraftVal.name,
		ReplyContent:     replyDraftVal.content,
		ReplyType:        replyDraftVal.typ,
		CreatedAt:        time.Now().UnixMilli(),
	}
	list := ByID("messages")
	if !list.IsNull() && !list.IsUndefined() {
		row := appendMsgNode(list, opt, true)
		AddClass(row, "msg-fresh") // yuborilgan zahoti kirish animatsiyasi
		optimisticNodes[cmid] = row
		// O'z xabaringiz — pastga scroll majburiy.
		ScrollBottom(list)
	}

	payload := map[string]any{
		"type":          "msg",
		"room":          store.CurRoom,
		"content":       text,
		"client_msg_id": cmid,
		"msg_type":      "text",
		"mentions":      mentions,
	}
	if replyTo > 0 {
		payload["reply_to"] = replyTo
	}
	sendWS(payload)
	cancelReply()
	throttledTyping(false)
	if offline {
		showToast(&Notif{Type: "ok", Title: "Navbatda", Body: "Ulanish tiklanganda yuboriladi"})
	}
}

// resolveMentions — matndagi @username/$id larini user ID ro'yxatiga o'tkazadi.
func resolveMentions(text string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, tok := range tokenizeContentFull(text) {
		if tok.kind != "mention" {
			continue
		}
		t := tok.text
		if strings.HasPrefix(t, "@") {
			name := t[1:]
			if id, ok := store.ByUser[name]; ok && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		} else if strings.HasPrefix(t, "$") {
			id := t[1:]
			if _, ok := store.Users[id]; ok && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func throttledTyping(on bool) {
	now := time.Now()
	if on {
		if now.Sub(lastTypingSent) < 2*time.Second {
			return
		}
		lastTypingSent = now
	} else {
		if now.Sub(lastTypingSent) < 500*time.Millisecond && !on {
			return
		}
		lastTypingSent = now
	}
	sendWS(map[string]any{"type": "typing", "room": store.CurRoom, "on": on})
}

var typingTimer js.Value
var scrollLock bool

func onTyping(m ServerMsg) {
	bar := ByID("typing-bar")
	if bar.IsNull() || bar.IsUndefined() {
		return
	}
	if m.Room != store.CurRoom {
		return
	}
	if m.TypingOn {
		u := store.Users[m.UserID]
		if u == nil {
			return
		}
		RemoveClass(bar, "hidden")
		SetText(bar, store.DisplayName(u)+" yozmoqda")
		AddClass(bar, "typing") // nuqtalar CSS animatsiyasi (::after)
	} else {
		RemoveClass(bar, "typing")
		AddClass(bar, "hidden")
	}
}

// ===== Onlayn ro'yxat =====

func renderOnline() {
	title := ByID("online-title")
	list := ByID("online-list")
	if list.IsNull() || list.IsUndefined() {
		return
	}
	n := 0
	HTML(list, "")
	for _, u := range store.Users {
		if !u.Online {
			continue
		}
		n++
		row := El("div", "online-row")
		dot := El("span", "dot")
		if u.Devices > 1 {
			AddClass(dot, "multi")
		}
		nm := El("span", "online-name")
		SetText(nm, store.DisplayName(u))
		if u.Verified {
			badge := El("span", "verified-badge small")
			SetText(badge, "✓")
			Attr(badge, "title", "Tasdiqlangan foydalanuvchi")
			Append(nm, badge)
		}
		cnt := El("span", "online-dev")
		if u.Devices > 1 {
			SetText(cnt, "×"+strconv.Itoa(u.Devices))
		}
		Append(row, dot)
		Append(row, nm)
		Append(row, cnt)
		SetData(row, "uid", u.ID)
		On(row, "click", func(_ js.Value, _ []js.Value) {
			openProfile(u.ID, u.Username, u.DisplayName, u.AvatarURL)
		})
		Append(list, row)
	}
	if !title.IsNull() {
		SetText(title, "Onlayn — "+strconv.Itoa(n))
	}
	// DM xonasida — suhbatdoshning jonli holati header'da.
	pid := peerIDForDM(store.CurRoom)
	if pid != "" {
		sub := ByID("room-sub")
		if !sub.IsNull() && !sub.IsUndefined() {
			if u := store.Users[pid]; u != nil && u.Online {
				SetText(sub, "🟢 Onlayn")
				AddClass(sub, "online")
			} else {
				SetText(sub, "⚪ Oflayn")
				RemoveClass(sub, "online")
			}
		}
	}
}

// peerIDForDM — DM xonasidagi suhbatdoshning ID'si (o'zimiz emas).
func peerIDForDM(room string) string {
	if !strings.HasPrefix(room, "dm:") {
		return ""
	}
	parts := strings.Split(room, ":")
	if len(parts) != 3 {
		return ""
	}
	if parts[1] == store.User.ID {
		return parts[2]
	}
	return parts[1]
}

// isDMRoom — hash'dan tiklangan xona DM formatdaligini tekshiradi.
func isDMRoom(room string) bool {
	if !strings.HasPrefix(room, "dm:") {
		return false
	}
	parts := strings.Split(room, ":")
	return len(parts) == 3 && len(parts[1]) == 36 && len(parts[2]) == 36
}

// ===== Orqa fon =====

func applyChatBackground() {
	messages := ByID("messages")
	if messages.IsNull() || messages.IsUndefined() {
		return
	}
	// Shaxsiy xonalarda fon qo'llanilmaydi (global fon tanlovi o'z qoladi).
	if store.CurRoom != "" && store.CurRoom != "global" {
		messages.Get("style").Set("backgroundImage", "")
		return
	}
	// Rasm butun chat maydonini to'ldirishi uchun o'lchamlar inline
	// o'rnatiladi — eski keshlangan CSS fonni buzib ko'rsatib qo'ymasligi
	// uchun. (CSS da ham cover !important bor, lekin inline kafolat beradi.)
	// cover: har qanday ekranda fon to'liq to'ldiradi (telefonlar uchun
	// portret rasmlar mukammal mos, desktopda standart wallpaper ko'rinishi).
	messages.Get("style").Set("backgroundSize", "cover, cover")
	messages.Get("style").Set("backgroundRepeat", "no-repeat, no-repeat")
	messages.Get("style").Set("backgroundPosition", "center center, center center")
	if store.BgCurrent != "" {
		for _, bg := range store.BgList {
			if bg.ID == store.BgCurrent {
				messages.Get("style").Set("backgroundImage", "linear-gradient(rgba(9,9,11,0.35), rgba(9,9,11,0.35)), url('"+bg.URL+"')")
				return
			}
		}
	}
	messages.Get("style").Set("backgroundImage", "")
}

// ===== Chiquv =====

func doLogout() {
	go func() {
		_, _ = doFetch("POST", "/api/auth/logout", map[string]any{"refresh_token": store.RefreshToken}, true, "")
		forceLogin()
	}()
}

// cssEscape — dataset selector xavfsizligi (CSS.escape).
func cssEscape(s string) string {
	if s == "" {
		return ""
	}
	css := js.Global().Get("CSS")
	if !css.IsUndefined() && !css.IsNull() && !css.Get("escape").IsUndefined() {
		v := css.Call("escape", s)
		if !v.IsUndefined() {
			return v.String()
		}
	}
	// Fallback — CSS maxsus belgilarni qochirish.
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `]`, `\]`, `[`, `\[`, `'`, `\'`)
	return r.Replace(s)
}

// dmRoomFor — ikki foydalanuvchi orasidagi shaxsiy xona nomi.
// Server bilan mos: ID'lar alifbo tartibida, dm:a:b formati.
func dmRoomFor(a, b string) string {
	if a < b {
		return "dm:" + a + ":" + b
	}
	return "dm:" + b + ":" + a
}
