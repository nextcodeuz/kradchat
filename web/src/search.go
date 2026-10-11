//go:build js && wasm

package main

import (
	"errors"
	"strconv"
	"strings"
	"syscall/js"
	"time"
)

// Foydalanuvchi qidirish — username yoki user ID bo'yicha.
// Topilgan foydalanuvchiga xabar yuborish yoki profilini ko'rish mumkin.

type searchUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Online      bool   `json:"online"`
	Devices     int    `json:"devices"`
	LastSeen    int64  `json:"last_seen"`
	Verified    bool   `json:"verified"`
	Blocked     bool   `json:"blocked"`
	BlockedMe   bool   `json:"blocked_me"`
}

func searchLayout() string {
	return `<div class="layout">
  ` + mobileBar() + `
  <aside class="sidebar glass">
    <div class="brand">KRAD<span>CHAT</span></div>
    <div class="me-card">
      <div class="me-avatar" id="me-avatar"></div>
      <div class="me-meta">
        <div class="me-name" id="me-name"></div>
        <div class="me-sub" id="me-sub"></div>
      </div>
      <button class="icon-btn" id="btn-logout" title="Chiqish" aria-label="Chiqish"><svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor" aria-hidden="true"><path d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z"/></svg></button>
    </div>
    <nav class="side-nav">
      <button class="nav-item" id="nav-chat">💬 Umumiy chat</button>
      <button class="nav-item active" id="nav-search">🔍 Foydalanuvchi qidirish</button>
      <button class="nav-item" id="nav-files">📁 Fayllarim</button>
      <button class="nav-item" id="nav-backgrounds">🖼 Orqa fonlar</button>
      <button class="nav-item" id="nav-devices">📱 Qurilmalar</button>
      <button class="nav-item" id="nav-settings">⚙️ Sozlamalar</button>
    </nav>
  </aside>
  <main class="screen-main">
    <header class="screen-header">Foydalanuvchi qidirish</header>
    <div class="screen-body">
      <section class="panel glass">
        <h3>🔍 Qidirish</h3>
        <p class="hint">Username yoki User ID kiriting.</p>
        <div class="search-row">
          <input id="search-q" type="text" placeholder="Masalan: user123 yoki user-id..." autocapitalize="none" spellcheck="false">
          <button class="btn-primary" id="btn-search">Qidirish</button>
        </div>
        <div class="field-msg" id="search-msg"></div>
        <div id="search-result"></div>
      </section>
    </div>
  </main>
</div>`
}

func renderSearch(app js.Value) {
	HTML(app, searchLayout())
	fillMeCard()
	wireNav(app)
	q := ByID("search-q")
	submit := func() { doSearch(app) }
	On(ByID("btn-search"), "click", func(_ js.Value, _ []js.Value) { submit() })
	On(q, "keydown", enterKey(submit))
	q.Call("focus")
}

func doSearch(app js.Value) {
	q := strings.TrimSpace(GetVal(ByID("search-q")))
	q = strings.TrimPrefix(q, "@")
	msg := ByID("search-msg")
	result := ByID("search-result")
	if q == "" {
		SetText(msg, "Qidiruv so'zini kiriting")
		return
	}
	SetText(msg, "Qidirilmoqda...")
	HTML(result, "")
	enc := js.Global().Get("encodeURIComponent").Invoke(q).String()
	go func() {
		var u searchUser
		err := fetchJSONInto("GET", "/api/users/search?q="+enc, nil, &u)
		if err != nil {
			var ae *apiError
			if errors.As(err, &ae) && ae.Status == 404 {
				SetText(msg, "Foydalanuvchi topilmadi")
			} else {
				SetText(msg, "❌ "+err.Error())
			}
			return
		}
		SetText(msg, "")
		store.Users[u.ID] = &OnlineUser{
			ID: u.ID, Username: u.Username, DisplayName: u.DisplayName,
			AvatarURL: u.AvatarURL, Online: u.Online, Devices: u.Devices,
			LastSeen: u.LastSeen, Verified: u.Verified,
		}
		if u.Username != "" {
			store.ByUser[u.Username] = u.ID
		}
		renderSearchResult(result, app, &u)
	}()
}

