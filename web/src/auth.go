//go:build js && wasm

package main

import (
	"fmt"
	"regexp"
	"strings"
	"syscall/js"
)

// Backend bilan mos username/parol qoidalari (web/src/auth.go va
// internal/security/validator.go o'rtasida sinxron).
var (
	reUsername      = regexp.MustCompile(`^[a-zA-Z0-9_.]+$`)
	rePasswordCheck = regexp.MustCompile(`[a-zA-Z].*[0-9]|[0-9].*[a-zA-Z]`)
)

// Auth ekranlari: login, register, 2FA.

func renderAuth(app js.Value) {
	HTML(app, authLayout(false, ""))
	wireAuth(app)
}

func renderRegister(app js.Value) {
	HTML(app, authLayout(true, ""))
	wireRegister(app)
}

var _ = js.Undefined

func renderTwoFA(app js.Value, pending string) {
	HTML(app, authLayout(false, "twofa"))
	store.Pending = pending
	btn := ByID("auth-submit")
	codeIn := ByID("auth-code")
	submit := func() { doTwoFA(GetVal(codeIn)) }
	On(btn, "click", func(_ js.Value, _ []js.Value) { submit() })
	On(codeIn, "keydown", enterKey(submit))
	On(ByID("auth-switch"), "click", func(_ js.Value, _ []js.Value) {
		renderAuth(app)
	})
	codeIn.Call("focus")
}

func authLayout(reg bool, mode string) string {
	title := "Kirish"
	subtitle := "KRADCHAT dunyosiga xush kelibsiz"
	btnText := "Kirish"
	sw := `Akkount yo'qmi? <a id="auth-switch" href="#">Ro'yxatdan o'tish</a>`
	fields := `<div class="field">
        <label for="auth-username">Foydalanuvchi nomi</label>
        <input id="auth-username" type="text" autocomplete="username" placeholder="username" autocapitalize="none">
      </div>
      <div class="field" id="password-field">
        <label for="auth-password">Parol</label>
        <input id="auth-password" type="password" autocomplete="current-password" placeholder="••••••••">
      </div>`
	if reg {
		title = "Ro'yxatdan o'tish"
		subtitle = "Yangi hisob yarating"
		btnText = "Yaratish"
		sw = `Hisobingiz bormi? <a id="auth-switch" href="#">Kirish</a>`
	}
	if mode == "twofa" {
		title = "Two-Factor Authentication"
		subtitle = "Authenticator ilovasidan 6 xonali kodni kiriting"
		btnText = "Tasdiqlash"
		sw = `Orqaga? <a id="auth-switch" href="#">Kirish</a>`
		fields = `<div class="field">
        <label for="auth-code">6 xonali kod</label>
        <input id="auth-code" type="text" inputmode="numeric" autocomplete="one-time-code" maxlength="6" placeholder="••••••">
      </div>`
	}
	return `<div class="auth-wrap">
  <div class="auth-card glass">
    <img src="/static/logo.png" alt="KRADCHAT" class="auth-logo">
    <h1 class="auth-title" id="auth-title">` + title + `</h1>
    <p class="auth-subtitle" id="auth-subtitle">` + subtitle + `</p>
    <div class="auth-form">
      ` + fields + `
      <button class="btn-primary btn-block" id="auth-submit">` + btnText + `</button>
      <div class="auth-error" id="auth-error"></div>
      <p class="auth-switch">` + sw + `</p>
    </div>
  </div>
</div>`
}

func wireAuth(app js.Value) {
	submit := func() {
		u := GetVal(ByID("auth-username"))
		p := GetVal(ByID("auth-password"))
		doLogin(u, p)
	}
	On(ByID("auth-submit"), "click", func(_ js.Value, _ []js.Value) { submit() })
	On(ByID("auth-username"), "keydown", enterKey(submit))
	On(ByID("auth-password"), "keydown", enterKey(submit))
	On(ByID("auth-switch"), "click", func(_ js.Value, _ []js.Value) {
		renderRegister(app)
	})
	ByID("auth-username").Call("focus")
}

func wireRegister(app js.Value) {
	submit := func() {
		u := GetVal(ByID("auth-username"))
		p := GetVal(ByID("auth-password"))
		doRegister(u, p)
	}
	On(ByID("auth-submit"), "click", func(_ js.Value, _ []js.Value) { submit() })
	On(ByID("auth-username"), "keydown", enterKey(submit))
	On(ByID("auth-password"), "keydown", enterKey(submit))
	On(ByID("auth-switch"), "click", func(_ js.Value, _ []js.Value) {
		renderAuth(app)
	})
	ByID("auth-username").Call("focus")
}

