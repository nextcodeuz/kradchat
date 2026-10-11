package emoji

import "strings"

// Emoji — bitta emoji metadata'si.
type Emoji struct {
	Emoji    string   `json:"emoji"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Keywords []string `json:"keywords"`
}

// Categories — emoji kategoriyalari (tartibli).
var Categories = []string{
	"Smileys", "People", "Gestures", "Animals", "Food", "Travel",
	"Activities", "Objects", "Symbols", "Flags",
}

// Shortcodes — shortcode -> unicode emoji.
var Shortcodes = map[string]string{}

// ByEmoji — unicode emoji -> shortcode (eng mashhur variant).
var ByEmoji = map[string]string{}

// List — barcha emoji metadata.
var List []Emoji

// Search — query bo'yicha qidiradi (name/keywords/shortcode).
func Search(query string, limit int) []Emoji {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]Emoji, 0, len(List))
		out = append(out, List...)
		return out
	}
	var out []Emoji
	for _, e := range List {
		if matchEmoji(e, q) {
			out = append(out, e)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func matchEmoji(e Emoji, q string) bool {
	if strings.Contains(strings.ToLower(e.Name), q) {
		return true
	}
	for _, k := range e.Keywords {
		if strings.Contains(strings.ToLower(k), q) {
			return true
		}
	}
	return false
}

// ByCategory — kategoriya bo'yicha ro'yxat.
func ByCategory(cat string) []Emoji {
	var out []Emoji
	for _, e := range List {
		if e.Category == cat {
			out = append(out, e)
		}
	}
	return out
}

// ReplaceShortcodes matndagi :shortcode: ni Unicode emoji ga almashtiradi.
func ReplaceShortcodes(s string) string {
	if !strings.Contains(s, ":") {
		return s
	}
	var sb strings.Builder
	rest := s
	for {
		start := strings.IndexByte(rest, ':')
		if start < 0 {
			sb.WriteString(rest)
			break
		}
		sb.WriteString(rest[:start])
		end := strings.IndexByte(rest[start+1:], ':')
		if end < 0 {
			sb.WriteString(rest[start:])
			break
		}
		code := rest[start+1 : start+1+end]
		if em, ok := Shortcodes[strings.ToLower(code)]; ok {
			sb.WriteString(em)
		} else {
			sb.WriteString(rest[start : start+1+end+1])
		}
		rest = rest[start+1+end+1:]
	}
	return sb.String()
}
