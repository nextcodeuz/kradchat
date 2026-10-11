package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"kradchat/internal/model"
)

func (r *messageRepo) Insert(ctx context.Context, m *model.Message) error {
	if r.dialect == "postgres" {
		return wrapErr(r.db.QueryRowContext(ctx, `
			INSERT INTO messages (user_id, room, content, msg_type, file_url, preview_url,
			       file_size, client_msg_id, reply_to, mentions, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			RETURNING id`,
			m.UserID, m.Room, m.Content, m.Type, m.FileURL, m.PreviewURL,
			m.FileSize, m.ClientMsgID, nullableInt64(m.ReplyTo), pgUUIDArray(m.Mentions),
			m.CreatedAt,
		).Scan(&m.ID))
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO messages (user_id, room, content, msg_type, file_url, preview_url,
		       file_size, client_msg_id, reply_to, mentions, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		m.UserID, m.Room, m.Content, m.Type, m.FileURL, m.PreviewURL,
		m.FileSize, m.ClientMsgID, nullableInt64(m.ReplyTo), mysqlJSONArray(m.Mentions), m.CreatedAt,
	)
	if err != nil {
		return wrapErr(err)
	}
	// Non-postgres'da ID ni qaytaramiz (ack/retry yo'li ID=0 bo'lmasligi uchun).
	if id, ierr := res.LastInsertId(); ierr == nil && id > 0 {
		m.ID = id
	} else if m.ClientMsgID != "" {
		var found int64
		if qerr := r.db.QueryRowContext(ctx,
			`SELECT id FROM messages WHERE client_msg_id=?`, m.ClientMsgID,
		).Scan(&found); qerr == nil && found > 0 {
			m.ID = found
		}
	}
	return nil
}

func (r *messageRepo) BulkInsert(ctx context.Context, msgs []*model.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, m := range msgs {
		if r.dialect == "postgres" {
			err = tx.QueryRowContext(ctx, `
				INSERT INTO messages (user_id, room, content, msg_type, file_url, preview_url,
				       file_size, client_msg_id, reply_to, mentions, created_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
				m.UserID, m.Room, m.Content, m.Type, m.FileURL, m.PreviewURL,
				m.FileSize, m.ClientMsgID, nullableInt64(m.ReplyTo), pgUUIDArray(m.Mentions), m.CreatedAt,
			).Scan(&m.ID)
		} else {
			res, e := tx.ExecContext(ctx, `
				INSERT INTO messages (user_id, room, content, msg_type, file_url, preview_url,
				       file_size, client_msg_id, reply_to, mentions, created_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
				m.UserID, m.Room, m.Content, m.Type, m.FileURL, m.PreviewURL,
				m.FileSize, m.ClientMsgID, nullableInt64(m.ReplyTo), mysqlJSONArray(m.Mentions), m.CreatedAt,
			)
			if e != nil {
				err = e
			} else if id, ie := res.LastInsertId(); ie == nil {
				m.ID = id
			}
		}
		if err != nil {
			return wrapErr(err)
		}
	}
	return wrapErr(tx.Commit())
}

func (r *messageRepo) ByClientID(ctx context.Context, clientMsgID string) (*model.Message, error) {
	var m model.Message
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx, `
			SELECT m.id, m.user_id, u.username, u.display_name, u.avatar_url,
			       m.room, m.content, m.msg_type, m.file_url, m.preview_url,
			       m.file_size, m.client_msg_id, COALESCE(m.reply_to, 0), m.created_at,
			       m.mentions, m.deleted_at
			FROM messages m JOIN users u ON u.id = m.user_id
			WHERE m.client_msg_id = $1`, clientMsgID,
		).Scan(&m.ID, &m.UserID, &m.Username, &m.DisplayName, &m.AvatarURL,
			&m.Room, &m.Content, &m.Type, &m.FileURL, &m.PreviewURL,
			&m.FileSize, &m.ClientMsgID, &m.ReplyTo, &ScanTime{T: &m.CreatedAt}, &m.Mentions,
			&ScanTime{T: &m.DeletedAt})
	} else {
		var mentions string
		err = r.db.QueryRowContext(ctx, `
			SELECT m.id, m.user_id, u.username, u.display_name, u.avatar_url,
			       m.room, m.content, m.msg_type, m.file_url, m.preview_url,
			       m.file_size, m.client_msg_id, COALESCE(m.reply_to, 0), m.created_at,
			       CAST(m.mentions AS CHAR), m.deleted_at
			FROM messages m JOIN users u ON u.id = m.user_id
			WHERE m.client_msg_id = ?`, clientMsgID,
		).Scan(&m.ID, &m.UserID, &m.Username, &m.DisplayName, &m.AvatarURL,
			&m.Room, &m.Content, &m.Type, &m.FileURL, &m.PreviewURL,
			&m.FileSize, &m.ClientMsgID, &m.ReplyTo, &ScanTime{T: &m.CreatedAt}, &mentions,
			&ScanTime{T: &m.DeletedAt})
		m.Mentions = unmarshalMentions(mentions)
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	return &m, nil
}

// ByID — xabarni ID bo'yicha qaytaradi (reply uchun original xabarni topish).
func (r *messageRepo) ByID(ctx context.Context, id int64) (*model.Message, error) {
	var m model.Message
	var err error
	mentionsCol := "m.mentions"
	if r.dialect != "postgres" {
		mentionsCol = "CAST(m.mentions AS CHAR)"
	}
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx, `
			SELECT m.id, m.user_id, u.username, u.display_name, u.avatar_url,
			       m.room, m.content, m.msg_type, m.file_url, m.preview_url,
			       m.file_size, m.client_msg_id, COALESCE(m.reply_to, 0), m.created_at,
			       `+mentionsCol+`, m.deleted_at
			FROM messages m JOIN users u ON u.id = m.user_id
			WHERE m.id = $1 AND m.deleted_at IS NULL`, id,
		).Scan(&m.ID, &m.UserID, &m.Username, &m.DisplayName, &m.AvatarURL,
			&m.Room, &m.Content, &m.Type, &m.FileURL, &m.PreviewURL,
			&m.FileSize, &m.ClientMsgID, &m.ReplyTo, &ScanTime{T: &m.CreatedAt}, &m.Mentions,
			&ScanTime{T: &m.DeletedAt})
	} else {
		var mentions string
		err = r.db.QueryRowContext(ctx, `
			SELECT m.id, m.user_id, u.username, u.display_name, u.avatar_url,
			       m.room, m.content, m.msg_type, m.file_url, m.preview_url,
			       m.file_size, m.client_msg_id, COALESCE(m.reply_to, 0), m.created_at,
			       `+mentionsCol+`, m.deleted_at
			FROM messages m JOIN users u ON u.id = m.user_id
			WHERE m.id = ? AND m.deleted_at IS NULL`, id,
		).Scan(&m.ID, &m.UserID, &m.Username, &m.DisplayName, &m.AvatarURL,
			&m.Room, &m.Content, &m.Type, &m.FileURL, &m.PreviewURL,
			&m.FileSize, &m.ClientMsgID, &m.ReplyTo, &ScanTime{T: &m.CreatedAt}, &mentions,
			&ScanTime{T: &m.DeletedAt})
		m.Mentions = unmarshalMentions(mentions)
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	return &m, nil
}

