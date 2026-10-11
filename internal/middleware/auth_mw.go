package middleware

import (
	"context"
	"net/http"
	"strings"

	"kradchat/internal/service"
)

// Auth JWT access token'ni tekshiradi.
func Auth(auth *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				WriteError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
				return
			}
			claims, err := auth.ValidateAccess(token)
			if err != nil {
				WriteError(w, http.StatusUnauthorized, "invalid_token", "invalid or expired token")
				return
			}
			ctx := r.Context()
			ctx = context.WithValue(ctx, CtxUserID, claims.UserID)
			ctx = context.WithValue(ctx, CtxUsername, claims.Username)
			ctx = context.WithValue(ctx, CtxDeviceID, claims.DeviceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	// Token faqat WebSocket uchun query parametrda ruxsat etiladi —
	// brauzer WS'ga header qo'ya olmaydi. Boshqa endpointlarda query
	// token ishlatilmaydi (loglarga tushishi va referrer'da qolishi oldi).
	if strings.HasPrefix(r.URL.Path, "/ws") {
		return r.URL.Query().Get("token")
	}
	return ""
}
