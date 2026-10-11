package middleware

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

type ctxKey string

const (
	CtxUserID   ctxKey = "user_id"
	CtxUsername ctxKey = "username"
	CtxDeviceID ctxKey = "device_id"
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, code, detail string) {
	WriteJSON(w, status, map[string]any{
		"error":  http.StatusText(status),
		"code":   code,
		"detail": detail,
	})
}

func GetUserID(r *http.Request) string {
	v, _ := r.Context().Value(CtxUserID).(string)
	return v
}

func GetUsername(r *http.Request) string {
	v, _ := r.Context().Value(CtxUsername).(string)
	return v
}

func GetDeviceID(r *http.Request) string {
	v, _ := r.Context().Value(CtxDeviceID).(string)
	return v
}

// Logging access log yozadi.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "err", rec, "stack", string(debug.Stack()))
				WriteError(sw, http.StatusInternalServerError, "internal", "internal error")
			}
			slog.Info("http",
				"method", r.Method, "path", r.URL.Path, "status", sw.status,
				"dur", time.Since(start).Round(time.Microsecond), "ip", ClientIP(r))
		}()
		next.ServeHTTP(sw, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack — WebSocket upgrade uchun kerak. Bazaviy ResponseWriter hijack qila
// olsagina uzatiladi (masalan gin/mux recorder'larida yo'q bo'lishi mumkin).
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hj.Hijack()
}

// Flush — Server-Sent Events va stream'li javoblar uchun.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ReaderFrom — io.Copy optimizatsiyasi (statik fayllar tezroq).
func (w *statusWriter) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(w.ResponseWriter, r)
}

// ClientIP — request kelgan haqiqiy IP.
// Faqat TRUSTED_PROXIES da ko'rsatilgan proksilardan kelgan so'rovlarda
// X-Forwarded-For ishoniladi; aks holda RemoteAddr — XFF spoofing orqali
// rate-limit va audit log'larni chetlab o'tish bloklanadi.
func ClientIP(r *http.Request) string {
	remote := hostOnly(r.RemoteAddr)
	if len(trustedProxyNets) == 0 {
		return remote
	}
	ip := net.ParseIP(remote)
	if ip == nil || !isTrustedProxy(ip) {
		return remote
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := splitComma(xff)
		if parts[0] != "" {
			return hostOnly(parts[0])
		}
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return hostOnly(xr)
	}
	return remote
}

var trustedProxyNets []*net.IPNet

// SetTrustedProxies — TRUSTED_PROXIES dan CIDR ro'yxatini yuklaydi
// (masalan "10.0.0.0/8,172.16.0.0/12"). Bo'sh bo'lsa XFF ishonilmaydi.
func SetTrustedProxies(s string) {
	trustedProxyNets = trustedProxyNets[:0]
	for _, part := range splitComma(s) {
		part = trimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			part += "/32"
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			slog.Warn("invalid trusted proxy CIDR", "cidr", part)
			continue
		}
		trustedProxyNets = append(trustedProxyNets, n)
	}
}

func isTrustedProxy(ip net.IP) bool {
	for _, n := range trustedProxyNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// hostOnly — "127.0.0.1:8080" dan "127.0.0.1" ni oladi.
func hostOnly(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func splitComma(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, trimSpace(s[start:i]))
			start = i + 1
		}
	}
	out = append(out, trimSpace(s[start:]))
	return out
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
