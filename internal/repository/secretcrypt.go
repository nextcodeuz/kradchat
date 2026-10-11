package repository

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// TOTP secret'lar DB'da shifrlangan saqlanadi (H5): DB read (dump/backup)
// olgan hujumchi 2FA kodlarini generatsiya qila olmasligi uchun.
// Format: "v1." + base64(nonce|ciphertext), AES-256-GCM, kalit JWT secret'dan.
// Eski plaintext yozuvlar o'qishda qabul qilinadi (legacy fallback) —
// keyingi SetEnabled'da avtomatik shifrlanadi. Kalit almashsa eski yozuvlar
// o'qilmaydi (fail-closed): JWT secret barqaror bo'lishi shart.
const secretCryptPrefix = "v1."

// deriveSecretKey — JWT secret'dan 32 baytlik kalit (domain separation bilan).
func deriveSecretKey(jwtSecret string) *[32]byte {
	sum := sha256.Sum256([]byte("kradchat-twofa-v1|" + jwtSecret))
	return &sum
}

// encryptSecret — plaintext secret'ni shifrlaydi ("": "" qaytaradi).
func encryptSecret(key *[32]byte, plaintext string) (string, error) {
	if plaintext == "" || key == nil {
		return plaintext, nil
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return secretCryptPrefix + base64.RawURLEncoding.EncodeToString(ct), nil
}

// decryptSecretCompat — shifrlangan yoki legacy plaintext secret'ni ochadi.
// Qaytaradi: (secret, ok). ok=false faqat buzilgan v1 yozuvda (fail-closed).
func decryptSecretCompat(key *[32]byte, stored string) (string, bool) {
	if stored == "" {
		return "", true
	}
	if !strings.HasPrefix(stored, secretCryptPrefix) {
		return stored, true // legacy plaintext — qabul qilinadi
	}
	if key == nil {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(stored, secretCryptPrefix))
	if err != nil {
		return "", false
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", false
	}
	if len(raw) < gcm.NonceSize() {
		return "", false
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", false
	}
	return string(pt), true
}
