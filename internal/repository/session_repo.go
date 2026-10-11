package repository

import (
	"context"

	"kradchat/internal/model"

	"github.com/google/uuid"
)

func (r *sessionRepo) Create(ctx context.Context, s *model.Session) error {
	if r.dialect == "postgres" {
		return wrapErr(r.db.QueryRowContext(ctx, `
			INSERT INTO sessions (user_id, device_id, refresh_hash, ip, user_agent, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at`,
			s.UserID, nullableStr(s.DeviceID), s.RefreshHash, s.IP, s.UserAgent, s.ExpiresAt,
		).Scan(&s.ID, &ScanTime{T: &s.CreatedAt}))
	}
	s.ID = uuid.NewString()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, device_id, refresh_hash, ip, user_agent, expires_at)
		VALUES (?,?,?,?,?,?,?)`,
		s.ID, s.UserID, nullableStr(s.DeviceID), s.RefreshHash, s.IP, s.UserAgent, s.ExpiresAt,
	)
	return wrapErr(err)
}

func (r *sessionRepo) ByRefreshHash(ctx context.Context, hash string) (*model.Session, error) {
	s := &model.Session{}
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx, `
			SELECT id, user_id, COALESCE(device_id,''), refresh_hash, ip, user_agent, expires_at, revoked_at, created_at
			FROM sessions WHERE refresh_hash=$1`, hash,
		).Scan(&s.ID, &s.UserID, &s.DeviceID, &s.RefreshHash, &s.IP, &s.UserAgent,
			&ScanTime{T: &s.ExpiresAt}, &ScanTime{T: &s.RevokedAt}, &ScanTime{T: &s.CreatedAt})
	} else {
		err = r.db.QueryRowContext(ctx, `
			SELECT id, user_id, COALESCE(device_id,''), refresh_hash, ip, user_agent, expires_at, revoked_at, created_at
			FROM sessions WHERE refresh_hash=?`, hash,
		).Scan(&s.ID, &s.UserID, &s.DeviceID, &s.RefreshHash, &s.IP, &s.UserAgent,
			&ScanTime{T: &s.ExpiresAt}, &ScanTime{T: &s.RevokedAt}, &ScanTime{T: &s.CreatedAt})
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	return s, nil
}

func (r *sessionRepo) Revoke(ctx context.Context, id string) error {
	var err error
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, id)
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=`+r.nowExpr()+` WHERE id=?`, id)
	}
	return wrapErr(err)
}

func (r *sessionRepo) RevokeAllForUser(ctx context.Context, userID string) error {
	var err error
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=`+r.nowExpr()+` WHERE user_id=? AND revoked_at IS NULL`, userID)
	}
	return wrapErr(err)
}

func (r *sessionRepo) RevokeDevice(ctx context.Context, deviceID string) error {
	var err error
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=now() WHERE device_id=$1 AND revoked_at IS NULL`, deviceID)
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=`+r.nowExpr()+` WHERE device_id=? AND revoked_at IS NULL`, deviceID)
	}
	return wrapErr(err)
}

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
