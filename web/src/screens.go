//go:build js && wasm

package main

import (
	"encoding/json"
	"html"
	"strconv"
	"strings"
	"syscall/js"
	"time"
)

// Sozlamalar, qurilmalar va orqa fon ekranlari.

func settingsLayout() string {
	me := store.User
	if me == nil {
		return ""
	}
	_ = time.Now
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
      <button class="nav-item" id="nav-search">🔍 Foydalanuvchi qidirish</button>
      <button class="nav-item" id="nav-files">📁 Fayllarim</button>
      <button class="nav-item" id="nav-backgrounds">🖼 Orqa fonlar</button>
      <button class="nav-item" id="nav-devices">📱 Qurilmalar</button>
      <button class="nav-item active" id="nav-settings">⚙️ Sozlamalar</button>
    </nav>
  </aside>
  <main class="screen-main">
    <header class="screen-header">Sozlamalar</header>
    <div class="screen-body">
      <section class="panel glass">
        <h3>Profil</h3>
        <div class="avatar-row">
          <div class="avatar-preview" id="av-preview"></div>
          <div class="avatar-actions">
            <button class="btn-primary" id="btn-choose-avatar">🖼 Rasm tanlash</button>
            <input id="av-file" type="file" accept="image/*" hidden>
            <p class="hint">JPEG, PNG, GIF yoki WEBP — 5 MB gacha.</p>
          </div>
        </div>
        <div class="field-msg" id="avatar-msg"></div>
        <div class="field">
          <label>Ko'rsatiladigan ism</label>
          <input id="set-display" type="text" value="` + html.EscapeString(me.DisplayName) + `">
        </div>
        <button class="btn-primary" id="btn-save-profile">Saqlash</button>
        <div class="field-msg" id="profile-msg"></div>
      </section>

      <section class="panel glass">
        <h3>Maxfiylik</h3>
        <div class="toggle-row">
          <label class="toggle-label">Onlayn holatni ko'rsatish</label>
          <label class="switch"><input type="checkbox" id="set-online"><span></span></label>
        </div>
        <div class="toggle-row">
          <label class="toggle-label">Oxirgi tashrifni ko'rsatish</label>
          <label class="switch"><input type="checkbox" id="set-lastseen"><span></span></label>
        </div>
        <div class="toggle-row">
          <label class="toggle-label">Username-ni yashirish</label>
          <label class="switch"><input type="checkbox" id="set-hideuser"><span></span></label>
        </div>
        <button class="btn-primary" id="btn-save-privacy">Saqlash</button>
        <div class="field-msg" id="privacy-msg"></div>
      </section>

      <section class="panel glass">
        <h3>Bildirishnomalar</h3>
        <div class="toggle-row">
          <label class="toggle-label">Bildirishnomalarni yoqish</label>
          <label class="switch"><input type="checkbox" id="set-notif"><span></span></label>
        </div>
        <button class="btn-primary" id="btn-save-notif">Saqlash</button>
        <div class="field-msg" id="notif-msg"></div>
      </section>

      <section class="panel glass">
        <h3>Ikki bosqichli himoya (2FA)</h3>
        <div id="twofa-state"></div>
        <div id="twofa-setup"></div>
        <div class="field-msg" id="twofa-msg"></div>
      </section>

      <section class="panel glass">
        <h3>Parolni o'zgartirish</h3>
        <div class="field"><label>Eski parol</label><input id="pw-old" type="password"></div>
        <div class="field"><label>Yangi parol</label><input id="pw-new" type="password"></div>
        <button class="btn-danger" id="btn-change-pw">Parolni yangilash</button>
        <div class="field-msg" id="pw-msg"></div>
        <p class="hint">Parol o'zgarganda barcha qurilmalar <b>darhol</b> uziladi.</p>
      </section>
    </div>
  </main>
</div>`
}

func renderSettings(app js.Value) {
	HTML(app, settingsLayout())
	fillMeCard()

	me := store.User
	SetChecked(ByID("set-online"), me.ShowOnlineStatus)
	SetChecked(ByID("set-lastseen"), me.ShowLastSeen)
	SetChecked(ByID("set-hideuser"), me.HideUsername)
	SetChecked(ByID("set-notif"), me.NotificationsEnabled)
	// Bildirishnomalar yoqilgan, lekin brauzer ruxsati hali olinmagan bo'lsa
	// foydalanuvchini ogohlantiramiz (ko'rsatma).
	if me.NotificationsEnabled {
		perm := notifPermission()
		switch perm {
		case "":
			SetText(ByID("notif-msg"), "Bu brauzer bildirishnomalarni qo'llamaydi")
		case "default":
			SetText(ByID("notif-msg"), "Bildirishnomalar uchun brauzer ruxsatini yoqing (tugma orqali)")
		case "denied":
			SetText(ByID("notif-msg"), "⚠ Ruxsat rad etilgan — sayt sozlamalaridan yoqing")
		}
	}

	renderTwoFAState(app)

	renderAvatarPreview()
	On(ByID("btn-choose-avatar"), "click", func(_ js.Value, _ []js.Value) {
		ByID("av-file").Call("click")
	})
	On(ByID("av-file"), "change", func(_ js.Value, _ []js.Value) {
		files := ByID("av-file").Get("files")
		if files.Length() > 0 {
			uploadAvatar(files.Index(0))
		}
	})

	On(ByID("btn-save-profile"), "click", func(_ js.Value, _ []js.Value) {
		name := GetVal(ByID("set-display"))
		saveSettings(map[string]any{"display_name": name}, ByID("profile-msg"), app)
	})
	On(ByID("btn-save-privacy"), "click", func(_ js.Value, _ []js.Value) {
		saveSettings(map[string]any{
			"show_online_status": IsChecked(ByID("set-online")),
			"show_last_seen":     IsChecked(ByID("set-lastseen")),
			"hide_username":      IsChecked(ByID("set-hideuser")),
		}, ByID("privacy-msg"), app)
	})
	On(ByID("btn-save-notif"), "click", func(_ js.Value, _ []js.Value) {
		enabled := IsChecked(ByID("set-notif"))
		if enabled && notifPermission() == "default" {
			requestNotifPermission()
		} else if enabled && notifPermission() == "denied" {
			SetText(ByID("notif-msg"), "⚠ Brauzer bildirishnomalarga ruxsat bermagan — sayt sozlamalaridan yoqing")
		}
		saveSettings(map[string]any{
			"notifications_enabled": enabled,
		}, ByID("notif-msg"), app)
	})
	// Bildirishnomalar yoqilganda brauzer ruxsatini darhol so'raymiz.
	On(ByID("set-notif"), "change", func(_ js.Value, _ []js.Value) {
		if IsChecked(ByID("set-notif")) {
			requestNotifPermission()
		}
	})
	On(ByID("btn-change-pw"), "click", func(_ js.Value, _ []js.Value) {
		changePassword()
	})

	wireNav(app)
}

func saveSettings(payload map[string]any, msg js.Value, app js.Value) {
	SetText(msg, "Saqlanmoqda...")
	go func() {
		var u User
		if err := fetchJSONInto("PATCH", "/api/me", payload, &u); err != nil {
			SetText(msg, "❌ "+err.Error())
			return
		}
		store.User = &u
		SetText(msg, "✅ Saqlandi")
		js.Global().Call("setTimeout", js.FuncOf(func(js.Value, []js.Value) any {
			SetText(msg, "")
			return nil
		}), 2000)
	}()
}

// renderAvatarPreview — sozlamalardagi profil rasm ko'rinishini to'ldiradi.
func renderAvatarPreview() {
	av := ByID("av-preview")
	if av.IsNull() || av.IsUndefined() {
		return
	}
	me := store.User
	SetText(av, "")
	if me != nil && me.AvatarURL != "" {
		av.Get("style").Set("backgroundImage", "url('"+me.AvatarURL+"')")
	} else {
		av.Get("style").Set("backgroundImage", "")
		if me != nil {
			SetText(av, avatarLetter(me.Username, me.DisplayName))
		}
	}
}

// uploadAvatar — tanlangan rasmni /api/me/avatar ga yuboradi.
func uploadAvatar(file js.Value) {
	msg := ByID("avatar-msg")
	if msg.IsNull() {
		return
	}
	SetText(msg, "Yuklanmoqda...")
	go func() {
		dataCh := make(chan []byte, 1)
		readBytes(file, dataCh)
		data := <-dataCh
		if data == nil {
			SetText(msg, "❌ Faylni o'qib bo'lmadi")
			return
		}
		u, err := postAvatar(data)
		if err != nil {
			SetText(msg, "❌ "+err.Error())
			return
		}
		store.User = &u
		SetText(msg, "✅ Rasm saqlandi")
		renderAvatarPreview()
		fillMeCard()
		js.Global().Call("setTimeout", js.FuncOf(func(js.Value, []js.Value) any {
			SetText(msg, "")
			return nil
		}), 2000)
	}()
}

// postAvatar — raw rasm baytlarini yuboradi; 401 bo'lsa refresh + retry.
func postAvatar(data []byte) (User, error) {
	var u User
	attempts := 0
	for {
		opts := js.Global().Get("Object").New()
		opts.Set("method", "POST")
		headers := js.Global().Get("Object").New()
		headers.Set("Authorization", "Bearer "+store.AccessToken)
		headers.Set("Accept", "application/json")
		opts.Set("headers", headers)
		u8 := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(u8, data)
		opts.Set("body", u8)

		respV, err := awaitJS(js.Global().Call("fetch", "/api/me/avatar", opts))
		if err != nil {
			return u, err
		}
		if respV.Get("status").Int() == 401 && store.RefreshToken != "" && attempts == 0 {
			attempts++
			if tryRefresh() {
				continue
			}
		}
		if !respV.Get("ok").Bool() {
			if t, err := awaitJS(respV.Call("text")); err == nil {
				var er struct {
					Error string `json:"error"`
				}
				if json.Unmarshal([]byte(t.String()), &er) == nil && er.Error != "" {
					return u, &apiError{Message: er.Error, Status: respV.Get("status").Int()}
				}
			}
			return u, &apiError{Message: respV.Get("status").String(), Status: respV.Get("status").Int()}
		}
		if err := unmarshalResponse(respV, &u); err != nil {
			return u, err
		}
		return u, nil
	}
}

func changePassword() {
	oldP := GetVal(ByID("pw-old"))
	newP := GetVal(ByID("pw-new"))
	msg := ByID("pw-msg")
	if oldP == "" || newP == "" {
		SetText(msg, "Ikkala maydonni to'ldiring")
		return
	}
	if len(newP) < 8 {
		SetText(msg, "Yangi parol kamida 8 belgi")
		return
	}
	SetText(msg, "Yuklanmoqda...")
	go func() {
		resp, err := fetchJSON("POST", "/api/auth/password", map[string]any{
			"old_password": oldP,
			"new_password": newP,
		}, true)
		if err != nil {
			SetText(msg, "❌ "+err.Error())
			return
		}
		SetText(msg, "✅ Parol yangilandi. Barcha qurilmalar uzilmoqda...")
		// Server barcha sessionlarni revoke qiladi va force_logout yuboradi.
		_ = resp
	}()
}

// ===== 2FA =====

func renderTwoFAState(app js.Value) {
	me := store.User
	state := ByID("twofa-state")
	setup := ByID("twofa-setup")
	if me.TwoFAEnabled {
		HTML(state, `<div class="badge badge-on">✓ 2FA yoqilgan</div>`)
		HTML(setup, `<div class="field"><label>Parol</label><input id="tfa-pass" type="password"></div>
<div class="field"><label>6 xonali kod</label><input id="tfa-code" type="text" inputmode="numeric"></div>
<button class="btn-danger" id="btn-tfa-off">O'chirish</button>`)
		On(ByID("btn-tfa-off"), "click", func(_ js.Value, _ []js.Value) {
			disable2FA(app)
		})
	} else {
		HTML(state, `<div class="badge badge-off">✗ 2FA o'chirilgan</div>`)
		HTML(setup, `<div class="field"><label>Parol</label><input id="tfa-pass" type="password"></div>
<button class="btn-primary" id="btn-tfa-on">Yoqish</button>`)
		On(ByID("btn-tfa-on"), "click", func(_ js.Value, _ []js.Value) {
			setup2FA(app)
		})
	}
}

