package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"kradchat/internal/config"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func Open(cfg *config.Config) (*sql.DB, error) {
	dsn := cfg.Database.DSN()
	driver := cfg.Database.DialectName()

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driver, err)
	}

	db.SetMaxOpenConns(cfg.Database.MaxConns)
	if driver == "sqlite" {
		// SQLite bitta yozuvchi — ortiqcha ulanishlar SQLITE_BUSY keltiradi.
		db.SetMaxOpenConns(10)
	}
	db.SetMaxIdleConns(cfg.Database.MaxConns / 4)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping %s@%s: %w", driver, cfg.Database.Host, err)
	}

	slog.Info("database connected", "driver", driver, "host", cfg.Database.Host, "db", cfg.Database.Name)
	return db, nil
}

// NormalizeError maps driver-specific errors to sentinel domain errors.
type Error struct {
	Kind    string // "duplicate" | "not_found" | "constraint" | "internal"
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s", e.Kind, e.Err.Error())
	}
	return e.Kind
}

func (e *Error) Unwrap() error { return e.Err }

func IsDup(err error) bool {
	return kindOf(err) == "duplicate"
}

func IsNotFound(err error) bool {
	return kindOf(err) == "not_found"
}

func kindOf(err error) string {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return "internal"
}
