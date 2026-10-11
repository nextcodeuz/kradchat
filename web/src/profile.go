//go:build js && wasm

package main

import (
	"strconv"
	"syscall/js"
)

// Profil oynasi — xabar muallifi yoki onlayn ro'yxatdan user'ni ochish,
// ID nusxalash.

var profileWired bool
var currentProfileID string
var currentBlocked bool
var currentBlockedMe bool

// initProfileModal — oyna elementlari index.html'da doimiy mavjud,
// shuning uchun boshqaruvlar bir marta bog'lanadi.
func initProfileModal() {
	if profileWired {
		return
	}
	profileWired = true
	overlay := ByID("profile-overlay")
	On(ByID("profile-close"), "click", func(_ js.Value, _ []js.Value) {
		AddClass(overlay, "hidden")
	})
	On(overlay, "click", func(_ js.Value, args []js.Value) {
		if len(args) == 0 {
			return
		}
		if args[0].Get("target").Equal(overlay) {
			AddClass(overlay, "hidden")
		}
	})
	On(ByID("btn-copy-id"), "click", func(_ js.Value, _ []js.Value) {
		id := ByID("profile-id").Get("textContent").String()
		copyToClipboard(id)
	})
	// Xabar yuborish (DM xonasini ochish).
	On(ByID("btn-profile-dm"), "click", func(_ js.Value, _ []js.Value) {
		id := currentProfileID
		if id == "" || id == store.User.ID || currentBlocked || currentBlockedMe {
			return
		}
		u := &searchUser{ID: id}
		if su, ok := store.Users[id]; ok {
			u.Username = su.Username
			u.DisplayName = su.DisplayName
			u.AvatarURL = su.AvatarURL
		}
		AddClass(ByID("profile-overlay"), "hidden")
		openDM(ByID("app"), u)
	})
	// Bloklash / blokdan olish.
	On(ByID("btn-block"), "click", func(_ js.Value, _ []js.Value) {
		target := currentProfileID
		if target == "" || target == store.User.ID {
			return
		}
		method := "POST"
		verb := "Bloklash"
		if currentBlocked {
			method = "DELETE"
			verb = "Blokdan olish"
		}
		confirmMsg := verb + " " + ByID("profile-name").Get("textContent").String() + "?"
		if !js.Global().Get("window").Call("confirm", confirmMsg).Bool() {
			return
		}
		go func() {
			var resp struct {
				Blocked bool `json:"blocked"`
			}
			if err := fetchJSONInto(method, "/api/users/"+target+"/block", nil, &resp); err != nil {
				showToast(&Notif{Type: "error", Title: "Xatolik", Body: err.Error()})
				return
			}
			currentBlocked = resp.Blocked
			if resp.Blocked {
				showToast(&Notif{Type: "ok", Title: "Bloklandi", Body: "Endi bu foydalanuvchi sizga xabar yoza olmaydi"})
			} else {
				showToast(&Notif{Type: "ok", Title: "Blokdan olindi", Body: "Xabar almashish qayta tiklandi"})
			}
			// Profil UI'ni yangilash (verified/last-seen ham qayta yuklanadi).
			go refreshProfileInfo(target)
			// Agar hozir shu user bilan DM xonada bo'lsak — chiqamiz.
			if peerIDForDM(store.CurRoom) == target {
				renderChat(ByID("app"))
			}
		}()
	})
}

// openProfile — user profil oynasini ochadi. id/username/display/avatar
// message yoki onlayn ro'yxatdan keladi; store.Users bilan birlashtiriladi.
func openProfile(id, username, display, avatar string) {
	initProfileModal()
	if u, ok := store.Users[id]; ok {
		if u.Username != "" {
			username = u.Username
		}
		if u.DisplayName != "" {
			display = u.DisplayName
		}
		if u.AvatarURL != "" {
			avatar = u.AvatarURL
		}
	}
	currentProfileID = id
	overlay := ByID("profile-overlay")
	av := ByID("profile-avatar")
	SetText(av, "")
	if avatar != "" {
		av.Get("style").Set("backgroundImage", "url('"+avatar+"')")
	} else {
		av.Get("style").Set("backgroundImage", "")
		SetText(av, avatarLetter(username, display))
	}
	nameEl := ByID("profile-name")
	SetText(nameEl, "")
	name := display
	if name == "" {
		name = "@" + username
	}
	SetText(nameEl, name)
	// Verified belgisi (store.Users'da ma'lum bo'lsa; yo'q bo'lsa keyinroq
	// refreshProfileInfo to'ldiradi).
	if u, ok := store.Users[id]; ok && u.Verified {
		badge := El("span", "verified-badge")
		SetText(badge, "✓")
		Attr(badge, "title", "Tasdiqlangan foydalanuvchi")
		Append(nameEl, badge)
	}
	if username != "" {
		SetText(ByID("profile-username"), "@"+username)
	} else {
		SetText(ByID("profile-username"), "")
	}
	status := ByID("profile-status")
	if u, ok := store.Users[id]; ok && u.Online {
		if u.Devices > 1 {
			SetText(status, "🟢 Onlayn · "+strconv.Itoa(u.Devices)+" qurilma")
		} else {
			SetText(status, "🟢 Onlayn")
		}
	} else if u, ok := store.Users[id]; ok && u.LastSeen > 0 {
		SetText(status, "⚪ Oflayn · so'nggi "+fmtLastSeen(u.LastSeen))
	} else {
		SetText(status, "⚪ Oflayn")
	}
	SetText(ByID("profile-id"), id)
	RemoveClass(overlay, "hidden")
	updateDmButton()
	// Block holati + verified/last-seen serverdan olinadi (store eskirgan bo'lishi mumkin).
	go refreshProfileInfo(id)
}

