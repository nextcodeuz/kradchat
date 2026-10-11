package security

import (
	"strings"
	"testing"
)

func TestValidateUsername(t *testing.T) {
	valid := []string{"tohirbek", "Tohir.Bek_9", "abc"}
	invalid := []string{"ab", "tohir@bek", "tohir bek", "a😀b", strings.Repeat("x", 51)}
	for _, u := range valid {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("ValidateUsername(%q) = %v, want nil", u, err)
		}
	}
	for _, u := range invalid {
		if err := ValidateUsername(u); err == nil {
			t.Errorf("ValidateUsername(%q) = nil, want error", u)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	valid := []string{"password1", "SalomDunyo2024", "12345678a"}
	invalid := []string{"short1", "onlyletters", "12345678", "nolong" + strings.Repeat("x", 70)}
	for _, p := range valid {
		if err := ValidatePassword(p); err != nil {
			t.Errorf("ValidatePassword(%q) = %v, want nil", p, err)
		}
	}
	for _, p := range invalid {
		if err := ValidatePassword(p); err == nil {
			t.Errorf("ValidatePassword(%q) = nil, want error", p)
		}
	}
}

func TestValidateRoom(t *testing.T) {
	if !ValidateRoom("global") {
		t.Error("global should be valid")
	}
	if !ValidateRoom("room-1_test") {
		t.Error("room-1_test should be valid")
	}
	if ValidateRoom("") || ValidateRoom("bad room!") || ValidateRoom(strings.Repeat("a", 65)) {
		t.Error("invalid rooms rejected")
	}
}

func TestValidateContent(t *testing.T) {
	if err := ValidateContent("salom dunyo"); err != nil {
		t.Errorf("normal content: %v", err)
	}
	if err := ValidateContent(strings.Repeat("a", MaxContentLen)); err != nil {
		t.Errorf("max content: %v", err)
	}
	if err := ValidateContent(strings.Repeat("a", MaxContentLen+1)); err == nil {
		t.Error("too long content should fail")
	}
	if err := ValidateContent("a\x00b"); err == nil {
		t.Error("control char should fail")
	}
}

func TestDMRoomForAndValidate(t *testing.T) {
	a := "550e8400-e29b-41d4-a716-446655440000"
	b := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	room := DMRoomFor(a, b)
	if !strings.HasPrefix(room, "dm:") {
		t.Errorf("DMRoomFor prefix = %q, want dm:", room)
	}
	if !ValidateDMRoom(room) {
		t.Errorf("ValidateDMRoom(%q) = false, want true", room)
	}
	if room != DMRoomFor(b, a) {
		t.Errorf("DMRoomFor not canonical: %q vs %q", room, DMRoomFor(b, a))
	}
	if !IsDMRoomMember(room, a) || !IsDMRoomMember(room, b) {
		t.Errorf("membership check failed for %q", room)
	}
}

func TestValidateDMRoomRejects(t *testing.T) {
	bad := []string{
		"global",
		"dm:550e8400-e29b-41d4-a716-446655440000",     // bitta uuid
		"dm:550e8400-e29b-41d4-a716-446655440000:xyz", // ikkinchisi uuid emas
		"dm:550e8400-e29b-41d4-a716-446655440000::6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"DM:550e8400-e29b-41d4-a716-446655440000:6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"dm:550e8400-e29b-41d4-a716-446655440000:6ba7b810-9dad-11d1-80b4-00c04fd430c8:extra",
	}
	for _, r := range bad {
		if ValidateDMRoom(r) {
			t.Errorf("ValidateDMRoom(%q) = true, want false", r)
		}
	}
}

func TestIsDMRoomMemberRejectsStrangers(t *testing.T) {
	a := "550e8400-e29b-41d4-a716-446655440000"
	b := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	c := "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	room := DMRoomFor(a, b)
	if IsDMRoomMember(room, c) {
		t.Errorf("outsider user %q considered member of %q", c, room)
	}
	if IsDMRoomMember("global", a) {
		t.Errorf("non-DM room should never allow membership")
	}
	if !IsDMRoomMember(DMRoomFor(a, a), a) {
		t.Errorf("self-DM should allow member")
	}
}

func TestCleanRemovesScript(t *testing.T) {
	in := `<script>alert(1)</script>salom <b>dunyo</b>`
	out := Clean(in)
	if strings.Contains(out, "script") {
		t.Errorf("script not removed: %q", out)
	}
	if !strings.Contains(out, "salom") {
		t.Errorf("text lost: %q", out)
	}
	if !strings.Contains(out, "<b>") {
		t.Errorf("allowed tag lost: %q", out)
	}
}

func TestCleanRemovesEventHandlers(t *testing.T) {
	cases := []string{
		`<img src=x onerror=alert(1)>`,
		`<div onclick="steal()">x</div>`,
		`<a href="javascript:alert(1)">x</a>`,
	}
	for _, in := range cases {
		out := Clean(in)
		if strings.Contains(strings.ToLower(out), "onerror") ||
			strings.Contains(strings.ToLower(out), "onclick") ||
			strings.Contains(strings.ToLower(out), "javascript:") {
			t.Errorf("unsafe content survived: in=%q out=%q", in, out)
		}
	}
}

func TestCleanStripsUnknownTags(t *testing.T) {
	out := Clean(`<iframe src=x></iframe>salom`)
	if strings.Contains(out, "<iframe") {
		t.Errorf("iframe survived as raw tag: %q", out)
	}
	if !strings.Contains(out, "salom") {
		t.Errorf("text lost: %q", out)
	}
}

func TestCleanClosingTagKept(t *testing.T) {
	out := Clean(`</b> salom <i>x</i>`)
	if !strings.Contains(out, "</b>") {
		t.Errorf("closing tag slash lost: %q", out)
	}
	if !strings.Contains(out, "<i>") || !strings.Contains(out, "</i>") {
		t.Errorf("allowed tag mangled: %q", out)
	}
}

func TestCleanCodeBlock(t *testing.T) {
	in := "```go\nfmt.Println(1)\n```"
	out := Clean(in)
	if !strings.Contains(out, "<pre><code") {
		t.Errorf("code block not preserved: %q", out)
	}
	if strings.Contains(out, "$KRADCHAT_CODE$") {
		t.Errorf("token not restored: %q", out)
	}
	if strings.Contains(out, "\ngo\n") {
		t.Errorf("lang line leaked into code: %q", out)
	}
}

func TestCleanCodeBlockLang(t *testing.T) {
	out := Clean("```go\nfunc main() {\n  fmt.Println(\"salom\")\n}\n```")
	want := `<pre><code data-lang="go">func main() {`
	if !strings.Contains(out, want) {
		t.Errorf("lang attr missing: %q", out)
	}
	if !strings.Contains(out, "&#34;") {
		t.Errorf("quotes must be html-escaped in stored content: %q", out)
	}
}

func TestCleanCodeBlockNoLang(t *testing.T) {
	out := Clean("```\nsalom\n```")
	if !strings.Contains(out, "<pre><code>salom</code></pre>") {
		t.Errorf("no-lang block wrong: %q", out)
	}
}

func TestCleanCodeBlockMidLineNotFence(t *testing.T) {
	out := Clean("x ```go\nsalom\n```")
	if strings.Contains(out, "<pre><code>") {
		t.Errorf("mid-line fence became code block: %q", out)
	}
}

func TestCleanCodeBlockUnclosed(t *testing.T) {
	out := Clean("```go\nsalom")
	if strings.Contains(out, "<pre><code>") {
		t.Errorf("unclosed fence became code block: %q", out)
	}
	if !strings.Contains(out, "salom") {
		t.Errorf("text lost: %q", out)
	}
}

func TestCleanCodeBlockXSS(t *testing.T) {
	out := Clean("```\n<script>alert(1)</script>\n```")
	if strings.Contains(out, "<script") {
		t.Errorf("script survived in code block: %q", out)
	}
	if !strings.Contains(out, "alert(1)") {
		t.Errorf("code content lost: %q", out)
	}
}

func TestCleanText(t *testing.T) {
	out := CleanText(`<b>salom</b> &amp; dunyo`)
	if out != "salom & dunyo" {
		t.Errorf("CleanText = %q, want %q", out, "salom & dunyo")
	}
}

func TestSanitizeUsername(t *testing.T) {
	if SanitizeUsername("  TohirBek ") != "tohirbek" {
		t.Error("sanitize username failed")
	}
}

func TestValidateClientMsgID(t *testing.T) {
	valid := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"abc-123_456.789",
		"x",
		"a1",
	}
	invalid := []string{
		"",
		strings.Repeat("a", 65),
		"@bad",
		"has space",
		"a/b",
	}
	for _, id := range valid {
		if !ValidateClientMsgID(id) {
			t.Errorf("ValidateClientMsgID(%q) = false, want true", id)
		}
	}
	for _, id := range invalid {
		if ValidateClientMsgID(id) {
			t.Errorf("ValidateClientMsgID(%q) = true, want false", id)
		}
	}
}

