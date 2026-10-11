package repository

import (
	"context"
	"time"
)

func (r *twoFARepo) SetEnabled(ctx context.Context, userID string, enabled bool, secret string) error {
	if secret != "" {
		enc, err := encryptSecret(r.encKey, secret)
		if err != nil {
			return wrapErr(err)
		}
		secret = enc
	}
	var err error
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx,
			`UPDATE users SET twofa_enabled=$2, twofa_secret=$3 WHERE id=$1`,
			userID, enabled, secret)
	} else {
		_, err = r.db.ExecContext(ctx,
			`UPDATE users SET twofa_enabled=?, twofa_secret=? WHERE id=?`,
			enabled, secret, userID)
	}
	return wrapErr(err)
}

func (r *twoFARepo) GetSecret(ctx context.Context, userID string) (string, error) {
	var s string
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx, `SELECT twofa_secret FROM users WHERE id=$1`, userID).Scan(&s)
	} else {
		err = r.db.QueryRowContext(ctx, `SELECT twofa_secret FROM users WHERE id=?`, userID).Scan(&s)
	}
	if err != nil {
		return "", wrapErr(err)
	}
	// DB'da shifrlangan saqlanadi; legacy plaintext ham qabul qilinadi.
	if pt, ok := decryptSecretCompat(r.encKey, s); ok {
		return pt, nil
	}
	return "", wrapErr(errDecryption)
}

func (r *twoFARepo) LogPasswordChange(ctx context.Context, userID, ip, userAgent string) error {
	var err error
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx,
			`INSERT INTO password_change_log (user_id, ip, user_agent) VALUES ($1,$2,$3)`,
			userID, ip, userAgent)
	} else {
		_, err = r.db.ExecContext(ctx,
			`INSERT INTO password_change_log (user_id, ip, user_agent) VALUES (?,?,?)`,
			userID, ip, userAgent)
	}
	return wrapErr(err)
}

// LatestPasswordChange — oxirgi parol o'zgarishi vaqti (WS revocation uchun).
// Yozuv bo'lmasa zero time + ErrNotFound qaytaradi.
func (r *twoFARepo) LatestPasswordChange(ctx context.Context, userID string) (t time.Time, err error) {
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx,
			`SELECT created_at FROM password_change_log WHERE user_id=$1 ORDER BY created_at DESC LIMIT 1`,
			userID).Scan(&ScanTime{T: &t})
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT created_at FROM password_change_log WHERE user_id=? ORDER BY created_at DESC LIMIT 1`,
			userID).Scan(&ScanTime{T: &t})
	}
	if err != nil {
		return time.Time{}, wrapErr(err)
	}
	return t, nil
}