// updateDmButton — "Xabar yuborish" tugmasi holati: o'zimizga yashirin,
// bloklash o'zaro bo'lsa o'chirilgan.
func updateDmButton() {
	row := ByID("profile-dm-row")
	btn := ByID("btn-profile-dm")
	if row.IsNull() || row.IsUndefined() || btn.IsNull() || btn.IsUndefined() {
		return
	}
	if currentProfileID == "" || currentProfileID == store.User.ID {
		AddClass(row, "hidden")
		return
	}
	RemoveClass(row, "hidden")
	if currentBlocked || currentBlockedMe {
		SetText(btn, "🚫 Xabar yuborib bo'lmaydi")
		Attr(btn, "disabled", "true")
		AddClass(btn, "btn-disabled")
	} else {
		SetText(btn, "✉ Xabar yuborish")
		btn.Call("removeAttribute", "disabled")
		RemoveClass(btn, "btn-disabled")
	}
}

// refreshProfileInfo — serverdan user'ning verified/last_seen/block statusini
// olib, ochiq profil UI'ni yangilaydi.
func refreshProfileInfo(id string) {
	var u searchUser
	if err := fetchJSONInto("GET", "/api/users/search?q="+js.Global().Get("encodeURIComponent").Invoke(id).String(), nil, &u); err != nil {
		return
	}
	if u.ID == "" || u.ID != currentProfileID {
		return
	}
	// Cache'ni yangilash.
	store.Users[u.ID] = &OnlineUser{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName,
		AvatarURL: u.AvatarURL, Online: u.Online, Devices: u.Devices,
		LastSeen: u.LastSeen, Verified: u.Verified,
	}
	store.ByUser[u.Username] = u.ID
	currentBlocked = u.Blocked
	currentBlockedMe = u.BlockedMe
	// Ism + verified belgisi.
	nameEl := ByID("profile-name")
	SetText(nameEl, "")
	name := u.DisplayName
	if name == "" {
		name = "@" + u.Username
	}
	SetText(nameEl, name)
	if u.Verified {
		badge := El("span", "verified-badge")
		SetText(badge, "✓")
		Attr(badge, "title", "Tasdiqlangan foydalanuvchi")
		Append(nameEl, badge)
	}
	if u.Username != "" {
		SetText(ByID("profile-username"), "@"+u.Username)
	}
	// Holat.
	status := ByID("profile-status")
	if u.Online {
		if u.Devices > 1 {
			SetText(status, "🟢 Onlayn · "+strconv.Itoa(u.Devices)+" qurilma")
		} else {
			SetText(status, "🟢 Onlayn")
		}
	} else if u.LastSeen > 0 {
		SetText(status, "⚪ Oflayn · so'nggi "+fmtLastSeen(u.LastSeen))
	} else {
		SetText(status, "⚪ Oflayn")
	}
	// Bloklash tugmasi.
	row := ByID("profile-block-row")
	if u.ID == store.User.ID {
		AddClass(row, "hidden")
		updateDmButton()
		return
	}
	RemoveClass(row, "hidden")
	btn := ByID("btn-block")
	note := ByID("profile-block-note")
	if u.Blocked {
		SetText(btn, "✓ Blokdan olish")
		RemoveClass(btn, "btn-danger")
		AddClass(btn, "btn-primary")
		SetText(note, "Bu foydalanuvchi sizning xabarlaringizni ko'rmaydi")
	} else {
		SetText(btn, "🚫 Bloklash")
		RemoveClass(btn, "btn-primary")
		AddClass(btn, "btn-danger")
		if u.BlockedMe {
			SetText(note, "Sizni bloklagan — unga xabar yoza olmaysiz")
		} else {
			SetText(note, "")
		}
	}
	updateDmButton()
}

// copyToClipboard — navigator.clipboard, muvaffaqiyatsiz bo'lsa eski fallback.
func copyToClipboard(text string) {
	navigator := js.Global().Get("navigator")
	clipboard := navigator.Get("clipboard")
	if !clipboard.IsUndefined() && !clipboard.Get("writeText").IsUndefined() {
		// Goroutine ichida awaitJS (bloklanuvchi) — okFn/errFn callback'lari
		// kutish davomida alive qoladi; aks holda GC ularni o'chirib,
		// fallback ishlamay qolardi.
		go func() {
			p := clipboard.Call("writeText", text)
			if _, err := awaitJS(p); err != nil {
				legacyCopy(text)
				return
			}
			showToast(&Notif{Type: "ok", Title: "Nusxalandi", Body: "ID buferga ko'chirildi"})
		}()
		return
	}
	legacyCopy(text)
}

// legacyCopy — clipboard API yo'q muhitda textarea + execCommand fallback.
func legacyCopy(text string) {
	ta := document.Call("createElement", "textarea")
	ta.Set("value", text)
	style := ta.Get("style")
	style.Set("position", "fixed")
	style.Set("top", "0")
	style.Set("left", "0")
	style.Set("opacity", "0")
	Append(document.Get("body"), ta)
	ta.Call("focus")
	ta.Call("select")
	ok := document.Call("execCommand", "copy").Bool()
	Remove(ta)
	if ok {
		showToast(&Notif{Type: "ok", Title: "Nusxalandi", Body: "ID buferga ko'chirildi"})
	} else {
		showToast(&Notif{Type: "error", Title: "Xatolik", Body: "Nusxalab bo'lmadi. ID: " + text})
	}
}