func setup2FA(app js.Value) {
	pass := GetVal(ByID("tfa-pass"))
	if pass == "" {
		SetText(ByID("twofa-msg"), "Parolni kiriting")
		return
	}
	SetText(ByID("twofa-msg"), "Tayyorlanmoqda...")
	go func() {
		var out struct {
			Secret    string `json:"secret"`
			OTPAuth   string `json:"otpauth_uri"`
			QRDataURL string `json:"qr_data_url"`
		}
		if err := fetchJSONInto("POST", "/api/auth/twofa/setup", map[string]any{"password": pass}, &out); err != nil {
			SetText(ByID("twofa-msg"), "❌ "+err.Error())
			return
		}
		HTML(ByID("twofa-state"), `<div class="badge badge-warn">2FA yoqilmoqda</div>`)
		qr := out.QRDataURL
		if !strings.HasPrefix(qr, "data:image/") {
			qr = ""
		}
		HTML(ByID("twofa-setup"), `
<input type="hidden" id="tfa-pass" value="`+html.EscapeString(pass)+`">
<div class="qr-box"><img id="tfa-qr" src="`+qr+`" alt="QR"></div>
<p class="hint">Authenticator ilovasida skanerlang. Maxfiy kalit: <code id="tfa-secret">`+html.EscapeString(out.Secret)+`</code></p>
<div class="field"><label>6 xonali kodni kiriting</label><input id="tfa-code" type="text" inputmode="numeric"></div>
<button class="btn-primary" id="btn-tfa-confirm">Tasdiqlash</button>`)
		SetText(ByID("twofa-msg"), "")
		On(ByID("btn-tfa-confirm"), "click", func(_ js.Value, _ []js.Value) {
			enable2FA(app)
		})
	}()
}

