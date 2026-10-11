package repository

import (
	"context"
	"database/sql"
)

// Block — foydalanuvchi bloklash yozuvini qo'shadi (idempotent: allaqachon
// mavjud bo'lsa conflict qaytarilmaydi, oddiygina o'tadi).
func (r *blockRepo) Block(ctx context.Context, blockerID, blockedID string) error {
	var err error
	switch r.dialect {
	case "postgres":
		_, err = r.db.ExecContext(ctx, `
			INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)
			ON CONFLICT (blocker_id, blocked_id) DO NOTHING`, blockerID, blockedID)
	case "sqlite":
		_, err = r.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO blocks (blocker_id, blocked_id) VALUES (?, ?)`, blockerID, blockedID)
	default: // mysql, mariadb
		_, err = r.db.ExecContext(ctx, `
			INSERT IGNORE INTO blocks (blocker_id, blocked_id) VALUES (?, ?)`, blockerID, blockedID)
	}
	return wrapErr(err)
}

// Unblock — bloklashni olib tashlaydi.
func (r *blockRepo) Unblock(ctx context.Context, blockerID, blockedID string) error {
	var err error
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx, `DELETE FROM blocks WHERE blocker_id=$1 AND blocked_id=$2`, blockerID, blockedID)
	} else {
		_, err = r.db.ExecContext(ctx, `DELETE FROM blocks WHERE blocker_id=? AND blocked_id=?`, blockerID, blockedID)
	}
	return wrapErr(err)
}

// Blocks — blockerID foydalanuvchisi blockedID ni bloklaganmi.
func (r *blockRepo) Blocks(ctx context.Context, blockerID, blockedID string) (bool, error) {
	var one int
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx,
			`SELECT 1 FROM blocks WHERE blocker_id=$1 AND blocked_id=$2`, blockerID, blockedID).Scan(&one)
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT 1 FROM blocks WHERE blocker_id=? AND blocked_id=?`, blockerID, blockedID).Scan(&one)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, wrapErr(err)
	}
	return one == 1, nil
}

// BlocksAny — ikki foydalanuvchi orasida bloklash bor-yo'qligi (istalgan yo'nalishda).
func (r *blockRepo) BlocksAny(ctx context.Context, a, b string) (bool, error) {
	var one int
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx,
			`SELECT 1 FROM blocks WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1) LIMIT 1`, a, b).Scan(&one)
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT 1 FROM blocks WHERE (blocker_id=? AND blocked_id=?) OR (blocker_id=? AND blocked_id=?) LIMIT 1`, a, b, b, a).Scan(&one)
	}
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, wrapErr(err)
	}
	return one == 1, nil
}

// All — barcha bloklash yozuvlarini qaytaradi (startup cache'ni to'ldirish uchun).
func (r *blockRepo) All(ctx context.Context) ([]BlockPair, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT blocker_id, blocked_id FROM blocks`)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()
	var out []BlockPair
	for rows.Next() {
		var p BlockPair
		if err := rows.Scan(&p.BlockerID, &p.BlockedID); err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, p)
	}
	return out, wrapErr(rows.Err())
}
