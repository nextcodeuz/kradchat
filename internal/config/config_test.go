package config

import (
	"strings"
	"testing"
)

func TestLoadRejectsDefaultJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "kradchat-super-secret-change-me")
	if _, err := Load(""); err == nil {
		t.Error("default JWT_SECRET should be rejected")
	}
}

func TestLoadRejectsShortJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "short")
	if _, err := Load(""); err == nil {
		t.Error("short JWT_SECRET should be rejected")
	}
}

func TestLoadRejectsBadBcryptCost(t *testing.T) {
	t.Setenv("JWT_SECRET", "a-secure-random-secret-123")
	for _, cost := range []string{"1", "16", "-5"} {
		t.Setenv("BCRYPT_COST", cost)
		if _, err := Load(""); err == nil {
			t.Errorf("BCRYPT_COST=%s should be rejected", cost)
		}
	}
}

func TestLoadAcceptsValidConfig(t *testing.T) {
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("BCRYPT_COST", "12")
	t.Setenv("DB_DRIVER", "postgres")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JWT.Secret != "0123456789abcdef0123456789abcdef" {
		t.Errorf("secret mismatch")
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("postgres default port = %d, want 5432", cfg.Database.Port)
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8,192.168.1.5")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(cfg.Security.TrustedProxies, "10.0.0.0/8") {
		t.Errorf("TrustedProxies not loaded: %q", cfg.Security.TrustedProxies)
	}
}