func TestValidateMessageType(t *testing.T) {
	for _, typ := range []string{"text", "image", "video", "file", "emoji"} {
		if !ValidateMessageType(typ) {
			t.Errorf("ValidateMessageType(%q) = false, want true", typ)
		}
	}
	for _, typ := range []string{"", "system", "audio", "SCRIPT", "text; DROP"} {
		if ValidateMessageType(typ) {
			t.Errorf("ValidateMessageType(%q) = true, want false", typ)
		}
	}
}

func TestValidateFileURL(t *testing.T) {
	valid := []string{
		"",
		"/static/uploads/2024/abc.png",
		"/static/uploads/avatars/user-1.jpg",
		"/static/uploads/2026/12/uuid_prev.jpg",
	}
	invalid := []string{
		"https://evil.com/x.png",
		"javascript:alert(1)",
		"/static/uploads/../secret",
		"..",
		"/static/secret",
		"\\static\\uploads",
		"/static/uploads/foo\x00bar",
	}
	for _, u := range valid {
		if !ValidateFileURL(u) {
			t.Errorf("ValidateFileURL(%q) = false, want true", u)
		}
	}
	for _, u := range invalid {
		if ValidateFileURL(u) {
			t.Errorf("ValidateFileURL(%q) = true, want false", u)
		}
	}
}

