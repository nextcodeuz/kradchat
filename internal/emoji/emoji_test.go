package emoji

import (
	"strings"
	"testing"
)

func TestListNotEmpty(t *testing.T) {
	if len(List) < 500 {
		t.Fatalf("emoji list too small: %d", len(List))
	}
	if len(Categories) < 8 {
		t.Fatalf("expected categories, got %d", len(Categories))
	}
}

func TestSearch(t *testing.T) {
	res := Search("fire", 10)
	if len(res) == 0 {
		t.Fatal("search 'fire' should find something")
	}
	for _, e := range res {
		if !strings.Contains(strings.ToLower(e.Name), "fire") &&
			!strings.Contains(strings.ToLower(strings.Join(e.Keywords, " ")), "fire") {
			t.Errorf("result %q not matching 'fire'", e.Name)
		}
	}
}

func TestByCategory(t *testing.T) {
	smileys := ByCategory("Smileys")
	if len(smileys) == 0 {
		t.Fatal("Smileys category empty")
	}
	for _, e := range smileys {
		if e.Category != "Smileys" {
			t.Errorf("wrong category %q", e.Category)
		}
	}
}

func TestShortcodes(t *testing.T) {
	if len(Shortcodes) == 0 {
		t.Fatal("shortcodes empty")
	}
	if em, ok := Shortcodes["grinning"]; !ok || em == "" {
		t.Error("expected :grinning: shortcode")
	}
}

func TestReplaceShortcodes(t *testing.T) {
	in := "Salom :grinning: dunyo"
	out := ReplaceShortcodes(in)
	if out == in {
		t.Error("shortcode not replaced")
	}
	if !strings.Contains(out, "😀") {
		t.Errorf("expected grinning emoji in output: %q", out)
	}
	unknown := ReplaceShortcodes(":not_a_real_code:")
	if unknown != ":not_a_real_code:" {
		t.Errorf("unknown shortcode should stay: %q", unknown)
	}
}

func TestUniqueEmoji(t *testing.T) {
	seen := make(map[string]bool)
	for _, e := range List {
		key := e.Emoji + "|" + e.Name
		if seen[key] {
			t.Errorf("duplicate entry %q (%s)", e.Emoji, e.Name)
		}
		seen[key] = true
	}
}