// ByIDs — bir nechta xabarni BITTA so'rovda qaytaradi (tarix reply N+1 oldi:
// 50 reply'li tarix 50 roundtrip o'rniga 1 so'rov). O'chirilganlar qaytmaydi.
func (r *messageRepo) ByIDs(ctx context.Context, ids []int64) (map[int64]*model.Message, error) {
	out := make(map[int64]*model.Message)
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 100 {
		ids = ids[:100]
	}
	mentionsCol := "m.mentions"
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		if r.dialect == "postgres" {
			ph[i] = fmt.Sprintf("$%d", i+1)
		} else {
			mentionsCol = "CAST(m.mentions AS CHAR)"
			ph[i] = "?"
		}
		args[i] = id
	}
	q := `SELECT m.id, m.user_id, u.username, u.display_name, u.avatar_url,
		m.room, m.content, m.msg_type, m.file_url, m.preview_url,
		m.file_size, m.client_msg_id, COALESCE(m.reply_to, 0), m.created_at,
		` + mentionsCol + `, m.deleted_at
		FROM messages m JOIN users u ON u.id = m.user_id
		WHERE m.id IN (` + strings.Join(ph, ",") + `) AND m.deleted_at IS NULL`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var m model.Message
		if r.dialect == "postgres" {
			err = rows.Scan(&m.ID, &m.UserID, &m.Username, &m.DisplayName, &m.AvatarURL,
				&m.Room, &m.Content, &m.Type, &m.FileURL, &m.PreviewURL,
				&m.FileSize, &m.ClientMsgID, &m.ReplyTo, &ScanTime{T: &m.CreatedAt}, &m.Mentions,
				&ScanTime{T: &m.DeletedAt})
		} else {
			var mentions string
			err = rows.Scan(&m.ID, &m.UserID, &m.Username, &m.DisplayName, &m.AvatarURL,
				&m.Room, &m.Content, &m.Type, &m.FileURL, &m.PreviewURL,
				&m.FileSize, &m.ClientMsgID, &m.ReplyTo, &ScanTime{T: &m.CreatedAt}, &mentions,
				&ScanTime{T: &m.DeletedAt})
			m.Mentions = unmarshalMentions(mentions)
		}
		if err != nil {
			return nil, wrapErr(err)
		}
		out[m.ID] = &m
	}
	return out, wrapErr(rows.Err())
}

