package model

import "testing"

func TestPublicID(t *testing.T) {
	u := User{ID: "uuid-123", Username: "tohirbek"}
	if u.PublicID() != "tohirbek" {
		t.Errorf("PublicID = %q, want %q", u.PublicID(), "tohirbek")
	}
	u.HideUsername = true
	if u.PublicID() != "uuid-123" {
		t.Errorf("hidden PublicID = %q, want uuid", u.PublicID())
	}
}

func TestMessageDefaults(t *testing.T) {
	m := Message{}
	if m.Type != "" {
		t.Errorf("default type should be empty")
	}
}