func enable2FA(app js.Value) {
	code := GetVal(ByID("tfa-code"))
	pass := GetVal(ByID("tfa-pass"))
	if len(code) < 6 {
		SetText(ByID("twofa-msg"), "6 xonali kod kiriting")
		return
	}
	if pass == "" {
		SetText(ByID("twofa-msg"), "Parolni kiriting")
		return
	}
	go func() {
		if _, err := fetchJSON("POST", "/api/auth/twofa/enable", map[string]any{"code": code, "password": pass}, true); err != nil {
			SetText(ByID("twofa-msg"), "❌ "+err.Error())
			return
		}
		store.User.TwoFAEnabled = true
		SetText(ByID("twofa-msg"), "✅ 2FA yoqildi")
		renderTwoFAState(app)
	}()
}

func disable2FA(app js.Value) {
	pass := GetVal(ByID("tfa-pass"))
	code := GetVal(ByID("tfa-code"))
	if pass == "" || len(code) < 6 {
		SetText(ByID("twofa-msg"), "Parol va kodni kiriting")
		return
	}
	go func() {
		if _, err := fetchJSON("POST", "/api/auth/twofa/disable", map[string]any{"password": pass, "code": code}, true); err != nil {
			SetText(ByID("twofa-msg"), "❌ "+err.Error())
			return
		}
		store.User.TwoFAEnabled = false
		SetText(ByID("twofa-msg"), "✅ 2FA o'chirildi")
		renderTwoFAState(app)
	}()
}

