package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

const postgresSchema = `
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(50) UNIQUE NOT NULL CHECK (char_length(username) BETWEEN 3 AND 50),
    display_name VARCHAR(100) NOT NULL,
    password_hash VARCHAR(255) NOT NULL CHECK (password_hash <> ''),
    avatar_url TEXT NOT NULL DEFAULT '',
    hide_username BOOLEAN NOT NULL DEFAULT false,
    show_last_seen BOOLEAN NOT NULL DEFAULT true,
    show_online_status BOOLEAN NOT NULL DEFAULT true,
    notifications_enabled BOOLEAN NOT NULL DEFAULT true,
    verified BOOLEAN NOT NULL DEFAULT false,
    twofa_enabled BOOLEAN NOT NULL DEFAULT false,
    twofa_secret TEXT NOT NULL DEFAULT '',
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Username har doim kichik harfda saqlanadi, lekin to'g'ridan-to'g'ri
-- DB yozuvlari ("Bob" va "bob") bilan case-dublikat paydo bo'lmasligi uchun.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_ci ON users (lower(username));

CREATE TABLE IF NOT EXISTS messages (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room VARCHAR(64) NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    msg_type VARCHAR(16) NOT NULL DEFAULT 'text',
    file_url TEXT NOT NULL DEFAULT '',
    preview_url TEXT NOT NULL DEFAULT '',
    file_size BIGINT NOT NULL DEFAULT 0,
    client_msg_id UUID NOT NULL UNIQUE,
    reply_to BIGINT NULL REFERENCES messages(id) ON DELETE SET NULL,
    mentions UUID[] NOT NULL DEFAULT '{}',
    deleted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_messages_room_time ON messages(room, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_user ON messages(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_room_id ON messages(room, id DESC);

CREATE TABLE IF NOT EXISTS blocks (
    blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id)
);
CREATE INDEX IF NOT EXISTS idx_blocks_blocked ON blocks(blocked_id);

CREATE TABLE IF NOT EXISTS devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    device_type VARCHAR(20) NOT NULL DEFAULT 'desktop',
    browser VARCHAR(50) NOT NULL DEFAULT '',
    os VARCHAR(50) NOT NULL DEFAULT '',
    fingerprint VARCHAR(128) NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);
CREATE INDEX IF NOT EXISTS idx_devices_fp ON devices(fingerprint);

CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id UUID NULL REFERENCES devices(id) ON DELETE SET NULL,
    refresh_hash VARCHAR(64) NOT NULL,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_refresh ON sessions(refresh_hash);

CREATE TABLE IF NOT EXISTS password_change_log (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_pcl_user ON password_change_log(user_id);

CREATE TABLE IF NOT EXISTS chat_backgrounds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    category VARCHAR(50) NOT NULL DEFAULT 'default',
    file_path TEXT NOT NULL UNIQUE,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_backgrounds (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    background_id UUID NOT NULL REFERENCES chat_backgrounds(id) ON DELETE CASCADE,
    room VARCHAR(64) NOT NULL DEFAULT 'global',
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, room)
);

-- uploads — fayl registry: egalik, ro'yxat, o'chirish va orphan GC uchun.
CREATE TABLE IF NOT EXISTS uploads (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    upload_id TEXT NOT NULL DEFAULT '',
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename TEXT NOT NULL DEFAULT '',
    mime VARCHAR(64) NOT NULL DEFAULT '',
    size BIGINT NOT NULL DEFAULT 0,
    url TEXT NOT NULL DEFAULT '',
    preview_url TEXT NOT NULL DEFAULT '',
    kind VARCHAR(16) NOT NULL DEFAULT 'chat',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_uploads_owner ON uploads(owner_id);
CREATE INDEX IF NOT EXISTS idx_uploads_url ON uploads(url);
`

