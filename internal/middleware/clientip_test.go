package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPWithoutTrustedProxy(t *testing.T) {
	// TRUSTED_PROXIES bo'lmasa — XFF ishonilmaydi, RemoteAddr qaytariladi.
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.RemoteAddr = "203.0.113.7:54321"
	r.Header.Set("X-Forwarded-For", "6.6.6.6")
	if got := ClientIP(r); got != "203.0.113.7" {
		t.Errorf("ClientIP = %q, want %q", got, "203.0.113.7")
	}
}

func TestClientIPFromTrustedProxy(t *testing.T) {
	SetTrustedProxies("10.0.0.0/8,172.16.0.0/12")
	defer SetTrustedProxies("")

	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.RemoteAddr = "10.1.2.3:443"
	r.Header.Set("X-Forwarded-For", "198.51.100.9, 10.1.2.3")
	if got := ClientIP(r); got != "198.51.100.9" {
		t.Errorf("ClientIP = %q, want %q", got, "198.51.100.9")
	}
}

func TestClientIPFromUntrustedHostSpoofs(t *testing.T) {
	SetTrustedProxies("10.0.0.0/8")
	defer SetTrustedProxies("")

	// Ommaviy IP dan kelgan — XFF'dagi ixtiyoriy qiymat ishonilmaydi.
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.RemoteAddr = "203.0.113.7:443"
	r.Header.Set("X-Forwarded-For", "6.6.6.6")
	if got := ClientIP(r); got != "203.0.113.7" {
		t.Errorf("ClientIP = %q, want %q", got, "203.0.113.7")
	}
}

func TestSetTrustedProxiesIgnoresBadCIDR(t *testing.T) {
	SetTrustedProxies("not-an-ip,10.0.0.0/8,,/99")
	defer SetTrustedProxies("")
	if len(trustedProxyNets) != 1 {
		t.Errorf("trustedProxyNets len = %d, want 1", len(trustedProxyNets))
	}
}
