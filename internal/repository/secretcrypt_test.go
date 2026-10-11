package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestSecretCryptRoundtrip — H5: shifrlash/ochish aylanishi.
func TestSecretCryptRoundtrip(t *testing.T) {
	key := deriveSecretKey(strings.Repeat("s", 32))
	enc, err := encryptSecret(key, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(enc, secretCryptPrefix) {
		t.Fatalf("prefix yo'q: %q", enc)
	}
	if strings.Contains(enc, "JBSWY3DPEHPK3PXP") {
		t.Fatal("plaintext sizdi")
	}
	pt, ok := decryptSecretCompat(key, enc)
	if !ok || pt != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("decrypt: %q ok=%v", pt, ok)
	}
	// Legacy plaintext qabul qilinadi (mavjud 2FA userlar bloklanmaydi).
	if pt, ok := decryptSecretCompat(key, "JBSWY3DPEHPK3PXP"); !ok || pt != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("legacy: %q ok=%v", pt, ok)
	}
	// Boshqa kalit bilan ochilmaydi (fail-closed).
	other := deriveSecretKey(strings.Repeat("x", 32))
	if _, ok := decryptSecretCompat(other, enc); ok {
		t.Fatal("noto'g'ri kalit ochdi")
	}
	// Buzilgan v1 yozuv — fail-closed.
	if _, ok := decryptSecretCompat(key, secretCryptPrefix+"!!!"); ok {
		t.Fatal("buzilgan yozuv ochildi")
	}
	if _, ok := decryptSecretCompat(nil, enc); ok {
		t.Fatal("nilsiz kalit ochdi")
	}
	if s, _ := decryptSecretCompat(key, ""); s != "" {
		t.Fatal("bo'sh secret")
	}
}

// TestTwoFASecretEncryptedAtRest — H5 integratsiya: DB'da "v1." saqlanadi,
// o'qishda plaintext qaytadi; legacy plaintext ham ishlaydi.
func TestTwoFASecretEncryptedAtRest(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, twofa_enabled INTEGER, twofa_secret TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, twofa_enabled, twofa_secret) VALUES ('u1',0,''),('u2',0,'')`); err != nil {
		t.Fatal(err)
	}
	key := deriveSecretKey(strings.Repeat("s", 32))
	r := &twoFARepo{base{db: db, dialect: "sqlite", encKey: key}}
	ctx := context.Background()

	if err := r.SetEnabled(ctx, "u1", true, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("set: %v", err)
	}
	var raw string
	if err := db.QueryRow(`SELECT twofa_secret FROM users WHERE id='u1'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, secretCryptPrefix) {
		t.Fatalf("DB'da plaintext: %q", raw)
	}
	got, err := r.GetSecret(ctx, "u1")
	if err != nil || got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("get: %q err=%v", got, err)
	}
	// Legacy plaintext yozuv o'qiladi.
	if _, err := db.Exec(`UPDATE users SET twofa_secret='LEGACYSECRET' WHERE id='u2'`); err != nil {
		t.Fatal(err)
	}
	if got, err := r.GetSecret(ctx, "u2"); err != nil || got != "LEGACYSECRET" {
		t.Fatalf("legacy get: %q err=%v", got, err)
	}
}