const mysqlSchema = `
CREATE TABLE IF NOT EXISTS users (
    id CHAR(36) PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL CHECK (char_length(username) BETWEEN 3 AND 50),
    display_name VARCHAR(100) NOT NULL,
    password_hash VARCHAR(255) NOT NULL CHECK (password_hash <> ''),
    avatar_url TEXT NOT NULL,
    hide_username TINYINT(1) NOT NULL DEFAULT 0,
    show_last_seen TINYINT(1) NOT NULL DEFAULT 1,
    show_online_status TINYINT(1) NOT NULL DEFAULT 1,
    notifications_enabled TINYINT(1) NOT NULL DEFAULT 1,
    verified TINYINT(1) NOT NULL DEFAULT 0,
    twofa_enabled TINYINT(1) NOT NULL DEFAULT 0,
    twofa_secret TEXT NOT NULL DEFAULT '',
    last_seen TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    room VARCHAR(64) NOT NULL,
    content TEXT NOT NULL,
    msg_type VARCHAR(16) NOT NULL DEFAULT 'text',
    file_url TEXT NOT NULL,
    preview_url TEXT NOT NULL,
    file_size BIGINT NOT NULL DEFAULT 0,
    client_msg_id VARCHAR(64) NOT NULL UNIQUE,
    reply_to BIGINT NULL,
    mentions TEXT NOT NULL,
    deleted_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_messages_room_time (room, created_at DESC),
    INDEX idx_messages_user (user_id),
    INDEX idx_messages_room_id (room, id DESC),
    CONSTRAINT fk_messages_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_messages_reply FOREIGN KEY (reply_to) REFERENCES messages(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS blocks (
    blocker_id CHAR(36) NOT NULL,
    blocked_id CHAR(36) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (blocker_id, blocked_id),
    INDEX idx_blocks_blocked (blocked_id),
    CONSTRAINT fk_blocks_blocker FOREIGN KEY (blocker_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_blocks_blocked FOREIGN KEY (blocked_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS devices (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL,
    device_type VARCHAR(20) NOT NULL DEFAULT 'desktop',
    browser VARCHAR(50) NOT NULL DEFAULT '',
    os VARCHAR(50) NOT NULL DEFAULT '',
    fingerprint VARCHAR(128) NOT NULL DEFAULT '',
    is_active TINYINT(1) NOT NULL DEFAULT 1,
    last_seen TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_devices_user (user_id),
    INDEX idx_devices_fp (fingerprint),
    CONSTRAINT fk_devices_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sessions (
    id CHAR(36) PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    device_id CHAR(36) NULL,
    refresh_hash VARCHAR(64) NOT NULL,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_sessions_user (user_id),
    INDEX idx_sessions_refresh (refresh_hash),
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_sessions_device FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS password_change_log (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id CHAR(36) NOT NULL,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_pcl_user (user_id),
    CONSTRAINT fk_pcl_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS chat_backgrounds (
    id CHAR(36) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    category VARCHAR(50) NOT NULL DEFAULT 'default',
    file_path TEXT NOT NULL UNIQUE,
    is_active TINYINT(1) NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS user_backgrounds (
    user_id CHAR(36) NOT NULL,
    background_id CHAR(36) NOT NULL,
    room VARCHAR(64) NOT NULL DEFAULT 'global',
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, room),
    CONSTRAINT fk_ub_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_ub_bg FOREIGN KEY (background_id) REFERENCES chat_backgrounds(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS uploads (
    id CHAR(36) PRIMARY KEY,
    upload_id VARCHAR(64) NOT NULL DEFAULT '',
    owner_id CHAR(36) NOT NULL,
    filename VARCHAR(255) NOT NULL DEFAULT '',
    mime VARCHAR(64) NOT NULL DEFAULT '',
    size BIGINT NOT NULL DEFAULT 0,
    url TEXT NOT NULL,
    preview_url TEXT NOT NULL,
    kind VARCHAR(16) NOT NULL DEFAULT 'chat',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_uploads_owner (owner_id),
    INDEX idx_uploads_url (url(191)),
    CONSTRAINT fk_uploads_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE CHECK (length(username) BETWEEN 3 AND 50),
    display_name TEXT NOT NULL,
    password_hash TEXT NOT NULL CHECK (length(password_hash) > 0),
    avatar_url TEXT NOT NULL DEFAULT '',
    hide_username INTEGER NOT NULL DEFAULT 0,
    show_last_seen INTEGER NOT NULL DEFAULT 1,
    show_online_status INTEGER NOT NULL DEFAULT 1,
    notifications_enabled INTEGER NOT NULL DEFAULT 1,
    verified INTEGER NOT NULL DEFAULT 0,
    twofa_enabled INTEGER NOT NULL DEFAULT 0,
    twofa_secret TEXT NOT NULL DEFAULT '',
    last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_ci ON users (lower(username));

CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    msg_type TEXT NOT NULL DEFAULT 'text',
    file_url TEXT NOT NULL DEFAULT '',
    preview_url TEXT NOT NULL DEFAULT '',
    file_size INTEGER NOT NULL DEFAULT 0,
    client_msg_id TEXT NOT NULL UNIQUE,
    reply_to INTEGER NULL REFERENCES messages(id) ON DELETE SET NULL,
    mentions TEXT NOT NULL DEFAULT '[]',
    deleted_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_messages_room_time ON messages(room, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_user ON messages(user_id);
-- History ORDER BY id DESC bilan paginatsiya qiladi; tezlik uchun room+id
-- indeksi. (Migrate har startda ishlaydi: IF NOT EXISTS.)
CREATE INDEX IF NOT EXISTS idx_messages_room_id ON messages(room, id DESC);

CREATE TABLE IF NOT EXISTS blocks (
    blocker_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (blocker_id, blocked_id)
);
CREATE INDEX IF NOT EXISTS idx_blocks_blocked ON blocks(blocked_id);

CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    device_type TEXT NOT NULL DEFAULT 'desktop',
    browser TEXT NOT NULL DEFAULT '',
    os TEXT NOT NULL DEFAULT '',
    fingerprint TEXT NOT NULL DEFAULT '',
    is_active INTEGER NOT NULL DEFAULT 1,
    last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);
CREATE INDEX IF NOT EXISTS idx_devices_fp ON devices(fingerprint);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id TEXT NULL REFERENCES devices(id) ON DELETE SET NULL,
    refresh_hash TEXT NOT NULL,
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_refresh ON sessions(refresh_hash);

CREATE TABLE IF NOT EXISTS password_change_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_pcl_user ON password_change_log(user_id);

CREATE TABLE IF NOT EXISTS chat_backgrounds (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'default',
    file_path TEXT NOT NULL UNIQUE,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_backgrounds (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    background_id TEXT NOT NULL REFERENCES chat_backgrounds(id) ON DELETE CASCADE,
    room TEXT NOT NULL DEFAULT 'global',
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, room)
);

CREATE TABLE IF NOT EXISTS uploads (
    id TEXT PRIMARY KEY,
    upload_id TEXT NOT NULL DEFAULT '',
    owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename TEXT NOT NULL DEFAULT '',
    mime TEXT NOT NULL DEFAULT '',
    size INTEGER NOT NULL DEFAULT 0,
    url TEXT NOT NULL DEFAULT '',
    preview_url TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'chat',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_uploads_owner ON uploads(owner_id);
CREATE INDEX IF NOT EXISTS idx_uploads_url ON uploads(url);
`