func TestValidateAvatarURL(t *testing.T) {
	valid := []string{
		"",
		"/static/uploads/avatars/me.jpg",
	}
	invalid := []string{
		"/static/uploads/2024/abc.png", // avatar emas
		"/static/backgrounds/x.png",
		"https://evil.com/a.png",
		"/static/uploads/avatars/../secret.png",
	}
	for _, u := range valid {
		if !ValidateAvatarURL(u) {
			t.Errorf("ValidateAvatarURL(%q) = false, want true", u)
		}
	}
	for _, u := range invalid {
		if ValidateAvatarURL(u) {
			t.Errorf("ValidateAvatarURL(%q) = true, want false", u)
		}
	}
}

func TestValidateUsernameDotRules(t *testing.T) {
	valid := []string{
		"bob",
		"alice_99",
		"bob.smith",
		"user_x.y",
	}
	invalid := []string{
		".bob",   // boshidagi nuqta
		"bob.",   // oxiridagi nuqta
		"bob..x", // ketma-ket nuqta
		"_bob",   // boshidagi underscore
		"bob_",   // oxiridagi underscore
		"a__b",   // ketma-ket underscore
		"...",    // faqat nuqtalar
	}
	for _, u := range valid {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("ValidateUsername(%q) = %v, want nil", u, err)
		}
	}
	for _, u := range invalid {
		if err := ValidateUsername(u); err == nil {
			t.Errorf("ValidateUsername(%q) = nil, want error", u)
		}
	}
}

func TestValidatePasswordByteLimit(t *testing.T) {
	// bcrypt 72 BYTE bilan chegaralangan — 73 baytli parol trunkiatsiya
	// xavfi tufayli qabul qilinmasligi kerak.
	ok := "p" + strings.Repeat("a", 69) + "1" // 71 bayt — qabul
	if err := ValidatePassword(ok); err != nil {
		t.Errorf("71-byte password rejected: %v", err)
	}
	boundary := "p" + strings.Repeat("a", 70) + "1" // 72 bayt — chegara
	if err := ValidatePassword(boundary); err != nil {
		t.Errorf("72-byte password rejected: %v", err)
	}
	over := "p" + strings.Repeat("a", 71) + "1" // 73 bayt — rad
	if err := ValidatePassword(over); err == nil {
		t.Error("73-byte password accepted, want error")
	}
	// 20 belgi lekin 74 bayt (4-baytli emoji) — rad etilishi kerak,
	// chunki rune sanasi emas, BYTE miqdori hal qiladi.
	emoji := "a1" + strings.Repeat("😀", 18) // 2 + 72 = 74 bayt
	if err := ValidatePassword(emoji); err == nil {
		t.Error("multibyte over-72-byte password accepted, want error")
	}
}

func TestSanitizeDisplayNameStripsControlChars(t *testing.T) {
	in := "Tohir\x00Bek\n\r"
	out := SanitizeDisplayName(in)
	if strings.ContainsAny(out, "\x00\n\r") {
		t.Errorf("control chars survived: %q", out)
	}
	if out != "TohirBek" {
		t.Errorf("SanitizeDisplayName = %q, want %q", out, "TohirBek")
	}
}
