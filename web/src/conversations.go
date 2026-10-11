//go:build js && wasm

package main

import (
	"html"
	"strings"
	"syscall/js"
)

// Shaxsiy (DM) suhbatlar ro'yxati — "+" tugmasi orqali ochiladi.
// Kimlar bilan yozishgan bo'lsa, o'sha userlar oxirgi xabari bilan ko'rsatiladi.

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

// convFabHTML — chat ekranidagi "+" tugmasi va Shaxsiy menyusi.
func convFabHTML() string {
	return `<button class="fab" id="btn-fab" title="Shaxsiy suhbatlar" aria-label="Shaxsiy suhbatlar">
  <svg viewBox="0 0 24 24" width="26" height="26" fill="currentColor" aria-hidden="true"><path d="M20 2H4c-1.1 0-2 .9-2 2v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm0 14H5.17L4 17.17V4h16v12zM7 9h10c.55 0 1 .45 1 1s-.45 1-1 1H7c-.55 0-1-.45-1-1s.45-1 1-1zm6 5H7c-.55 0-1-.45-1-1s.45-1 1-1h6c.55 0 1 .45 1 1s-.45 1-1 1zm4-6H7c-.55 0-1-.45-1-1s.45-1 1-1h10c.55 0 1 .45 1 1s-.45 1-1 1z"/></svg>
</button>
<div class="conv-menu hidden" id="conv-menu">
  <div class="conv-menu-head">
    <span>💬 Shaxsiy suhbatlar</span>
    <button class="icon-btn" id="btn-conv-close" title="Yopish">✕</button>
  </div>
  <div class="conv-list" id="conv-list"></div>
</div>`
}

// wireConvFab — "+" tugmasi va menyu hodisalarini ulaydi.
func wireConvFab(app js.Value) {
	fab := ByID("btn-fab")
	if fab.IsNull() || fab.IsUndefined() {
		return
	}
	On(fab, "click", func(_ js.Value, _ []js.Value) { toggleConvMenu(app) })
	On(ByID("btn-conv-close"), "click", func(_ js.Value, _ []js.Value) { closeConvMenu() })
	// Tashqariga bosilganda yopiladi (FAB va menyuning o'ziga bosish hisobga olinmaydi).
	On(JS("document"), "click", func(_ js.Value, args []js.Value) {
		if len(args) == 0 {
			return
		}
		target := args[0].Get("target")
		if target.IsUndefined() || target.IsNull() {
			return
		}
		if !target.Call("closest", "#btn-fab").IsNull() {
			return
		}
		if !target.Call("closest", "#conv-menu").IsNull() {
			return
		}
		closeConvMenu()
	})
}

func toggleConvMenu(app js.Value) {
	menu := ByID("conv-menu")
	if menu.IsNull() || menu.IsUndefined() {
		return
	}
	if menu.Get("classList").Call("contains", "hidden").Bool() {
		RemoveClass(menu, "hidden")
		loadConvList(app)
	} else {
		AddClass(menu, "hidden")
	}
}

func closeConvMenu() {
	menu := ByID("conv-menu")
	if menu.IsNull() || menu.IsUndefined() {
		return
	}
	AddClass(menu, "hidden")
}

// loadConvList — /api/conversations dan suhbatlar ro'yxatini olib ko'rsatadi.
func loadConvList(app js.Value) {
	list := ByID("conv-list")
	if list.IsNull() || list.IsUndefined() {
		return
	}
	HTML(list, `<div class="conv-loading">Yuklanmoqda...</div>`)
	go func() {
		var resp struct {
			Conversations []Conversation `json:"conversations"`
		}
		if err := fetchJSONInto("GET", "/api/conversations", nil, &resp); err != nil {
			HTML(list, `<div class="conv-empty">❌ `+htmlEscape(err.Error())+`</div>`)
			return
		}
		renderConvList(list, app, resp.Conversations)
	}()
}

func renderConvList(list js.Value, app js.Value, convs []Conversation) {
	if len(convs) == 0 {
		HTML(list, `<div class="conv-empty">Hali suhbatlar yo'q.<br>Foydalanuvchi qidirish orqali xabar yuboring.</div>`)
		return
	}
	HTML(list, "")
	for i := range convs {
		Append(list, convItemEl(app, &convs[i]))
	}
}

func convItemEl(app js.Value, c *Conversation) js.Value {
	item := El("div", "conv-item")

	av := El("div", "conv-avatar")
	if c.AvatarURL != "" {
		av.Get("style").Set("backgroundImage", "url('"+c.AvatarURL+"')")
	} else {
		SetText(av, avatarLetter(c.Username, c.DisplayName))
	}

	meta := El("div", "conv-meta")
	nameRow := El("div", "conv-name-row")
	nameEl := El("div", "conv-name")
	SetText(nameEl, convDisplayName(c))
	if c.Verified {
		badge := El("span", "verified-badge")
		SetText(badge, "✓")
		Attr(badge, "title", "Tasdiqlangan foydalanuvchi")
		Append(nameEl, badge)
	}
	if c.Online {
		dot := El("span", "conv-dot online")
		Attr(dot, "title", "Onlayn")
		Append(nameRow, dot)
	} else if c.LastSeen > 0 {
		dot := El("span", "conv-dot")
		Attr(dot, "title", "So'nggi "+fmtLastSeen(c.LastSeen))
		Append(nameRow, dot)
	}
	timeEl := El("span", "conv-time")
	if c.LastTime > 0 {
		SetText(timeEl, fmtLastSeen(c.LastTime))
	}
	Append(nameRow, nameEl)
	Append(nameRow, timeEl)

	preview := El("div", "conv-preview")
	switch c.LastType {
	case "image":
		SetText(preview, "🖼 Rasm")
	case "video":
		SetText(preview, "🎬 Video")
	case "file":
		SetText(preview, "📎 Fayl")
	case "system":
		SetText(preview, html.UnescapeString(c.LastContent))
	default:
		SetText(preview, html.UnescapeString(convTruncate(c.LastContent)))
	}
	if c.Blocked {
		SetText(preview, "🚫 Bloklangan")
	} else if c.BlockedMe {
		SetText(preview, "🚫 Sizni bloklagan")
	}

	Append(meta, nameRow)
	Append(meta, preview)
	Append(item, av)
	Append(item, meta)

	On(item, "click", func(_ js.Value, _ []js.Value) { openConv(app, c) })
	return item
}

func openConv(app js.Value, c *Conversation) {
	closeConvMenu()
	title := convDisplayName(c)
	sub := "Shaxsiy xona"
	if c.Username != "" {
		sub = "@" + c.Username
	}
	renderChatRoom(app, c.Room, title, sub)
}

func convDisplayName(c *Conversation) string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	if c.Username != "" {
		return "@" + c.Username
	}
	return "Foydalanuvchi"
}

// convTruncate — oxirgi xabar matnini bir qatorli ixcham ko'rinishga keltiradi.
func convTruncate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "…"
	}
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) > 60 {
		return string(runes[:60]) + "…"
	}
	return s
}

// htmlEscape — ro'yxat xatosini to'g'ri ko'rsatish uchun minimal escape.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
