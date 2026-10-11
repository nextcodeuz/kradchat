package repository

import (
	"context"

	"kradchat/internal/model"

	"github.com/google/uuid"
)

func (r *backgroundRepo) AllActive(ctx context.Context) ([]*model.Background, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, category, file_path, is_active, created_at FROM chat_backgrounds WHERE is_active=true ORDER BY created_at`)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()
	var out []*model.Background
	for rows.Next() {
		b := &model.Background{}
		if err = rows.Scan(&b.ID, &b.Name, &b.Category, &b.FilePath, &b.IsActive, &ScanTime{T: &b.CreatedAt}); err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, b)
	}
	return out, wrapErr(rows.Err())
}

func (r *backgroundRepo) ByPath(ctx context.Context, path string) (*model.Background, error) {
	b := &model.Background{}
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx,
			`SELECT id, name, category, file_path, is_active, created_at FROM chat_backgrounds WHERE file_path=$1`,
			path,
		).Scan(&b.ID, &b.Name, &b.Category, &b.FilePath, &b.IsActive, &ScanTime{T: &b.CreatedAt})
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT id, name, category, file_path, is_active, created_at FROM chat_backgrounds WHERE file_path=?`,
			path,
		).Scan(&b.ID, &b.Name, &b.Category, &b.FilePath, &b.IsActive, &ScanTime{T: &b.CreatedAt})
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	return b, nil
}

func (r *backgroundRepo) Create(ctx context.Context, b *model.Background) error {
	if r.dialect == "postgres" {
		return wrapErr(r.db.QueryRowContext(ctx, `
			INSERT INTO chat_backgrounds (name, category, file_path) VALUES ($1,$2,$3)
			RETURNING id, created_at`,
			b.Name, b.Category, b.FilePath,
		).Scan(&b.ID, &ScanTime{T: &b.CreatedAt}))
	}
	b.ID = uuid.NewString()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO chat_backgrounds (id, name, category, file_path) VALUES (?,?,?,?)`,
		b.ID, b.Name, b.Category, b.FilePath)
	return wrapErr(err)
}

func (r *backgroundRepo) Get(ctx context.Context, id string) (*model.Background, error) {
	b := &model.Background{}
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx,
			`SELECT id, name, category, file_path, is_active, created_at FROM chat_backgrounds WHERE id=$1`, id,
		).Scan(&b.ID, &b.Name, &b.Category, &b.FilePath, &b.IsActive, &ScanTime{T: &b.CreatedAt})
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT id, name, category, file_path, is_active, created_at FROM chat_backgrounds WHERE id=?`, id,
		).Scan(&b.ID, &b.Name, &b.Category, &b.FilePath, &b.IsActive, &ScanTime{T: &b.CreatedAt})
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	return b, nil
}

func (r *backgroundRepo) SetUserBG(ctx context.Context, userID, backgroundID, room string) error {
	var err error
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx, `
			INSERT INTO user_backgrounds (user_id, background_id, room)
			VALUES ($1,$2,$3)
			ON CONFLICT (user_id, room) DO UPDATE SET background_id=$2, applied_at=now()`,
			userID, backgroundID, room)
	} else if r.dialect == "sqlite" {
		_, err = r.db.ExecContext(ctx, `
			INSERT INTO user_backgrounds (user_id, background_id, room)
			VALUES (?,?,?)
			ON CONFLICT(user_id, room) DO UPDATE SET
				background_id=excluded.background_id, applied_at=CURRENT_TIMESTAMP`,
			userID, backgroundID, room)
	} else {
		_, err = r.db.ExecContext(ctx, `
			INSERT INTO user_backgrounds (user_id, background_id, room)
			VALUES (?,?,?)
			ON DUPLICATE KEY UPDATE background_id=VALUES(background_id), applied_at=now()`,
			userID, backgroundID, room)
	}
	return wrapErr(err)
}

// ClearUserBG — foydalanuvchining xona uchun fon tanlovini olib tashlaydi.
func (r *backgroundRepo) ClearUserBG(ctx context.Context, userID, room string) error {
	var err error
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx,
			`DELETE FROM user_backgrounds WHERE user_id=$1 AND room=$2`, userID, room)
	} else {
		_, err = r.db.ExecContext(ctx,
			`DELETE FROM user_backgrounds WHERE user_id=? AND room=?`, userID, room)
	}
	return wrapErr(err)
}

func (r *backgroundRepo) UserBGByRoom(ctx context.Context, userID, room string) (string, error) {
	var id string
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx,
			`SELECT background_id FROM user_backgrounds WHERE user_id=$1 AND room=$2`, userID, room,
		).Scan(&id)
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT background_id FROM user_backgrounds WHERE user_id=? AND room=?`, userID, room,
		).Scan(&id)
	}
	if err != nil {
		return "", wrapErr(err)
	}
	return id, nil
}