func Migrate(ctx context.Context, db *sql.DB, dialect string) error {
	var schema string
	switch dialect {
	case "postgres":
		schema = postgresSchema
	case "mysql":
		schema = mysqlSchema
	case "sqlite":
		schema = sqliteSchema
	default:
		return fmt.Errorf("unknown dialect %q", dialect)
	}

	for _, stmt := range splitStatements(schema) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate statement failed: %w\nstmt: %s", err, strings.TrimSpace(stmt)[:min(160, len(strings.TrimSpace(stmt)))])
		}
	}
	slog.Info("migrations applied", "dialect", dialect)
	return ensureColumns(ctx, db, dialect)
}

// ensureColumns — mavjud (eski) DB'larga yangi kolonnalarni qo'shadi.
// CREATE TABLE IF NOT EXISTS yangi jadval yaratmaydi, shuning uchun qo'shilgan
// kolonnalar eski bazalarga ALTER TABLE orqali kiritiladi.
func ensureColumns(ctx context.Context, db *sql.DB, dialect string) error {
	added := []string{}

	if !columnExists(ctx, db, dialect, "users", "verified") {
		var stmt string
		switch dialect {
		case "postgres":
			stmt = `ALTER TABLE users ADD COLUMN verified BOOLEAN NOT NULL DEFAULT false`
		case "mysql":
			stmt = `ALTER TABLE users ADD COLUMN verified TINYINT(1) NOT NULL DEFAULT 0`
		default:
			stmt = `ALTER TABLE users ADD COLUMN verified INTEGER NOT NULL DEFAULT 0`
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("add users.verified: %w", err)
		}
		added = append(added, "users.verified")
	}

	if !columnExists(ctx, db, dialect, "messages", "deleted_at") {
		var stmt string
		switch dialect {
		case "postgres":
			stmt = `ALTER TABLE messages ADD COLUMN deleted_at TIMESTAMPTZ NULL`
		case "mysql":
			stmt = `ALTER TABLE messages ADD COLUMN deleted_at TIMESTAMP NULL`
		default:
			stmt = `ALTER TABLE messages ADD COLUMN deleted_at DATETIME NULL`
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("add messages.deleted_at: %w", err)
		}
		added = append(added, "messages.deleted_at")
	}

	if len(added) > 0 {
		slog.Info("schema columns added", "columns", strings.Join(added, ", "))
	}
	return nil
}

// columnExists — jadvalda kolonna mavjudligini tekshiradi.
func columnExists(ctx context.Context, db *sql.DB, dialect, table, column string) bool {
	var q string
	var args []any
	switch dialect {
	case "postgres":
		q = `SELECT column_name FROM information_schema.columns WHERE table_name=$1 AND column_name=$2`
		args = []any{table, column}
	case "mysql":
		q = `SELECT column_name FROM information_schema.columns WHERE table_name=? AND column_name=?`
		args = []any{table, column}
	default:
		rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
		if err != nil {
			return false
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt any
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				return false
			}
			if name == column {
				return true
			}
		}
		return false
	}
	var name string
	err := db.QueryRowContext(ctx, q, args...).Scan(&name)
	return err == nil
}

func splitStatements(schema string) []string {
	var stmts []string
	var cur strings.Builder
	for _, line := range strings.Split(schema, "\n") {
		cur.WriteString(line)
		cur.WriteString("\n")
		if strings.HasSuffix(strings.TrimSpace(line), ";") {
			stmts = append(stmts, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 && strings.TrimSpace(cur.String()) != "" {
		stmts = append(stmts, cur.String())
	}
	return stmts
}
