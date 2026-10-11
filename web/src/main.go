//go:build js && wasm

package main

import "syscall/js"

func main() {
	document.Set("title", "KRADCHAT — Talk to the Future")
	app := ByID("app")
	initProfileModal()

	// Sessiyani tiklash: tokenlar bor bo'lsa /api/me orqali tekshirib chatga o'tamiz.
	loadSession()
	if store.AccessToken != "" {
		bootFromSession(app)
	} else {
		renderAuth(app)
	}

	// App'ni jonli ushlab turadi — main qaytmaydi.
	select {}
}

// bootFromSession — localStorage'dagi token'ni serverda tasdiqlaydi.
// Token eskirgan/noto'g'ri bo'lsa login ekraniga qaytadi.
// (Chat to'g'ridan-to'g'ri ochilmasa nil User bilan panic bo'lardi.)
func bootFromSession(app js.Value) {
	runAsync(func() {
		var u User
		if err := fetchJSONInto("GET", "/api/me", nil, &u); err != nil {
			forceLogin()
			return
		}
		store.User = &u
		// Reload'da hash'dagi xonani tiklaymiz (DM share/deep-link).
		if room := roomFromHash(); room != "" && room != "global" {
			renderChatRoom(app, room, "Shaxsiy xona", room)
		} else {
			renderChat(app)
		}
		connectWS()
	})
}

// forceLogin — sessiyani tozalab login ekraniga qaytaradi.
func forceLogin() {
	closeWS()
	clearSession()
	pendingWS = nil
	store.AccessToken = ""
	store.RefreshToken = ""
	store.Pending = ""
	store.User = nil
	store.Messages = nil
	store.Users = make(map[string]*OnlineUser)
	store.ByUser = make(map[string]string)
	func() {
		defer func() { _ = recover() }()
		js.Global().Get("location").Set("hash", "")
	}()
	renderAuth(ByID("app"))
}

var _ = js.Value{}