// ===== Qurilmalar =====

func renderDevices(app js.Value) {
	HTML(app, devicesLayout())
	fillMeCard()
	SetText(ByID("devices-msg"), "Yuklanmoqda...")
	go func() {
		var out struct {
			Devices []*Device `json:"devices"`
		}
		if err := fetchJSONInto("GET", "/api/devices", nil, &out); err != nil {
			SetText(ByID("devices-msg"), "❌ "+err.Error())
			return
		}
		SetText(ByID("devices-msg"), "")
		list := ByID("devices-list")
		HTML(list, "")
		curID := "" // joriy device_id noma'lum — barchasiga tugma beramiz
		_ = curID
		for _, d := range out.Devices {
			row := El("div", "device-row glass")
			icon := El("div", "device-icon")
			SetText(icon, deviceIcon(d.DeviceType))
			meta := El("div", "device-meta")
			nm := El("div", "device-name")
			SetText(nm, deviceTitle(d))
			info := El("div", "device-info")
			SetText(info, d.IP+" · "+d.OS+" · "+d.Browser+" · "+(func() string {
				if d.IsActive {
					return "faol"
				}
				return "faol emas"
			})()+" · "+shortTime(d.LastSeen))
			Append(meta, nm)
			Append(meta, info)
			del := El("button", "btn-danger btn-small")
			SetText(del, "Uzish")
			SetData(del, "devid", d.ID)
			On(del, "click", func(_ js.Value, _ []js.Value) {
				terminateDevice(GetData(del, "devid"), app)
			})
			Append(row, icon)
			Append(row, meta)
			Append(row, del)
			Append(list, row)
		}
		if len(out.Devices) == 0 {
			SetText(list, "Qurilmalar yo'q")
		}
	}()
	wireNav(app)
}

