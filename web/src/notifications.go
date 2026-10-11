//go:build js && wasm

package main

import (
	"syscall/js"
)

// Brauzer (native) push bildirishnomalari.
// Xabarlar WebSocket orqali kechikishsiz keladi; faqat ko'rsatish bu yerda.
// MUHIM: Notification API'larining HEch bir xatosi tost/panic'ga olib
// kelmasligi kerak — brauzer qo'llamasa yoki ruxsat yo'q bo'lsa jim o'tadi.

// safeJS — JS chaqiruvini panic xatosiz bajaradi. Qaytarilgan qiymat
// undefined/zero bo'lsa — muvaffaqiyatsiz degani.
func safeJS(fn func() js.Value) (v js.Value) {
	defer func() {
		if recover() != nil {
			v = js.Value{} // zero = undefined
		}
	}()
	return fn()
}

func notifSupported() bool {
	n := js.Global().Get("Notification")
	return !n.IsUndefined() && !n.IsNull() &&
		!n.Get("permission").IsUndefined() &&
		!n.Get("requestPermission").IsUndefined()
}

func notifPermission() string {
	if !notifSupported() {
		return ""
	}
	v := safeJS(func() js.Value { return js.Global().Get("Notification").Get("permission") })
	if v.IsUndefined() || v.IsNull() {
		return ""
	}
	return v.String()
}

// requestNotifPermission — brauzer ruxsatini so'raydi.
// Foydalanuvchi qadamidan (tugma bosish) chaqirilishi kerak.
// Natijaga ko'ra foydalanuvchiga yo'llanma beriladi; XATO tosti chiqmaydi.
func requestNotifPermission() {
	if !notifSupported() {
		return
	}
	if notifPermission() != "default" {
		return
	}
	n := js.Global().Get("Notification")
	p := safeJS(func() js.Value { return n.Call("requestPermission") })
	if p.IsUndefined() || p.IsNull() {
		// Eski Safari kalitli usul yoki qo'llab-quvvatlanmaydi — jim o'tamiz.
		return
	}
	// Promise natijasini kuzatamiz (xato bo'lsa ham xabar chiqmaydi).
	go func() {
		v, err := awaitJS(p)
		if err != nil {
			return
		}
		switch v.String() {
		case "denied":
			showToast(&Notif{Type: "error", Title: "Bildirishnomalar",
				Body: "Brauzer ruxsat bermadi. Sayt sozlamalaridan bildirishnomalarni yoqing."})
		case "granted":
			showToast(&Notif{Type: "ok", Title: "Bildirishnomalar",
				Body: "Yoqildi ✓ Yangi xabarlar uchun ogohlantirish olasiz."})
		}
	}()
}

// showBrowserNotif — native bildirishnoma ko'rsatadi.
// Faqat foydalanuvchi bildirishnomalarni yoqgan bo'lsa ishlaydi.
func showBrowserNotif(title, body string) {
	if store.User == nil || !store.User.NotificationsEnabled {
		return
	}
	if notifPermission() != "granted" {
		return
	}
	opts := js.Global().Get("Object").New()
	if body != "" {
		opts.Set("body", body)
	}
	opts.Set("tag", "kradchat")
	// new Notification ruxsat o'rtada o'zgargan bo'lsa ham xato bermaydi.
	safeJS(func() js.Value {
		return js.Global().Get("Notification").New(title, opts)
	})
}

// maybeNotifyNewMessage — joriy xonada boshqa foydalanuvchi yozsa va
// tab orqa fonda bo'lsa native bildirishnoma ko'rsatadi.
func maybeNotifyNewMessage(m *Msg) {
	if m == nil || m.UserID == "" {
		return
	}
	if store.User == nil || !store.User.NotificationsEnabled {
		return
	}
	if m.UserID == store.User.ID {
		return
	}
	if notifPermission() != "granted" {
		return
	}
	// Tab oldingi planda bo'lsa — toast yetarli, bezovta qilmaymiz.
	if !document.Get("hidden").Bool() {
		return
	}
	title := displayOf(m)
	body := m.Content
	if body == "" {
		switch m.Type {
		case "image":
			body = "📷 Rasm yuborildi"
		case "video":
			body = "🎬 Video yuborildi"
		default:
			body = "📎 Fayl yuborildi"
		}
	}
	if r := []rune(body); len(r) > 120 {
		body = string(r[:120]) + "…"
	}
	showBrowserNotif(title, body)
}
