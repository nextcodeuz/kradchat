//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"syscall/js"
)

// fetch wrapper — JS Promise'ni Go kanaliga o'tkazadi.
func awaitJS(promise js.Value) (js.Value, error) {
	done := make(chan js.Value, 1)
	fail := make(chan js.Value, 1)
	var okFn, errFn js.Func
	okFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		done <- args[0]
		okFn.Release()
		errFn.Release()
		return nil
	})
	errFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		fail <- args[0]
		okFn.Release()
		errFn.Release()
		return nil
	})
	promise.Call("then", okFn).Call("catch", errFn)
	select {
	case v := <-done:
		return v, nil
	case e := <-fail:
		return js.Value{}, fmt.Errorf("%s", e.Call("toString").String())
	}
}

// unmarshalResponse — fetch response'ni matn sifatida o'qib, Go structga decode qiladi.
// Eski yo'l: resp.json() + JSON.stringify round-trip'i JS muhitida
// "JSON.stringify is not a function" panic'iga olib kelishi mumkin edi.
// Endi JSON parse butunlay Go tomonida bajariladi — JS JSON'ga bog'liq emas.
func unmarshalResponse(resp js.Value, out any) error {
	text, err := awaitJS(resp.Call("text"))
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(text.String()), out)
}

type apiError struct {
	Message string
	Status  int
}

func (e *apiError) Error() string { return e.Message }

// fetchJSON — REST API chaqiruv. 401 bo'lsa refresh va qayta urinish.
func fetchJSON(method, path string, payload any, authed bool) (js.Value, error) {
	resp, err := doFetch(method, path, payload, authed, "")
	if err != nil {
		return js.Value{}, err
	}
	if resp.Get("status").Int() == 401 && authed && store.RefreshToken != "" {
		if tryRefresh() {
			return doFetch(method, path, payload, true, "")
		}
	}
	if !resp.Get("ok").Bool() {
		msg := resp.Get("status").String()
		if t, err := awaitJS(resp.Call("text")); err == nil {
			var er struct {
				Error  string `json:"error"`
				Code   string `json:"code"`
				Detail string `json:"detail"`
			}
			if json.Unmarshal([]byte(t.String()), &er) == nil && er.Error != "" {
				msg = er.Error
			}
		}
		return js.Value{}, &apiError{Message: msg, Status: resp.Get("status").Int()}
	}
	return resp, nil
}

func doFetch(method, path string, payload any, authed bool, raw string) (js.Value, error) {
	opts := js.Global().Get("Object").New()
	opts.Set("method", method)
	headers := js.Global().Get("Object").New()
	headers.Set("Accept", "application/json")
	if authed && store.AccessToken != "" {
		headers.Set("Authorization", "Bearer "+store.AccessToken)
	}
	var body string
	switch {
	case raw != "":
		body = raw
		headers.Set("Content-Type", "application/json")
	case payload != nil:
		b, err := json.Marshal(payload)
		if err != nil {
			return js.Value{}, err
		}
		body = string(b)
		headers.Set("Content-Type", "application/json")
	}
	opts.Set("headers", headers)
	if body != "" {
		opts.Set("body", body)
	}
	p := js.Global().Call("fetch", path, opts)
	return awaitJS(p)
}

// fetchJSONInto — javobni out'ga decode qiladi.
func fetchJSONInto(method, path string, payload any, out any) error {
	resp, err := fetchJSON(method, path, payload, true)
	if err != nil {
		return err
	}
	return unmarshalResponse(resp, out)
}

// tryRefreshMu — bir vaqtda ikki refresh poygasini seriyalashtiradi
// (boot + WS reconnect bir xil tokenni parallel ishlatmasligi uchun).
var tryRefreshMu sync.Mutex

// tryRefresh — refresh token bilan yangi juftlik olish.
func tryRefresh() bool {
	tryRefreshMu.Lock()
	defer tryRefreshMu.Unlock()
	if tryRefreshOnce() {
		return true
	}
	// Rotation raqobati: boshqa tab refresh'ni aylantirib yuborgan bo'lishi mumkin —
	// localStorage'dagi so'nggi token'ni o'qib, bir marta ko'proq urinamiz.
	loadSession()
	if store.RefreshToken != "" {
		if tryRefreshOnce() {
			return true
		}
	}
	forceLogin()
	return false
}

func tryRefreshOnce() bool {
	resp, err := doFetch("POST", "/api/auth/refresh", map[string]any{"refresh_token": store.RefreshToken}, false, "")
	if err != nil || !resp.Get("ok").Bool() {
		return false
	}
	var ar struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		User         *User  `json:"user"`
	}
	if err := unmarshalResponse(resp, &ar); err != nil {
		return false
	}
	store.AccessToken = ar.AccessToken
	if ar.RefreshToken != "" {
		store.RefreshToken = ar.RefreshToken
	}
	if ar.User != nil {
		store.User = ar.User
	}
	persistSession()
	return true
}

// saveSession — login/2fa muvaffaqiyatida.
func saveSession(ar struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         *User  `json:"user"`
}) {
	store.AccessToken = ar.AccessToken
	store.RefreshToken = ar.RefreshToken
	store.User = ar.User
	persistSession()
}

// persistSession — tokenlarni localStorage'ga saqlaydi (refresh'da ham ishlaydi).
func persistSession() {
	ls := js.Global().Get("localStorage")
	ls.Call("setItem", "krad_access", store.AccessToken)
	ls.Call("setItem", "krad_refresh", store.RefreshToken)
}

// loadSession — localStorage'dan sessiyani tiklaydi.
// getItem yo'q kalitda JS null qaytaradi — Go'da "null" string bo'lib qolmasligi
// uchun tekshiriladi (avval "null" token deb o'ylab ortiqcha /api/me so'rar edi).
func loadSession() {
	ls := js.Global().Get("localStorage")
	store.AccessToken = storageString(ls, "krad_access")
	store.RefreshToken = storageString(ls, "krad_refresh")
}

func storageString(ls js.Value, key string) string {
	v := ls.Call("getItem", key)
	if v.IsNull() || v.IsUndefined() {
		return ""
	}
	s := v.String()
	if s == "" || s == "null" || s == "undefined" {
		return ""
	}
	return s
}

// clearSession — localStorage'dan sessiyani o'chiradi.
func clearSession() {
	ls := js.Global().Get("localStorage")
	ls.Call("removeItem", "krad_access")
	ls.Call("removeItem", "krad_refresh")
}
