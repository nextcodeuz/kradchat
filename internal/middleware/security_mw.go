package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// SecurityHeaders — xavfsizlik headerlari.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		// CSP — o'z domendan + Google Fonts + data/blob media (emoji, upload, fonlar).
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"img-src 'self' data: blob:; media-src 'self' blob:; "+
				"font-src 'self' data: https://fonts.gstatic.com; "+
				"connect-src 'self' ws: wss: https://fonts.googleapis.com; "+
				"object-src 'none'; base-uri 'self'; form-action 'self'; "+
				"frame-ancestors 'none';")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		next.ServeHTTP(w, r)
	})
}

// AllowedOrigin — ruxsat etilgan Origin tekshiruvi.
// Statik ro'yxat (localhost) dan tashqari, so'rov kelgan Host bilan bir xil
// origin ham ruxsat etiladi — shu tariqa telefon/LAN IP orqali kirishda
// ham CORS ishlaydi, boshqa saytlardan kelgan so'rovlar esa bloklanadi.
func AllowedOrigin(origins []string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // no browser (curl, ws server-side)
		}
		for _, o := range origins {
			if strings.EqualFold(o, origin) {
				return true
			}
		}
		return originMatchesHost(origin, r.Host)
	}
}

// originMatchesHost — "http(s)://host:port" shaklidagi origin so'rov
// Host'iga mos keladimi. r.Host tarkibida scheme bo'lmaydi, shuning
// uchun u.Host bilan solishtiramiz. Faqat http/https scheme ruxsat —
// brauzerlar boshqa scheme'da Origin yubormaydi (g'alati scheme'lar rad).
func originMatchesHost(origin, host string) bool {
	if host == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

// CORS — API uchun.
func CORS(allowedOrigin func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !allowedOrigin(r) {
				WriteError(w, http.StatusForbidden, "forbidden", "origin not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