func renderSearchResult(container js.Value, app js.Value, u *searchUser) {
	HTML(container, "")
	card := El("div", "user-result glass")

	av := El("div", "ur-avatar")
	SetText(av, "")
	if u.AvatarURL != "" {
		av.Get("style").Set("backgroundImage", "url('"+u.AvatarURL+"')")
	} else {
		SetText(av, avatarLetter(u.Username, u.DisplayName))
	}

	meta := El("div", "ur-meta")
	nameEl := El("div", "ur-name")
	SetText(nameEl, searchDisplayName(u))
	if u.Verified {
		badge := El("span", "verified-badge")
		SetText(badge, "✓")
		Attr(badge, "title", "Tasdiqlangan foydalanuvchi")
		Append(nameEl, badge)
	}
	unameEl := El("div", "ur-username")
	if u.Username != "" {
		SetText(unameEl, "@"+u.Username)
	} else {
		SetText(unameEl, "Username yashiringan")
	}
	statusEl := El("div", "ur-status")
	switch {
	case u.Blocked:
		SetText(statusEl, "🚫 Bloklangan")
	case u.BlockedMe:
		SetText(statusEl, "🚫 Sizni bloklagan")
	case u.Online:
		if u.Devices > 1 {
			SetText(statusEl, "🟢 Onlayn · "+strconv.Itoa(u.Devices)+" qurilma")
		} else {
			SetText(statusEl, "🟢 Onlayn")
		}
	case u.LastSeen > 0:
		SetText(statusEl, "⚪ Oflayn · so'nggi "+fmtLastSeen(u.LastSeen))
	default:
		SetText(statusEl, "⚪ Oflayn")
	}
	Append(meta, nameEl)
	Append(meta, unameEl)
	Append(meta, statusEl)

	actions := El("div", "ur-actions")
	if u.ID != store.User.ID {
		sendBtn := El("button", "btn-primary btn-small")
		if u.Blocked || u.BlockedMe {
			SetText(sendBtn, "🚫 Bloklangan")
			Attr(sendBtn, "disabled", "true")
			AddClass(sendBtn, "btn-disabled")
		} else {
			SetText(sendBtn, "✉ Xabar yuborish")
			On(sendBtn, "click", func(_ js.Value, _ []js.Value) {
				openDM(app, u)
			})
		}
		Append(actions, sendBtn)
	}
	profBtn := El("button", "btn-copy")
	SetText(profBtn, "👤 Profilini ko'rish")
	On(profBtn, "click", func(_ js.Value, _ []js.Value) {
		openProfile(u.ID, u.Username, u.DisplayName, u.AvatarURL)
	})
	Append(actions, profBtn)

	Append(card, av)
	Append(card, meta)
	Append(card, actions)
	Append(container, card)
}

func searchDisplayName(u *searchUser) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return "Foydalanuvchi"
}

// fmtLastSeen — timestamp (ms) ni inson o'qiydigan vaqtga aylantiradi.
func fmtLastSeen(ms int64) string {
	if ms <= 0 {
		return ""
	}
	t := time.UnixMilli(ms)
	now := time.Now()
	diff := now.Sub(t)
	switch {
	case diff < time.Minute:
		return "hozir"
	case diff < time.Hour:
		return strconv.Itoa(int(diff.Minutes())) + " daqiqa oldin"
	case t.Year() == now.Year() && t.YearDay() == now.YearDay():
		return "bugun " + t.Format("15:04")
	default:
		return t.Format("02.01 15:04")
	}
}

// openDM — qidirilgan foydalanuvchi bilan shaxsiy xona ochadi.
func openDM(app js.Value, u *searchUser) {
	room := dmRoomFor(store.User.ID, u.ID)
	title := searchDisplayName(u)
	sub := "Shaxsiy xona"
	if u.Username != "" {
		sub = "@" + u.Username
	}
	renderChatRoom(app, room, title, sub)
	showToast(&Notif{Type: "ok", Title: "Shaxsiy xona", Body: "Xabaringizni yozing"})
}