func enterKey(fn func()) func(js.Value, []js.Value) {
	return func(_ js.Value, args []js.Value) {
		if len(args) > 0 && args[0].Get("key").String() == "Enter" {
			fn()
		}
	}
}

func authError(msg string) {
	el := ByID("auth-error")
	if el.IsNull() || el.IsUndefined() {
		return
	}
	SetText(el, msg)
}

// runAsync — goroutine'ni panic xatosini ekranda ko'rsatish bilan ishga
// tushiradi. JS'da Go panic'i hech qanday xabar bermay o'chadi — shuning
// uchun aynan qaysi qadamda buzilgani ko'rinmay qolardi.
func runAsync(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				authError("Xatolik: " + fmt.Sprint(r))
			}
		}()
		fn()
	}()
}

func doLogin(username, password string) {
	authError("")
	if username == "" || password == "" {
		authError("Username va parolni kiriting")
		return
	}
	btn := ByID("auth-submit")
	btn.Set("disabled", true)
	runAsync(func() {
		var out struct {
			Status         string `json:"status"`
			PendingSession string `json:"pending_session"`
			Auth           *struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				User         *User  `json:"user"`
			} `json:"auth"`
		}
		resp, err := fetchJSON("POST", "/api/auth/login", map[string]any{
			"username": username,
			"password": password,
		}, false)
		if err != nil {
			btn.Set("disabled", false)
			authError(err.Error())
			return
		}
		if err := unmarshalResponse(resp, &out); err != nil {
			btn.Set("disabled", false)
			authError("Server javobi noto'g'ri")
			return
		}
		btn.Set("disabled", false)
		switch out.Status {
		case "2fa_required":
			renderTwoFA(ByID("app"), out.PendingSession)
		case "ok":
			if out.Auth != nil {
				saveSession(*out.Auth)
				connectWS()
				renderChat(ByID("app"))
				if store.User != nil && store.User.NotificationsEnabled {
					requestNotifPermission()
				}
			}
		default:
			authError("Noma'lum javob")
		}
	})
}

func doTwoFA(code string) {
	authError("")
	if len(code) < 6 {
		authError("6 xonali kod kiriting")
		return
	}
	btn := ByID("auth-submit")
	btn.Set("disabled", true)
	runAsync(func() {
		var out struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			User         *User  `json:"user"`
		}
		resp, err := fetchJSON("POST", "/api/auth/twofa/verify", map[string]any{
			"pending_session": store.Pending,
			"code":            code,
		}, false)
		if err != nil {
			btn.Set("disabled", false)
			authError(err.Error())
			return
		}
		if err := unmarshalResponse(resp, &out); err != nil {
			btn.Set("disabled", false)
			authError("Server javobi noto'g'ri")
			return
		}
		btn.Set("disabled", false)
		saveSession(out)
		store.Pending = ""
		connectWS()
		renderChat(ByID("app"))
		if store.User != nil && store.User.NotificationsEnabled {
			requestNotifPermission()
		}
	})
}

func doRegister(username, password string) {
	authError("")
	if username == "" || password == "" {
		authError("Username va parolni kiriting")
		return
	}
	username = strings.ToLower(strings.TrimSpace(username))
	if len([]rune(username)) < 3 {
		authError("Username kamida 3 belgi bo'lsin")
		return
	}
	if !reUsername.MatchString(username) {
		authError("Username faqat harf, raqam, _ va . bo'lishi mumkin")
		return
	}
	if username[0] == '.' || username[0] == '_' ||
		username[len(username)-1] == '.' || username[len(username)-1] == '_' ||
		strings.Contains(username, "..") || strings.Contains(username, "__") {
		authError("Username boshida/oxirida nuqta yoki _ bo'lmasin")
		return
	}
	if len(password) < 8 {
		authError("Parol kamida 8 belgidan iborat bo'lsin")
		return
	}
	if !rePasswordCheck.MatchString(password) {
		authError("Parolda harf va raqam bo'lsin")
		return
	}
	btn := ByID("auth-submit")
	btn.Set("disabled", true)
	runAsync(func() {
		var out struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			User         *User  `json:"user"`
		}
		resp, err := fetchJSON("POST", "/api/auth/register", map[string]any{
			"username":     username,
			"password":     password,
			"display_name": username,
		}, false)
		if err != nil {
			btn.Set("disabled", false)
			authError(err.Error())
			return
		}
		if err := unmarshalResponse(resp, &out); err != nil {
			btn.Set("disabled", false)
			authError("Server javobi noto'g'ri")
			return
		}
		btn.Set("disabled", false)
		saveSession(out)
		connectWS()
		renderChat(ByID("app"))
		if store.User != nil && store.User.NotificationsEnabled {
			requestNotifPermission()
		}
	})
}