func devicesLayout() string {
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
      <button class="nav-item" id="nav-search">🔍 Foydalanuvchi qidirish</button>
      <button class="nav-item" id="nav-files">📁 Fayllarim</button>
      <button class="nav-item" id="nav-backgrounds">🖼 Orqa fonlar</button>
      <button class="nav-item active" id="nav-devices">📱 Qurilmalar</button>
      <button class="nav-item" id="nav-settings">⚙️ Sozlamalar</button>
    </nav>
  </aside>
  <main class="screen-main">
    <header class="screen-header">Qurilmalar</header>
    <div class="screen-body">
      <section class="panel glass">
        <h3>Ulangan qurilmalar</h3>
        <p class="hint">Barcha qurilmalaringiz — IP va brauzer ma'lumotlari bilan.</p>
        <div id="devices-list"></div>
        <div class="field-msg" id="devices-msg"></div>
      </section>
    </div>
  </main>
</div>`
}

func terminateDevice(id string, app js.Value) {
	go func() {
		if _, err := fetchJSON("DELETE", "/api/devices/"+id, nil, true); err != nil {
			showToast(&Notif{Type: "error", Title: "Xatolik", Body: err.Error()})
			return
		}
		renderDevices(app)
	}()
}

func deviceTitle(d *Device) string {
	if d.DeviceType != "" {
		return d.DeviceType
	}
	return "Qurilma"
}

func deviceIcon(t string) string {
	switch t {
	case "smartphone":
		return "📱"
	case "tablet":
		return "📲"
	case "desktop":
		return "🖥"
	case "laptop":
		return "💻"
	default:
		return "📟"
	}
}

func shortTime(s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Format("02.01 15:04")
}

// ===== Orqa fonlar =====

func renderBackgrounds(app js.Value) {
	HTML(app, backgroundsLayout())
	fillMeCard()
	SetText(ByID("bg-msg"), "Yuklanmoqda...")
	go func() {
		// Server javob shakli: {"backgrounds": [...]} — array EMAS.
		var out struct {
			Backgrounds []*BG `json:"backgrounds"`
		}
		if err := fetchJSONInto("GET", "/api/backgrounds", nil, &out); err != nil {
			SetText(ByID("bg-msg"), "❌ "+err.Error())
			return
		}
		store.BgList = out.Backgrounds
		SetText(ByID("bg-msg"), "")
		renderBgGrid()
	}()
	wireNav(app)
}

func backgroundsLayout() string {
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
      <button class="nav-item" id="nav-search">🔍 Foydalanuvchi qidirish</button>
      <button class="nav-item" id="nav-files">📁 Fayllarim</button>
      <button class="nav-item active" id="nav-backgrounds">🖼 Orqa fonlar</button>
      <button class="nav-item" id="nav-devices">📱 Qurilmalar</button>
      <button class="nav-item" id="nav-settings">⚙️ Sozlamalar</button>
    </nav>
  </aside>
  <main class="screen-main">
    <header class="screen-header">Chat orqa fonlari</header>
    <div class="screen-body">
      <section class="panel glass">
        <h3>Fondan tanlang</h3>
        <p class="hint">Yangi rasmlar <code>backgrounds/</code> papkasiga tushganda avtomatik qo'shiladi.</p>
        <div class="bg-grid" id="bg-grid"></div>
        <div class="field-msg" id="bg-msg"></div>
      </section>
    </div>
  </main>
</div>`
}

func renderBgGrid() {
	grid := ByID("bg-grid")
	if grid.IsNull() {
		return
	}
	HTML(grid, "")
	// "Fonsiz" varianti
	none := El("div", "bg-card")
	none.Get("style").Set("background", "#0d0d10")
	txt := El("div", "bg-card-label")
	SetText(txt, "Fonsiz")
	Append(none, txt)
	if store.BgCurrent == "" {
		AddClass(none, "selected")
	}
	On(none, "click", func(_ js.Value, _ []js.Value) {
		clearBackground()
	})
	Append(grid, none)

	for _, bg := range store.BgList {
		card := El("div", "bg-card")
		card.Get("style").Set("backgroundImage", "url('"+bg.URL+"')")
		if bg.ID == store.BgCurrent {
			AddClass(card, "selected")
		}
		label := El("div", "bg-card-label")
		SetText(label, bg.Name)
		Append(card, label)
		SetData(card, "bgid", bg.ID)
		On(card, "click", func(_ js.Value, _ []js.Value) {
			setBackground(GetData(card, "bgid"))
		})
		Append(grid, card)
	}
}

