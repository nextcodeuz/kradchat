//go:build js && wasm

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"syscall/js"
	"time"
)

// WebSocket client — reconnect bilan.

type WSClient struct {
	conn      js.Value
	funcs     []js.Func
	closed    bool
	reconnect int
	backoff   int
}

var ws *WSClient

// pendingWS — ulanish yo'q paytda yuborilgan xabarlar navbati (offline).
// Composer'dan tashqari typing/fon/fayl-xabarlar izsiz yo'qolmasligi uchun.
// Cap 50 — cheksiz o'sish bloklanadi.
var pendingWS []string

func connectWS() {
	if ws != nil && !ws.closed {
		return
	}
	ws = &WSClient{}
	ws.funcs = []js.Func{}
	ws.backoff = 0

	protocol := "wss"
	if js.Global().Get("location").Get("protocol").String() == "http:" {
		protocol = "ws"
	}
	host := js.Global().Get("location").Get("host").String()
	url := protocol + "://" + host + "/ws?token=" + store.AccessToken
	ws.conn = js.Global().Get("WebSocket").New(url)

	ws.addHandler("onopen", func(_ js.Value, _ []js.Value) {
		ws.backoff = 0
		// Offline navbatni bo'shatamiz (reconnect'da yozilganlar ketadi).
		flushPendingWS()
		// history + online so'raladi
		sendWS(map[string]any{"type": "history", "room": store.CurRoom, "cursor": 0})
		// Fon tanlovi faqat global xona uchun. DM xonada turgan paytda
		// qayta ulanish bo'lsa ham "global" yuboriladi — aks holda server
		// "dm:..." xonani rad etib, xatolik tosti chiqardi.
		if store.BgCurrent != "" {
			sendWS(map[string]any{"type": "bg", "background_id": store.BgCurrent, "room": "global"})
		}
	})
	ws.addHandler("onmessage", func(_ js.Value, args []js.Value) {
		defer func() {
			if r := recover(); r != nil {
				showToast(&Notif{Type: "error", Title: "WS xatosi", Body: fmt.Sprint(r)})
			}
		}()
		if len(args) == 0 {
			return
		}
		raw := args[0].Get("data").String()
		handleWSMessage(raw)
	})
	ws.addHandler("onclose", func(_ js.Value, _ []js.Value) {
		ws.closed = true
		ws.release()
		if ws.reconnect < 5 {
			ws.reconnect++
		}
		delay := 1000 << ws.reconnect
		if delay > 15000 {
			delay = 15000
		}
		var timerFn js.Func
		timerFn = js.FuncOf(func(js.Value, []js.Value) any {
			defer timerFn.Release()
			if store.AccessToken != "" {
				// Access token eskirgan bo'lsa (JWT exp) avval refresh —
				// aks holda server qayta ulanishni rad etadi va chat abadiy o'chib qolardi.
				// runAsync: tryRefresh async fetch qiladi (awaitJS bloklanishi mumkin),
				// shuning uchun main goroutinani (js callback) bloklamaymiz.
				runAsync(func() {
					if store.RefreshToken != "" && accessTokenExpired() {
						tryRefresh()
					}
					connectWS()
				})
			}
			return nil
		})
		js.Global().Call("setTimeout", timerFn, delay)
	})
	ws.addHandler("onerror", func(_ js.Value, _ []js.Value) {
		// onclose o'zi hal qiladi
	})
}

func (w *WSClient) addHandler(name string, fn func(js.Value, []js.Value)) {
	f := js.FuncOf(func(this js.Value, args []js.Value) any {
		fn(this, args)
		return nil
	})
	w.funcs = append(w.funcs, f)
	w.conn.Set(name, f)
}

func (w *WSClient) release() {
	for _, f := range w.funcs {
		f.Release()
	}
	w.funcs = nil
}

// sendWS — WS orqali JSON yuborish. Ulanmagan bo'lsa navbatga qo'shadi
// (offline paytda yozilganlar reconnect'da ketadi). Typing xabarlar navbatga
// qo'shilmaydi — ular eskiradi.
func sendWS(payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if !wsReady() {
		if isTypingPayload(payload) {
			return
		}
		if len(pendingWS) < 50 {
			pendingWS = append(pendingWS, string(b))
		}
		return
	}
	ws.conn.Call("send", string(b))
}

// flushPendingWS — onopen'da offline navbatni bo'shatadi.
func flushPendingWS() {
	if len(pendingWS) == 0 || !wsReady() {
		return
	}
	queued := pendingWS
	pendingWS = nil
	for _, raw := range queued {
		ws.conn.Call("send", raw)
	}
}

