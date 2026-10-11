package repository

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// openIsRefDB — IsReferenced uchun minimal schema (uploads/messages/users).
func openIsRefDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	schema := []string{
		`CREATE TABLE uploads (id TEXT PRIMARY KEY, url TEXT, preview_url TEXT)`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY, file_url TEXT, preview_url TEXT, deleted_at TIMESTAMP NULL)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, avatar_url TEXT)`,
	}
	for _, q := range schema {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	return db
}

// TestIsReferencedSQLite — H1 regressiya: sqlite'da placeholder/argument
// mos kelmasa jonli fayl "ishlatilmayapti" deb topilib o'chib ketardi.
func TestIsReferencedSQLite(t *testing.T) {
	db := openIsRefDB(t)
	r := &fileRepo{base{db: db, dialect: "sqlite"}}
	ctx := context.Background()

	if _, err := db.Exec(`INSERT INTO uploads (id, url, preview_url) VALUES ('u1','/static/uploads/2026/a.png','')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages (id, file_url, preview_url, deleted_at) VALUES (5,'/static/uploads/2026/m.png','',NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, avatar_url) VALUES ('u9','/static/uploads/2026/av.png')`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		url  string
		excl int64
		want bool
	}{
		{"uploads-hiti", "/static/uploads/2026/a.png", 0, true},
		{"messages-hiti", "/static/uploads/2026/m.png", 0, true},
		{"exclude-xabar", "/static/uploads/2026/m.png", 5, false},
		{"avatar-hiti", "/static/uploads/2026/av.png", 0, true},
		{"nomalum", "/static/uploads/2026/yoq.png", 0, false},
		{"bosh", "", 0, false},
	}
	for _, c := range cases {
		got, err := r.IsReferenced(ctx, c.url, c.excl)
		if err != nil {
			t.Errorf("%s: err=%v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got=%v want=%v", c.name, got, c.want)
		}
	}
}

// TestByIDsBatch — F8: reply originallar bitta so'rovda; o'chirilgan va
// mavjud bo'lmagan ID'lar xaritada bo'lmaydi.
func TestByIDsBatch(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT, display_name TEXT, avatar_url TEXT)`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY, user_id TEXT, room TEXT, content TEXT, msg_type TEXT, file_url TEXT, preview_url TEXT, file_size INTEGER, client_msg_id TEXT, reply_to INTEGER, mentions TEXT, created_at TIMESTAMP, deleted_at TIMESTAMP NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, display_name, avatar_url) VALUES ('u1','alice','Alice','')`); err != nil {
		t.Fatal(err)
	}
	msgs := []string{
		`INSERT INTO messages (id, user_id, room, content, msg_type, file_url, preview_url, file_size, client_msg_id, mentions, created_at) VALUES (1,'u1','global','bir','text','','',0,'','[]',CURRENT_TIMESTAMP)`,
		`INSERT INTO messages (id, user_id, room, content, msg_type, file_url, preview_url, file_size, client_msg_id, mentions, created_at) VALUES (2,'u1','global','ikki','text','','',0,'','[]',CURRENT_TIMESTAMP)`,
		`INSERT INTO messages (id, user_id, room, content, msg_type, file_url, preview_url, file_size, client_msg_id, reply_to, mentions, created_at) VALUES (3,'u1','global','uch','text','','',0,'',1,'[]',CURRENT_TIMESTAMP)`,
		`INSERT INTO messages (id, user_id, room, content, msg_type, file_url, preview_url, file_size, client_msg_id, mentions, created_at, deleted_at) VALUES (4,'u1','global','tort','text','','',0,'','[]',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
	}
	for _, q := range msgs {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	r := &messageRepo{base{db: db, dialect: "sqlite"}}
	got, err := r.ByIDs(context.Background(), []int64{1, 2, 3, 4, 999})
	if err != nil {
		t.Fatalf("ByIDs: %v", err)
	}
	for _, id := range []int64{1, 2, 3} {
		m, ok := got[id]
		if !ok || m == nil {
			t.Errorf("id=%d topilmadi", id)
			continue
		}
		if m.ID != id {
			t.Errorf("id=%d: noto'g'ri xabar id=%d", id, m.ID)
		}
	}
	if _, ok := got[4]; ok {
		t.Error("o'chirilgan xabar (id=4) qaytmasligi kerak")
	}
	if _, ok := got[999]; ok {
		t.Error("mavjud bo'lmagan id qaytmasligi kerak")
	}
	if got[3].ReplyTo != 1 {
		t.Errorf("reply_to: got=%d want=1", got[3].ReplyTo)
	}
	if empty, err := r.ByIDs(context.Background(), nil); err != nil || len(empty) != 0 {
		t.Errorf("bo'sh kirish: %v err=%v", empty, err)
	}
}