func setBackground(id string) {
	store.BgCurrent = id
	applyChatBackground()
	// Fon tanlovi faqat global xona uchun (shaxsiy xonalarda fon yo'q).
	sendWS(map[string]any{"type": "bg", "room": "global", "background_id": id})
	renderBgGrid()
	showToast(&Notif{Type: "ok", Title: "Orqa fon", Body: "Fon almashtirildi"})
}

func clearBackground() {
	store.BgCurrent = ""
	applyChatBackground()
	sendWS(map[string]any{"type": "bg", "room": "global", "background_id": "none"})
	renderBgGrid()
}

// ===== Umumiy =====

func fillMeCard() {
	me := store.User
	name := ByID("me-name")
	sub := ByID("me-sub")
	av := ByID("me-avatar")
	if name.IsNull() {
		return
	}
	if me == nil {
		return
	}
	SetText(name, me.DisplayName)
	if me.DisplayName == "" {
		SetText(name, "@"+me.Username)
	}
	SetText(sub, "id: "+me.ID)
	if me.AvatarURL != "" {
		av.Get("style").Set("backgroundImage", "url('"+me.AvatarURL+"')")
		SetText(av, "")
	} else {
		r := []rune(me.Username)
		if len(r) > 0 {
			SetText(av, strings.ToUpper(string(r[0])))
		}
	}
}

func refreshMe() {
	go func() {
		var u User
		if err := fetchJSONInto("GET", "/api/me", nil, &u); err != nil {
			return
		}
		store.User = &u
		fillMeCard()
	}()
}

// wireNav — yon paneldagi navigatsiya + logout.
func wireNav(app js.Value) {
	nav := func(target func(js.Value)) func(js.Value, []js.Value) {
		return func(_ js.Value, _ []js.Value) { target(app) }
	}
	On(ByID("nav-chat"), "click", nav(renderChat))
	On(ByID("nav-search"), "click", nav(renderSearch))
	On(ByID("nav-files"), "click", nav(renderFiles))
	On(ByID("nav-backgrounds"), "click", nav(renderBackgrounds))
	On(ByID("nav-devices"), "click", nav(renderDevices))
	On(ByID("nav-settings"), "click", nav(renderSettings))
	On(ByID("btn-logout"), "click", func(_ js.Value, _ []js.Value) { doLogout() })
	wireMobileBar(app)
}

// mobileBar — tor ekranlarda ko'rsatiladigan yuqori panel (hamburger).
func mobileBar() string {
	return `<div class="mobile-bar">
  <button class="icon-btn" id="btn-menu" title="Menyu">☰</button>
  <div class="mobile-brand">KRAD<span>CHAT</span></div>
</div>`
}

// wireMobileBar — hamburger tugmasi orqali sidebar drawer'ini ochadi/yopadi.
// Chat ekranida gamburger chat-header ichida (btn-menu-chat), boshqa
// ekranlarda mobile-bar ichida (btn-menu) — ikkalasi ham ulanadi.
func wireMobileBar(app js.Value) {
	for _, id := range []string{"btn-menu", "btn-menu-chat"} {
		btn := ByID(id)
		if btn.IsNull() || btn.IsUndefined() {
			continue
		}
		On(btn, "click", func(_ js.Value, _ []js.Value) {
			toggleSidebar()
		})
	}
	On(app, "click", func(_ js.Value, args []js.Value) {
		if len(args) == 0 {
			return
		}
		sidebar := Q(".sidebar")
		if sidebar.IsNull() || sidebar.IsUndefined() {
			return
		}
		if !sidebar.Get("classList").Call("contains", "open").Bool() {
			return
		}
		target := args[0].Get("target")
		if target.IsUndefined() || target.IsNull() {
			return
		}
		// Hamburger / mobil panelga bosish drawer'ni DARHOL yopmasligi kerak.
		// Aks holda toggle: ochiladi + app handler yopadi — sidebar umuman ochilmaydi.
		// (Chat header'dagi btn-menu-chat ham shu yerda — u .mobile-bar ichida emas.)
		if !target.Call("closest", ".mobile-bar,.head-menu").IsNull() {
			return
		}
		if target.Call("closest", ".sidebar").IsNull() {
			sidebar.Get("classList").Call("remove", "open")
		}
	})
}

func toggleSidebar() {
	sidebar := Q(".sidebar")
	if sidebar.IsNull() || sidebar.IsUndefined() {
		return
	}
	if sidebar.Get("classList").Call("contains", "open").Bool() {
		sidebar.Get("classList").Call("remove", "open")
	} else {
		sidebar.Get("classList").Call("add", "open")
	}
}

var _ = strconv.Itoa