func isTypingPayload(payload any) bool {
	if m, ok := payload.(map[string]any); ok {
		if t, ok := m["type"].(string); ok && t == "typing" {
			return true
		}
	}
	return false
}

// wsReady — WS ulanish ochiq (readyState == OPEN) ekanini tekshiradi.
func wsReady() bool {
	if ws == nil || ws.closed || ws.conn.IsNull() || ws.conn.IsUndefined() {
		return false
	}
	return ws.conn.Get("readyState").Int() == 1
}

// accessTokenExpired — JWT access token exp (soniya) hozirga yaqin/otib ketganini
// aniqlaydi. Token buzilgan bo'lsa ham true (refresh'ga urinish zarar qilmaydi).
func accessTokenExpired() bool {
	tok := store.AccessToken
	if tok == "" {
		return true
	}
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return true
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return true
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return true
	}
	return claims.Exp <= time.Now().Unix()+30
}

func handleWSMessage(raw string) {
	var m ServerMsg
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return
	}
	switch m.Type {
	case "ready":
		store.Users = make(map[string]*OnlineUser)
		store.ByUser = make(map[string]string)
		for _, u := range m.Users {
			store.Users[u.ID] = u
			store.ByUser[u.Username] = u.ID
		}
		// Server fon katalogini ham yuboradi — BgList bo'lsa, tanlangan
		// fon qayta yuklashda ham to'g'ri ko'rsatiladi (url topiladi).
		if len(m.Backgrounds) > 0 {
			store.BgList = m.Backgrounds
		}
		if m.Background != "" {
			store.BgCurrent = m.Background
		}
		applyChatBackground()
		renderOnline()
	case "msg":
		onMessage(m.Message)
		maybeNotifyNewMessage(m.Message)
	case "history":
		onHistory(m.Messages)
	case "online":
		for _, u := range m.Users {
			store.Users[u.ID] = u
			store.ByUser[u.Username] = u.ID
		}
		renderOnline()
	case "presence":
		// Server batched yuboradi (Users [] array) — massiv ulanishda bir
		// nechta alohida xabar o'rniga bitta guruhli xabar.
		if len(m.Users) > 0 {
			for _, u := range m.Users {
				store.Users[u.ID] = u
				store.ByUser[u.Username] = u.ID
			}
		} else if m.User != nil {
			store.Users[m.User.ID] = m.User
			store.ByUser[m.User.Username] = m.User.ID
		} else if m.UserID != "" {
			if u, ok := store.Users[m.UserID]; ok {
				u.Online = m.Online
				u.Devices = m.Devices
			}
		}
		renderOnline()
	case "typing":
		onTyping(m)
	case "bg_list":
		store.BgList = m.Backgrounds
	case "bg_set":
		if m.Background != "" {
			store.BgCurrent = m.Background
		}
		applyChatBackground()
	case "notif":
		if m.Notify != nil {
			// DM/reply bildirishnomasi: xonani ochiq bo'lsa toast/notif
			// takrorlanmaydi (xabarning o'zi ekranda).
			if m.Notify.Room != "" && m.Notify.Room == store.CurRoom {
				return
			}
			showToast(m.Notify)
			// Mention bildirishnomasi — kechikishsiz native push.
			showBrowserNotif(m.Notify.Title, m.Notify.Body)
		}
	case "deleted":
		onMessageDeleted(m)
	case "ack":
		// Haqiqiy xabar ID'si serverda saqlangach keladi (async insert).
		onMessageAck(m)
	case "force_logout", "kick":
		forceLogin()
	case "error":
		if m.Code == "duplicate" {
			return
		}
		showToast(&Notif{Type: "error", Title: "Xatolik", Body: translateWSError(m.Error)})
	case "ok":
		// duplicate/ack — hech narsa qilmaymiz
	}
}

// translateWSError — serverning inglizcha xato xabarlarini o'zbekchaga
// o'tkazadi (tanish xatolar); qolganlari asl holicha ko'rsatiladi.
func translateWSError(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "you are blocked":
		return "Sizni bloklashgan — bu xonaga xabar yoza olmaysiz"
	case "message not found":
		return "Xabar topilmadi"
	case "you can only delete your own messages":
		return "Faqat o'z xabarlaringizni o'chira olasiz"
	case "too many messages":
		return "Juda tez yuboryapsiz, biroz kuting"
	}
	return s
}

func closeWS() {
	if ws != nil && !ws.closed {
		ws.closed = true
		ws.release()
		ws.conn.Call("close")
	}
}
