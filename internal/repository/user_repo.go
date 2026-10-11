package repository

import (
	"context"
	"time"

	"kradchat/internal/model"

	"github.com/google/uuid"
)

func (r *userRepo) Create(ctx context.Context, u *model.User) error {
	if r.dialect == "postgres" {
		return wrapErr(r.db.QueryRowContext(ctx, `
			INSERT INTO users (username, display_name, password_hash, avatar_url)
			VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
			u.Username, u.DisplayName, u.PasswordHash, u.AvatarURL,
		).Scan(&u.ID, &ScanTime{T: &u.CreatedAt}))
	}
	u.ID = uuid.NewString()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (id, username, display_name, password_hash, avatar_url)
		VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, u.PasswordHash, u.AvatarURL,
	)
	return wrapErr(err)
}

func (r *userRepo) ByUsername(ctx context.Context, username string) (*model.User, error) {
	// Username doim kichik harfda saqlanadi (service darajasida
	// SanitizeUsername orqali), shuning uchun to'g'ridan-to'g'ri taqqoslash
	// indeksdan foydalanadi va `lower()` dagi kabi sekinlashmaydi.
	u := &model.User{}
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx, `
			SELECT id, username, display_name, password_hash, avatar_url, hide_username,
			       show_last_seen, show_online_status, notifications_enabled, verified,
			       twofa_enabled, twofa_secret, last_seen, created_at
			FROM users WHERE username = $1`, username,
		).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.AvatarURL,
			&u.HideUsername, &u.ShowLastSeen, &u.ShowOnlineStatus, &u.NotificationsEnabled, &u.Verified,
			&u.TwoFAEnabled, &u.TwoFASecret, &ScanTime{T: &u.LastSeen}, &ScanTime{T: &u.CreatedAt})
	} else {
		err = r.db.QueryRowContext(ctx, `
			SELECT id, username, display_name, password_hash, avatar_url, hide_username,
			       show_last_seen, show_online_status, notifications_enabled, verified,
			       twofa_enabled, twofa_secret, last_seen, created_at
			FROM users WHERE username = ?`, username,
		).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.AvatarURL,
			&u.HideUsername, &u.ShowLastSeen, &u.ShowOnlineStatus, &u.NotificationsEnabled, &u.Verified,
			&u.TwoFAEnabled, &u.TwoFASecret, &ScanTime{T: &u.LastSeen}, &ScanTime{T: &u.CreatedAt})
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	// twofa_secret DB'da shifrlangan (legacy plaintext ham qabul qilinadi).
	if pt, ok := decryptSecretCompat(r.encKey, u.TwoFASecret); ok {
		u.TwoFASecret = pt
	} else {
		u.TwoFASecret = ""
	}
	return u, nil
}

func (r *userRepo) ByID(ctx context.Context, id string) (*model.User, error) {
	u := &model.User{}
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx, `
			SELECT id, username, display_name, password_hash, avatar_url, hide_username,
			       show_last_seen, show_online_status, notifications_enabled, verified,
			       twofa_enabled, twofa_secret, last_seen, created_at
			FROM users WHERE id = $1`, id,
		).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.AvatarURL,
			&u.HideUsername, &u.ShowLastSeen, &u.ShowOnlineStatus, &u.NotificationsEnabled, &u.Verified,
			&u.TwoFAEnabled, &u.TwoFASecret, &ScanTime{T: &u.LastSeen}, &ScanTime{T: &u.CreatedAt})
	} else {
		err = r.db.QueryRowContext(ctx, `
			SELECT id, username, display_name, password_hash, avatar_url, hide_username,
			       show_last_seen, show_online_status, notifications_enabled, verified,
			       twofa_enabled, twofa_secret, last_seen, created_at
			FROM users WHERE id = ?`, id,
		).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.AvatarURL,
			&u.HideUsername, &u.ShowLastSeen, &u.ShowOnlineStatus, &u.NotificationsEnabled, &u.Verified,
			&u.TwoFAEnabled, &u.TwoFASecret, &ScanTime{T: &u.LastSeen}, &ScanTime{T: &u.CreatedAt})
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	// twofa_secret DB'da shifrlangan (legacy plaintext ham qabul qilinadi).
	if pt, ok := decryptSecretCompat(r.encKey, u.TwoFASecret); ok {
		u.TwoFASecret = pt
	} else {
		u.TwoFASecret = ""
	}
	return u, nil
}

// SetVerified — user'ning verified (tasdiqlangan) holatini o'rnatadi.
func (r *userRepo) SetVerified(ctx context.Context, userID string, verified bool) error {
	var err error
	var res interface{ RowsAffected() (int64, error) }
	if r.dialect == "postgres" {
		var rr interface{ RowsAffected() (int64, error) }
		_ = rr
		res0, e := r.db.ExecContext(ctx, `UPDATE users SET verified=$2 WHERE id=$1`, userID, verified)
		res, err = res0, e
	} else {
		res0, e := r.db.ExecContext(ctx, `UPDATE users SET verified=? WHERE id=?`, verified, userID)
		res, err = res0, e
	}
	if err != nil {
		return wrapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *userRepo) Update(ctx context.Context, u *model.User) error {
	var err error
	var res interface{ RowsAffected() (int64, error) }
	if r.dialect == "postgres" {
		res0, e := r.db.ExecContext(ctx, `
			UPDATE users SET display_name=$2, avatar_url=$3, hide_username=$4,
			       show_last_seen=$5, show_online_status=$6, notifications_enabled=$7
			WHERE id=$1`,
			u.ID, u.DisplayName, u.AvatarURL, u.HideUsername, u.ShowLastSeen,
			u.ShowOnlineStatus, u.NotificationsEnabled)
		res, err = res0, e
	} else {
		res0, e := r.db.ExecContext(ctx, `
			UPDATE users SET display_name=?, avatar_url=?, hide_username=?,
			       show_last_seen=?, show_online_status=?, notifications_enabled=?
			WHERE id=?`,
			u.DisplayName, u.AvatarURL, u.HideUsername, u.ShowLastSeen,
			u.ShowOnlineStatus, u.NotificationsEnabled, u.ID)
		res, err = res0, e
	}
	if err != nil {
		return wrapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *userRepo) UpdatePassword(ctx context.Context, userID, hash string) error {
	var err error
	var res interface{ RowsAffected() (int64, error) }
	if r.dialect == "postgres" {
		res0, e := r.db.ExecContext(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, userID, hash)
		res, err = res0, e
	} else {
		res0, e := r.db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, hash, userID)
		res, err = res0, e
	}
	if err != nil {
		return wrapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *userRepo) SetLastSeen(ctx context.Context, userID string, t time.Time) error {
	var err error
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx, `UPDATE users SET last_seen=$2 WHERE id=$1`, userID, t)
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE users SET last_seen=? WHERE id=?`, t, userID)
	}
	return wrapErr(err)
}
