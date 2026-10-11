package security

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	ErrUsernameTooShort = errors.New("username must be at least 3 chars")
	ErrUsernameTooLong  = errors.New("username must be at most 50 chars")
	ErrUsernameInvalid  = errors.New("username allows only letters, digits, _, .")
	ErrPasswordTooShort = errors.New("password must be at least 8 chars")
	ErrPasswordTooLong  = errors.New("password must be at most 72 chars")
	ErrPasswordWeak     = errors.New("password must contain letter and digit")
	ErrRoomInvalid      = errors.New("room name invalid")
	ErrContentTooLong   = errors.New("message too long")
	ErrBadID            = errors.New("missing or invalid id")
	ErrClientMsgID      = errors.New("missing or invalid client_msg_id")
	ErrMessageType      = errors.New("unsupported message type")
	ErrFileURL          = errors.New("file_url must be a local /static/uploads path")
	ErrFileTooBig       = errors.New("file too large")
)

var (
	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.]+$`)
	roomRe     = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	// Shaxsiy xona (DM): dm:<uuid>:<uuid>. UUID foydalanuvchi ID'laridir,
	// shuning uchun xona nomi server tomondan qat'iy quriladi va boshqa
	// formatdagi nomlar qabul qilinmaydi.
	dmRoomRe = regexp.MustCompile(`^dm:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// Control chars excluding \n \r \t
	controlRe = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`)
	// Display name uchun barcha control charlar (yangi qator ham)
	displayControlRe = regexp.MustCompile(`[\x00-\x1F\x7F]`)
	// UUID format (v4 with dashes) or short alnum token produced by clients
	cmidRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
	uploadRe  = regexp.MustCompile(`^/static/uploads/[a-zA-Z0-9/_.-]+$`)
	avatarURL = "/static/uploads/avatars/"
)

const MaxContentLen = 4096

func ValidateUsername(u string) error {
	n := utf8.RuneCountInString(u)
	if n < 3 {
		return ErrUsernameTooShort
	}
	if n > 50 {
		return ErrUsernameTooLong
	}
	if !usernameRe.MatchString(u) {
		return ErrUsernameInvalid
	}
	// Boshida/oxirida nuqta yoki underscore — adashtiruvchi va path'ga
	// o'xshash nomlar (".bob", "_x", "bob.") qabul qilinmaydi.
	if strings.HasPrefix(u, ".") || strings.HasPrefix(u, "_") ||
		strings.HasSuffix(u, ".") || strings.HasSuffix(u, "_") {
		return ErrUsernameInvalid
	}
	// Ketma-ket nuqta/underscore ham taqiqlanadi ("..", "__").
	if strings.Contains(u, "..") || strings.Contains(u, "__") {
		return ErrUsernameInvalid
	}
	return nil
}

func ValidatePassword(p string) error {
	n := utf8.RuneCountInString(p)
	if n < 8 {
		return ErrPasswordTooShort
	}
	// bcrypt faqat 72 BYTE qabul qiladi va qolganini jimgina kesib tashlaydi.
	// Shuning uchun rune emas, bayt o'lchamini tekshiramiz — aks holda
	// ikkita har xil parol bir xil hash berishi mumkin edi.
	if len(p) > 72 {
		return ErrPasswordTooLong
	}
	hasLetter := false
	hasDigit := false
	for _, r := range p {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			hasLetter = true
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return ErrPasswordWeak
	}
	return nil
}

func ValidateRoom(room string) bool {
	if room == "" {
		return false
	}
	return roomRe.MatchString(room)
}

// DMRoomFor — ikki foydalanuvchi orasidagi shaxsiy xona nomini quriladi.
// ID'lar alifbo tartibida joylashtiriladi — shunda A->B va B->A xonasi bitta.
func DMRoomFor(a, b string) string {
	if a < b {
		return "dm:" + a + ":" + b
	}
	return "dm:" + b + ":" + a
}

// ValidateDMRoom — xona nomi to'g'ri DM formatida ekanini tekshiradi.
func ValidateDMRoom(room string) bool {
	return dmRoomRe.MatchString(room)
}

// IsDMRoomMember — user DM xonasining ishtirokchilaridan biri ekanini
// tekshiradi. Boshqa foydalanuvchilar bu xonaga kira olmaydi.
func IsDMRoomMember(room, userID string) bool {
	if !ValidateDMRoom(room) || userID == "" {
		return false
	}
	rest := room[3:] // "dm:" prefiksini tashlab yuboramiz
	a, b, found := strings.Cut(rest, ":")
	if !found {
		return false
	}
	return a == userID || b == userID
}

// DMRoomPeer — DM xonasidagi suhbatdoshning ID'si (berilgan user'ning o'zi emas).
// Xona ishtirokchisi bo'lmasa bo'sh string qaytaradi.
func DMRoomPeer(room, userID string) string {
	if !ValidateDMRoom(room) || userID == "" {
		return ""
	}
	rest := room[3:]
	a, b, found := strings.Cut(rest, ":")
	if !found {
		return ""
	}
	switch {
	case a == userID:
		return b
	case b == userID:
		return a
	default:
		return ""
	}
}

func ValidateContent(c string) error {
	if len(c) > MaxContentLen {
		return ErrContentTooLong
	}
	if controlRe.MatchString(c) {
		return errors.New("message contains forbidden control characters")
	}
	return nil
}

// ValidateClientMsgID checks the dedup key sent by clients.
func ValidateClientMsgID(id string) bool {
	return id != "" && cmidRe.MatchString(id)
}

// ValidateUserID — mention/user ID UUID formatda bo'lishi kerak.
func ValidateUserID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c == '-' {
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// ValidMessageTypes — xabarning ruxsat etilgan turlari.
var ValidMessageTypes = map[string]bool{
	"text":  true,
	"image": true,
	"video": true,
	"file":  true,
	"emoji": true,
}

func ValidateMessageType(t string) bool {
	return ValidMessageTypes[t]
}

// ValidateFileURL — faqat mahalliy /static/uploads yo'li ruxsat etiladi;
// boshqa protokollar yoki path traversal bloklanadi.
func ValidateFileURL(u string) bool {
	if u == "" {
		return true
	}
	if strings.Contains(u, "..") || strings.ContainsAny(u, "\\\x00") {
		return false
	}
	return uploadRe.MatchString(u)
}

// CleanFileName — fayl xabaridagi original nomni xavfsiz ko'rinishga keltiradi:
// yo'l prefixi olib tashlanadi, control belgilar o'chiriladi, uzunlik cheklanadi.
func CleanFileName(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\\", "/")
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if b := []byte(s); len(b) > 255 {
		s = string(b[:255])
	}
	return s
}

// ValidateAvatarURL — avatar faqat avatars papkasi ichidagi mahalliy
// path bo'lishi kerak, shuning uchun boshqa fayllarga havola qo'yib
// bo'lmaydi (masalan boshqa userning avatarini, yoki background).
func ValidateAvatarURL(u string) bool {
	if u == "" {
		return true
	}
	if strings.Contains(u, "..") || strings.ContainsAny(u, "\\\x00") {
		return false
	}
	return strings.HasPrefix(u, avatarURL) && uploadRe.MatchString(u)
}

// SanitizeUsername lower-case + trims, returns cleaned value.
func SanitizeUsername(u string) string {
	return strings.ToLower(strings.TrimSpace(u))
}

func SanitizeDisplayName(d string) string {
	d = strings.TrimSpace(d)
	d = displayControlRe.ReplaceAllString(d, "")
	if d == "" {
		return ""
	}
	r := []rune(d)
	if len(r) > 100 {
		r = r[:100]
	}
	return string(r)
}
