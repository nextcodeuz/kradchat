package security

import (
	"html"
	"regexp"
	"strings"
)

var (
	scriptTagRe  = regexp.MustCompile(`(?is)<script[\s\S]*?</script>`)
	styleTagRe   = regexp.MustCompile(`(?is)<style[\s\S]*?</style>`)
	onEventRe    = regexp.MustCompile(`(?is)\son\w+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	javascriptRe = regexp.MustCompile(`(?i)\b(javascript|vbscript|data):`)
	srcRefRe     = regexp.MustCompile(`(?i)\b(src|href)\s*=\s*("(?:javascript|vbscript|data):[^"]*"|'(?:javascript|vbscript|data):[^']*')`)
)

// allowedTags — xabarlarda ruxsat etilgan minimal taglar.
var allowedTags = map[string]bool{
	"b": true, "i": true, "em": true, "strong": true, "u": true,
	"br": true, "p": true, "span": true,
}

// Clean XSS xavfini yo'qotadi: script/style/on* atributlari va JS uri
// olib tashlanadi; qolgan HTML allowlist bo'yicha filtrlanadi.
// Kod bloklari (```...```) alohida saqlanadi va sanitizer orqali
// HTML eskap qilinadi (xavfsiz render uchun).
func Clean(input string) string {
	if input == "" {
		return ""
	}

	// 1. Kod bloklarini vaqtinchalik token bilan himoyalash.
	var codeBlocks []codeBlock
	input = extractCodeBlocks(input, &codeBlocks)

	// 2. Xavfli narsalarni tozalash.
	s := scriptTagRe.ReplaceAllString(input, "")
	s = styleTagRe.ReplaceAllString(s, "")
	s = onEventRe.ReplaceAllString(s, "")
	s = javascriptRe.ReplaceAllString(s, "")
	s = srcRefRe.ReplaceAllString(s, "")

	// 3. Allowlist filtridan o'tkazish.
	s = filterAllowedTags(s)

	// 4. Kod bloklarini qaytarish (HTML eskap qilingan).
	s = restoreCodeBlocks(s, codeBlocks)

	// 5. Kiril boshqaruv belgilari (virus qo'yish urinishlari).
	s = strings.Map(func(r rune) rune {
		if r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' || r == '\x00' {
			return -1
		}
		return r
	}, s)

	return strings.TrimSpace(s)
}

// CleanText to'liq HTML dan tozalangan oddiy matn qaytaradi (hech qanday tag).
func CleanText(input string) string {
	s := Clean(input)
	s = tagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(html.UnescapeString(s))
}

var tagRe = regexp.MustCompile(`(?s)<[^>]*>`)

// EscapeHTMLString — matnni HTML eskap qiladi (code bloklari va emoji uchun).
func EscapeHTMLString(s string) string {
	return html.EscapeString(s)
}

// filterAllowedTags har bir tagni allowlist bo'yicha tekshiradi; boshqa
// taglar o'chiriladi, atributlar esa yechiladi (faqat tag nomi qoladi).
func filterAllowedTags(s string) string {
	var sb strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '<' {
			j := strings.IndexByte(s[i:], '>')
			if j < 0 {
				sb.WriteString(html.EscapeString(s[i:]))
				break
			}
			tagText := s[i : i+j+1]
			tagName := extractTagName(tagText)
			if tagName != "" && allowedTags[tagName] {
				// Foydalanuvchi kiritgan ko'rinishni saqlaymiz: ochuvchi yoki
				// yopuvchi tag bo'lishi (slash) o'zgarishsiz qoladi — client
				// matn sifatida ko'rsatadi, shuning uchun slash yo'qolmasligi kerak.
				prefix := "<"
				if tr := strings.TrimSpace(tagText); strings.HasPrefix(tr, "</") {
					prefix = "</"
				}
				sb.WriteString(prefix)
				sb.WriteString(tagName)
				sb.WriteString(">")
			} else {
				// Ruxsat etilmagan tag — xavfsiz ko'rinishda chiqarish.
				sb.WriteString(html.EscapeString(tagText))
			}
			i += j + 1
		} else {
			sb.WriteByte(s[i])
			i++
		}
	}
	return sb.String()
}

func extractTagName(tag string) string {
	t := strings.Trim(tag, "<> ")
	if strings.HasPrefix(t, "/") {
		return strings.ToLower(strings.TrimSpace(t[1:]))
	}
	idx := strings.IndexAny(t, " \t\n/")
	if idx > 0 {
		t = t[:idx]
	}
	return strings.ToLower(t)
}

// codeBlock — chiqarib olingan kod bloki: til nomi va tarkib.
type codeBlock struct {
	lang string
	code string
}

func extractCodeBlocks(s string, blocks *[]codeBlock) string {
	var sb strings.Builder
	rest := s
	for {
		start := strings.Index(rest, "```")
		if start < 0 {
			sb.WriteString(rest)
			break
		}
		// Kod bloki faqat qator boshida ochiladi (matn ichidagi ``` emas).
		if start > 0 && rest[start-1] != '\n' {
			sb.WriteString(rest[:start+1])
			rest = rest[start+1:]
			continue
		}
		// Fence qatori: "```lang" — qolgan qismi til nomi sifatida olinadi.
		nl := strings.IndexByte(rest[start:], '\n')
		if nl < 0 {
			sb.WriteString(rest[start:])
			break
		}
		fenceLine := rest[start : start+nl]
		bodyStart := start + nl + 1
		closeAt := findClosingFence(rest, bodyStart)
		if closeAt < 0 {
			sb.WriteString(rest[start:])
			break
		}
		lang := strings.TrimSpace(fenceLine[3:])
		code := strings.TrimSuffix(rest[bodyStart:closeAt], "\n")
		*blocks = append(*blocks, codeBlock{lang: lang, code: code})
		sb.WriteString("$KRADCHAT_CODE$")
		sb.WriteString(intToToken(len(*blocks) - 1))
		rest = rest[closeAt+3:]
	}
	return sb.String()
}

// findClosingFence — from'dan boshlab qator boshidagi yopuvchi ``` ni topadi.
func findClosingFence(s string, from int) int {
	i := from
	for i+3 <= len(s) {
		j := strings.Index(s[i:], "```")
		if j < 0 {
			return -1
		}
		at := i + j
		if at == 0 || s[at-1] == '\n' {
			return at
		}
		i = at + 1
	}
	return -1
}

func restoreCodeBlocks(s string, blocks []codeBlock) string {
	for i, b := range blocks {
		attr := ""
		if b.lang != "" {
			attr = ` data-lang="` + html.EscapeString(b.lang) + `"`
		}
		s = strings.ReplaceAll(s, "$KRADCHAT_CODE$"+intToToken(i), "<pre><code"+attr+">"+html.EscapeString(b.code)+"</code></pre>")
	}
	return s
}

func intToToken(i int) string {
	if i == 0 {
		return "0"
	}
	var sb strings.Builder
	for i > 0 {
		sb.WriteByte(byte('0' + i%10))
		i /= 10
	}
	// reverse
	b := []byte(sb.String())
	for x, y := 0, len(b)-1; x < y; x, y = x+1, y-1 {
		b[x], b[y] = b[y], b[x]
	}
	return string(b)
}
