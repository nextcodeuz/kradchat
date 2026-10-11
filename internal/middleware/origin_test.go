package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestAllowedOrigin(t *testing.T) {
	check := AllowedOrigin([]string{"http://localhost:8080", "http://127.0.0.1:8080"})

	cases := []struct {
		name    string
		origin  string
		host    string
		allowed bool
	}{
		{"no origin (curl)", "", "localhost:8080", true},
		{"static list", "http://localhost:8080", "localhost:8080", true},
		{"127.0.0.1 static", "http://127.0.0.1:8080", "localhost:8080", true},
		{"same host LAN", "http://192.168.1.50:8080", "192.168.1.50:8080", true},
		{"same host hostname", "http://myserver.lan:8080", "myserver.lan:8080", true},
		{"phone via LAN", "http://192.168.1.50:8080", "192.168.1.50:8080", true},
		{"different host", "http://evil.com:8080", "192.168.1.50:8080", false},
		{"different port", "http://192.168.1.50:9090", "192.168.1.50:8080", false},
		{"scheme difference ok", "https://192.168.1.50:8080", "192.168.1.50:8080", true},
		{"cross-site", "https://attacker.example", "localhost:8080", false},
		{"bad url", "::not-a-url::", "localhost:8080", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "http://"+c.host+"/api/auth/login", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := check(r); got != c.allowed {
			t.Errorf("%s: AllowedOrigin(origin=%q, host=%q) = %v, want %v",
				c.name, c.origin, c.host, got, c.allowed)
		}
	}
}

func TestOriginMatchesHost(t *testing.T) {
	if !originMatchesHost("http://10.0.0.5:8080", "10.0.0.5:8080") {
		t.Error("LAN origin should match host")
	}
	if originMatchesHost("http://10.0.0.5:8080", "10.0.0.5:9090") {
		t.Error("different port should not match")
	}
	if originMatchesHost("http://10.0.0.5:8080", "") {
		t.Error("empty host should not match")
	}
	if originMatchesHost("http://10.0.0.5:8080", "10.0.0.6:8080") {
		t.Error("different host should not match")
	}
}