func (r *messageRepo) History(ctx context.Context, room string, before int64, limit int) ([]*model.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	mentionsCol := "m.mentions"
	if r.dialect != "postgres" {
		mentionsCol = "CAST(m.mentions AS CHAR)"
	}
	q := `SELECT m.id, m.user_id, u.username, u.display_name, u.avatar_url,
	             m.room, m.content, m.msg_type, m.file_url, m.preview_url,
	             m.file_size, m.client_msg_id, COALESCE(m.reply_to, 0), m.created_at,
	             ` + mentionsCol + `, m.deleted_at
	      FROM messages m JOIN users u ON u.id = m.user_id
	      WHERE m.room = ? AND m.deleted_at IS NULL`
	args := []any{room}
	if before > 0 {
		q += ` AND m.id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY m.id DESC LIMIT ?`
	args = append(args, limit)

	if r.dialect == "postgres" {
		q = mysqlToPostgres(q)
	}

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	var out []*model.Message
	for rows.Next() {
		m := &model.Message{}
		if r.dialect == "postgres" {
			err = rows.Scan(&m.ID, &m.UserID, &m.Username, &m.DisplayName, &m.AvatarURL,
				&m.Room, &m.Content, &m.Type, &m.FileURL, &m.PreviewURL,
				&m.FileSize, &m.ClientMsgID, &m.ReplyTo, &ScanTime{T: &m.CreatedAt}, &m.Mentions,
				&ScanTime{T: &m.DeletedAt})
		} else {
			var mentions string
			err = rows.Scan(&m.ID, &m.UserID, &m.Username, &m.DisplayName, &m.AvatarURL,
				&m.Room, &m.Content, &m.Type, &m.FileURL, &m.PreviewURL,
				&m.FileSize, &m.ClientMsgID, &m.ReplyTo, &ScanTime{T: &m.CreatedAt}, &mentions,
				&ScanTime{T: &m.DeletedAt})
			m.Mentions = unmarshalMentions(mentions)
		}
		if err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapErr(err)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ConversationItem — DM suhbatining oxirgi xabari (Shaxsiy ro'yxati uchun).
type ConversationItem struct {
	Room    string
	PeerID  string
	Content string
	Type    string
	Time    time.Time
}

// Conversations — foydalanuvchi ishtirok etgan barcha DM xonalarini oxirgi
// xabari bilan qaytaradi. Xona nomi dm:<idA>:<idB> bo'lgani uchun ishtirokchilik
// "dm:<me>:%" yoki "dm:%:<me>" LIKE patternlari bilan aniqlanadi.
func (r *messageRepo) Conversations(ctx context.Context, userID string) ([]ConversationItem, error) {
	q := `SELECT m.room, m.content, m.msg_type, m.created_at
	      FROM messages m
	      JOIN (SELECT room, MAX(id) AS max_id
	            FROM messages
	            WHERE (room LIKE ? OR room LIKE ?) AND deleted_at IS NULL
	            GROUP BY room) t ON m.id = t.max_id
	      WHERE m.deleted_at IS NULL
	      ORDER BY m.created_at DESC LIMIT 100`
	if r.dialect == "postgres" {
		q = mysqlToPostgres(q)
	}
	rows, err := r.db.QueryContext(ctx, q, "dm:"+userID+":%", "dm:%:"+userID)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer rows.Close()

	var out []ConversationItem
	for rows.Next() {
		var it ConversationItem
		if err := rows.Scan(&it.Room, &it.Content, &it.Type, &ScanTime{T: &it.Time}); err != nil {
			return nil, wrapErr(err)
		}
		out = append(out, it)
	}
	return out, wrapErr(rows.Err())
}

// MarkDeleted — xabarni o'chirilgan deb belgilaydi (deleted_at o'rnatiladi).
// Content o'chirilmaydi, chunki history'dan filtrlanadi; lekin client tomonda
// realtime "deleted" xabari orqali yashiriladi.
func (r *messageRepo) MarkDeleted(ctx context.Context, id int64) error {
	if r.dialect == "postgres" {
		res, err := r.db.ExecContext(ctx, `UPDATE messages SET deleted_at = now() WHERE id=$1 AND deleted_at IS NULL`, id)
		if err != nil {
			return wrapErr(err)
		}
		if ra, rerr := res.RowsAffected(); rerr == nil && ra == 0 {
			return ErrNotFound
		}
		return nil
	}
	res, err := r.db.ExecContext(ctx, `UPDATE messages SET deleted_at = CURRENT_TIMESTAMP WHERE id=? AND deleted_at IS NULL`, id)
	if err != nil {
		return wrapErr(err)
	}
	if ra, rerr := res.RowsAffected(); rerr == nil && ra == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateContentByFileURL — o'z fayl xabarlarining ko'rinadigan nomini
// yangilaydi (rename oqimi; mos xabar bo'lmasa ham xato emas).
func (r *messageRepo) UpdateContentByFileURL(ctx context.Context, ownerID, fileURL, content string) error {
	if r.dialect == "postgres" {
		_, err := r.db.ExecContext(ctx,
			`UPDATE messages SET content=$1 WHERE user_id=$2 AND file_url=$3 AND deleted_at IS NULL`,
			content, ownerID, fileURL)
		return wrapErr(err)
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE messages SET content=? WHERE user_id=? AND file_url=? AND deleted_at IS NULL`,
		content, ownerID, fileURL)
	return wrapErr(err)
}

// OwnsFileURL — userning o'chirilmagan xabari shu faylga havolami
// (legacy fayllarni claim qilish uchun egalik isboti).
func (r *messageRepo) OwnsFileURL(ctx context.Context, ownerID, fileURL string) (bool, error) {
	var one int
	var err error
	if r.dialect == "postgres" {
		err = r.db.QueryRowContext(ctx,
			`SELECT 1 FROM messages WHERE user_id=$1 AND file_url=$2 AND deleted_at IS NULL LIMIT 1`,
			ownerID, fileURL).Scan(&one)
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT 1 FROM messages WHERE user_id=? AND file_url=? AND deleted_at IS NULL LIMIT 1`,
			ownerID, fileURL).Scan(&one)
	}
	if err != nil {
		return false, nil
	}
	return one == 1, nil
}

func mysqlToPostgres(q string) string {
	var sb strings.Builder
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			fmt.Fprintf(&sb, "$%d", n)
		} else {
			sb.WriteByte(q[i])
		}
	}
	return sb.String()
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func pgUUIDArray(ids []string) any {
	if len(ids) == 0 {
		return "{}"
	}
	var sb strings.Builder
	sb.WriteString("{")
	for i, id := range ids {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%q", id))
	}
	sb.WriteString("}")
	return sb.String()
}

func mysqlJSONArray(ids []string) any {
	if len(ids) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

func unmarshalMentions(s string) []string {
	var ids []string
	if s == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return nil
	}
	return ids
}
