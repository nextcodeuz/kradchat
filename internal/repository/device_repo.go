package repository

import (
	"context"
	"database/sql"

	"kradchat/internal/model"

	"github.com/google/uuid"
)

func (r *deviceRepo) Create(ctx context.Context, d *model.Device) error {
	if r.dialect == "postgres" {
		return wrapErr(r.db.QueryRowContext(ctx, `
			INSERT INTO devices (user_id, ip, user_agent, device_type, browser, os, fingerprint)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at`,
			d.UserID, d.IP, d.UserAgent, d.DeviceType, d.Browser, d.OS, d.Fingerprint,
		).Scan(&d.ID, &ScanTime{T: &d.CreatedAt}))
	}
	d.ID = uuid.NewString()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO devices (id, user_id, ip, user_agent, device_type, browser, os, fingerprint)
		VALUES (?,?,?,?,?,?,?,?)`,
		d.ID, d.UserID, d.IP, d.UserAgent, d.DeviceType, d.Browser, d.OS, d.Fingerprint,
	)
	return wrapErr(err)
}

func (r *deviceRepo) Update(ctx context.Context, d *model.Device) error {
	var err error
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx, `
			UPDATE devices SET ip=$2, user_agent=$3, device_type=$4, browser=$5, os=$6,
			       is_active=$7, last_seen=$8 WHERE id=$1`,
			d.ID, d.IP, d.UserAgent, d.DeviceType, d.Browser, d.OS, d.IsActive, d.LastSeen)
	} else {
		_, err = r.db.ExecContext(ctx, `
			UPDATE devices SET ip=?, user_agent=?, device_type=?, browser=?, os=?,
			       is_active=?, last_seen=? WHERE id=?`,
			d.IP, d.UserAgent, d.DeviceType, d.Browser, d.OS, d.IsActive, d.LastSeen, d.ID)
	}
	return wrapErr(err)
}

func (r *deviceRepo) ByID(ctx context.Context, id string) (*model.Device, error) {
	d := &model.Device{}
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx, `
			SELECT id, user_id, ip, user_agent, device_type, browser, os, fingerprint, is_active, last_seen, created_at
			FROM devices WHERE id=$1`, id,
		).Scan(&d.ID, &d.UserID, &d.IP, &d.UserAgent, &d.DeviceType, &d.Browser, &d.OS,
			&d.Fingerprint, &d.IsActive, &ScanTime{T: &d.LastSeen}, &ScanTime{T: &d.CreatedAt})
	} else {
		err = r.db.QueryRowContext(ctx, `
			SELECT id, user_id, ip, user_agent, device_type, browser, os, fingerprint, is_active, last_seen, created_at
			FROM devices WHERE id=?`, id,
		).Scan(&d.ID, &d.UserID, &d.IP, &d.UserAgent, &d.DeviceType, &d.Browser, &d.OS,
			&d.Fingerprint, &d.IsActive, &ScanTime{T: &d.LastSeen}, &ScanTime{T: &d.CreatedAt})
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	return d, nil
}

func (r *deviceRepo) ByUser(ctx context.Context, userID string) ([]*model.Device, error) {
	var out []*model.Device
	var q *sql.Rows
	var err error
	if r.dialect == "postgres" {
		q, err = r.db.QueryContext(ctx, `
			SELECT id, user_id, ip, user_agent, device_type, browser, os, fingerprint, is_active, last_seen, created_at
			FROM devices WHERE user_id=$1 ORDER BY last_seen DESC`, userID)
	} else {
		q, err = r.db.QueryContext(ctx, `
			SELECT id, user_id, ip, user_agent, device_type, browser, os, fingerprint, is_active, last_seen, created_at
			FROM devices WHERE user_id=? ORDER BY last_seen DESC`, userID)
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	defer q.Close()
	for q.Next() {
		d := &model.Device{}
		if err = q.Scan(&d.ID, &d.UserID, &d.IP, &d.UserAgent, &d.DeviceType, &d.Browser, &d.OS,
			&d.Fingerprint, &d.IsActive, &ScanTime{T: &d.LastSeen}, &ScanTime{T: &d.CreatedAt}); err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, d)
	}
	return out, wrapErr(q.Err())
}

func (r *deviceRepo) ByFingerprint(ctx context.Context, userID, fingerprint string) (*model.Device, error) {
	d := &model.Device{}
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx, `
			SELECT id, user_id, ip, user_agent, device_type, browser, os, fingerprint, is_active, last_seen, created_at
			FROM devices WHERE user_id=$1 AND fingerprint=$2 ORDER BY last_seen DESC LIMIT 1`,
			userID, fingerprint,
		).Scan(&d.ID, &d.UserID, &d.IP, &d.UserAgent, &d.DeviceType, &d.Browser, &d.OS,
			&d.Fingerprint, &d.IsActive, &ScanTime{T: &d.LastSeen}, &ScanTime{T: &d.CreatedAt})
	} else {
		err = r.db.QueryRowContext(ctx, `
			SELECT id, user_id, ip, user_agent, device_type, browser, os, fingerprint, is_active, last_seen, created_at
			FROM devices WHERE user_id=? AND fingerprint=? ORDER BY last_seen DESC LIMIT 1`,
			userID, fingerprint,
		).Scan(&d.ID, &d.UserID, &d.IP, &d.UserAgent, &d.DeviceType, &d.Browser, &d.OS,
			&d.Fingerprint, &d.IsActive, &ScanTime{T: &d.LastSeen}, &ScanTime{T: &d.CreatedAt})
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	return d, nil
}

func (r *deviceRepo) Deactivate(ctx context.Context, id, userID string) error {
	if r.dialect == "postgres" {
		res, err := r.db.ExecContext(ctx, `UPDATE devices SET is_active=false WHERE id=$1 AND user_id=$2 AND is_active=TRUE`, id, userID)
		if err != nil {
			return wrapErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	}
	res, err := r.db.ExecContext(ctx, `UPDATE devices SET is_active=false WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return wrapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountActive — foydalanuvchining aktiv qurilmalari soni (qurilma limiti uchun).
func (r *deviceRepo) CountActive(ctx context.Context, userID string) (int, error) {
	var n int
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE user_id=$1 AND is_active=TRUE`, userID).Scan(&n)
	} else {
		err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE user_id=? AND is_active=1`, userID).Scan(&n)
	}
	return n, wrapErr(err)
}
