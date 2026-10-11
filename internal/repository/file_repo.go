package repository

import (
	"context"
	"database/sql"
	"errors"

	"kradchat/internal/model"

	"github.com/google/uuid"
)

// CreateFile — yuklangan faylni registry'ga yozadi.
func (r *fileRepo) CreateFile(ctx context.Context, f *model.FileRecord) error {
	if f.ID == "" {
		f.ID = uuid.NewString()
	}
	if r.dialect == "postgres" {
		return wrapErr(r.db.QueryRowContext(ctx, `
			INSERT INTO uploads (id, upload_id, owner_id, filename, mime, size, url, preview_url, kind)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING created_at`,
			f.ID, f.UploadID, f.OwnerID, f.Filename, f.Mime, f.Size, f.URL, f.PreviewURL, f.Kind,
		).Scan(&ScanTime{T: &f.CreatedAt}))
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO uploads (id, upload_id, owner_id, filename, mime, size, url, preview_url, kind)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		f.ID, f.UploadID, f.OwnerID, f.Filename, f.Mime, f.Size, f.URL, f.PreviewURL, f.Kind,
	)
	return wrapErr(err)
}

func scanFileRecord(row interface {
	Scan(dest ...any) error
}) (*model.FileRecord, error) {
	f := &model.FileRecord{}
	if err := row.Scan(&f.ID, &f.UploadID, &f.OwnerID, &f.Filename, &f.Mime,
		&f.Size, &f.URL, &f.PreviewURL, &f.Kind, &ScanTime{T: &f.CreatedAt}); err != nil {
		return nil, wrapErr(err)
	}
	return f, nil
}

const fileCols = `id, upload_id, owner_id, filename, mime, size, url, preview_url, kind, created_at`

// FileByID — faylni ID bo'yicha.
func (r *fileRepo) FileByID(ctx context.Context, id string) (*model.FileRecord, error) {
	if r.dialect == "postgres" {
		return scanFileRecord(r.db.QueryRowContext(ctx,
			`SELECT `+fileCols+` FROM uploads WHERE id=$1`, id))
	}
	return scanFileRecord(r.db.QueryRowContext(ctx,
		`SELECT `+fileCols+` FROM uploads WHERE id=?`, id))
}

// FileByURL — faylni URL bo'yicha (egalik tekshiruvi uchun).
func (r *fileRepo) FileByURL(ctx context.Context, url string) (*model.FileRecord, error) {
	if r.dialect == "postgres" {
		return scanFileRecord(r.db.QueryRowContext(ctx,
			`SELECT `+fileCols+` FROM uploads WHERE url=$1 OR preview_url=$1 LIMIT 1`, url))
	}
	return scanFileRecord(r.db.QueryRowContext(ctx,
		`SELECT `+fileCols+` FROM uploads WHERE url=? OR preview_url=? LIMIT 1`, url, url))
}

// FilesByOwner — foydalanuvchining fayllari (Fayllarim ro'yxati).
func (r *fileRepo) FilesByOwner(ctx context.Context, ownerID string, limit, offset int) ([]*model.FileRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	// F9: ulkan offset katta OFFSET skan/sort bilan DB CPU yuklaydi.
	if offset > 10000 {
		offset = 10000
	}
	q := `SELECT ` + fileCols + ` FROM uploads WHERE owner_id=? ORDER BY created_at DESC LIMIT ? OFFSET ?`
	if r.dialect == "postgres" {
		q = `SELECT ` + fileCols + ` FROM uploads WHERE owner_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	}
	rows, err := r.db.QueryContext(ctx, q, ownerID, limit, offset)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()
	var out []*model.FileRecord
	for rows.Next() {
		f := &model.FileRecord{}
		if err := rows.Scan(&f.ID, &f.UploadID, &f.OwnerID, &f.Filename, &f.Mime,
			&f.Size, &f.URL, &f.PreviewURL, &f.Kind, &ScanTime{T: &f.CreatedAt}); err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, f)
	}
	return out, wrapErr(rows.Err())
}

// DeleteFile — faqat egasi o'chira oladi. Qaytaradi: o'chirilgan yozuv.
func (r *fileRepo) DeleteFile(ctx context.Context, id, ownerID string) (*model.FileRecord, error) {
	rec, err := r.FileByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.OwnerID != ownerID {
		return nil, ErrNotFound
	}
	if r.dialect == "postgres" {
		res, err := r.db.ExecContext(ctx, `DELETE FROM uploads WHERE id=$1 AND owner_id=$2`, id, ownerID)
		if err != nil {
			return nil, wrapErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
		return rec, nil
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM uploads WHERE id=? AND owner_id=?`, id, ownerID)
	if err != nil {
		return nil, wrapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return rec, nil
}

// UpdateFilename — fayl nomini yangilaydi (faqat egasi).
func (r *fileRepo) UpdateFilename(ctx context.Context, id, ownerID, filename string) (*model.FileRecord, error) {
	rec, err := r.FileByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.OwnerID != ownerID {
		return nil, ErrNotFound
	}
	if r.dialect == "postgres" {
		_, err = r.db.ExecContext(ctx, `UPDATE uploads SET filename=$1 WHERE id=$2`, filename, id)
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE uploads SET filename=? WHERE id=?`, filename, id)
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	rec.Filename = filename
	return rec, nil
}

// IsReferenced — fayl hali biror joyda ishlatiladimi.
// Tekshiriladi: uploads registry + o'chirilmagan xabarlar + user avatarlari.
func (r *fileRepo) IsReferenced(ctx context.Context, url string, excludeMsgID int64) (bool, error) {
	if url == "" {
		return false, nil
	}
	q1 := `SELECT 1 FROM uploads WHERE url=? OR preview_url=? LIMIT 1`
	q3 := `SELECT 1 FROM users WHERE avatar_url=? LIMIT 1`
	var one int
	if r.dialect == "postgres" {
		q1 = `SELECT 1 FROM uploads WHERE url=$1 OR preview_url=$1 LIMIT 1`
		q3 = `SELECT 1 FROM users WHERE avatar_url=$1 LIMIT 1`
	}
	// sqlite/mysql'da placeholder soni argument soniga mos bo'lishi shart
	// (mos kelmasa driver xato qaytaradi va fayl "ishlatilmayapti" deb topiladi).
	if err := r.db.QueryRowContext(ctx, q1, url, url).Scan(&one); err == nil {
		return true, nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, wrapErr(err)
	}
	q2 := `SELECT 1 FROM messages WHERE (file_url=? OR preview_url=?) AND deleted_at IS NULL AND id<>? LIMIT 1`
	if r.dialect == "postgres" {
		q2 = `SELECT 1 FROM messages WHERE (file_url=$1 OR preview_url=$1) AND deleted_at IS NULL AND id<>$2 LIMIT 1`
	}
	if err := r.db.QueryRowContext(ctx, q2, url, url, excludeMsgID).Scan(&one); err == nil {
		return true, nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, wrapErr(err)
	}
	if err := r.db.QueryRowContext(ctx, q3, url).Scan(&one); err == nil {
		return true, nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, wrapErr(err)
	}
	return false, nil
}
