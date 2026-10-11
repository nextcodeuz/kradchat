package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

type Database struct {
	Driver   string // postgres | mysql | mariadb | sqlite
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	Path     string // sqlite: DB fayl yo'li
	MaxConns int
	SSLMode  string
}

type Server struct {
	Host string
	Port int
}

type JWT struct {
	Secret        string
	AccessExpire  time.Duration
	RefreshExpire time.Duration
	Issuer        string
}

type Upload struct {
	Dir          string
	MaxFileSize  int64
	ChunkSize    int64
	AllowedTypes []string
}

type Security struct {
	RateLimitReq      int
	RateLimitWindow   time.Duration
	WsMsgPerMin       int
	WsMsgBurst        int
	MaxMessageSize    int64
	BcryptCost        int
	PendingSessionTTL time.Duration
	// TrustedProxies — X-Forwarded-For ishonadigan proksi IP/CIDR ro'yxati
	// (vergul bilan). Bo'sh bo'lsa ClientIP RemoteAddr'ni ishlatadi —
	// spoofing XFF orqali rate-limit chetlab o'tish bloklanadi.
	TrustedProxies string
}

type Background struct {
	Dir     string
	Poll    time.Duration
	MaxSize int64
}

type Config struct {
	Database   Database
	Server     Server
	JWT        JWT
	Upload     Upload
	Security   Security
	Background Background
	StaticDir  string
	Env        string
	LogLevel   string
	// VerifiedUsers — startup'da verified (tasdiqlangan) belgilanishi kerak
	// bo'lgan usernamlar ro'yxati (vergul bilan). Default bo'sh — hech kim
	// avtomatik verified bo'lmaydi (xavfli default: "developer,admin" kabi
	// nomlar bilan ro'yxatdan o'tish verified badge berardi).
	VerifiedUsers []string
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = ".env"
	}
	_ = godotenv.Load(path)

	cfg := &Config{
		Database: Database{
			Driver:   getenv("DB_DRIVER", "postgres"),
			Host:     getenv("DB_HOST", "localhost"),
			Port:     getenvInt("DB_PORT", 5432),
			User:     getenv("DB_USER", "kradchat"),
			Password: getenv("DB_PASSWORD", "kradchat"),
			Name:     getenv("DB_NAME", "kradchat"),
			Path:     getenv("DB_PATH", "./kradchat.db"),
			MaxConns: getenvInt("DB_MAX_CONNS", 50),
			SSLMode:  getenv("DB_SSLMODE", "disable"),
		},
		Server: Server{
			Host: getenv("SERVER_HOST", "0.0.0.0"),
			Port: getenvInt("SERVER_PORT", 8080),
		},
		JWT: JWT{
			Secret:        getenv("JWT_SECRET", "kradchat-super-secret-change-me"),
			AccessExpire:  getenvDur("JWT_ACCESS_EXPIRE", 15*time.Minute),
			RefreshExpire: getenvDur("JWT_REFRESH_EXPIRE", 7*24*time.Hour),
			Issuer:        getenv("JWT_ISSUER", "kradchat"),
		},
		Upload: Upload{
			Dir:         getenv("UPLOAD_DIR", "./web/static/uploads"),
			MaxFileSize: getenvInt64("MAX_FILE_SIZE", 50*1024*1024),
			ChunkSize:   getenvInt64("UPLOAD_CHUNK_SIZE", 1024*1024),
			AllowedTypes: []string{
				"image/jpeg", "image/png", "image/gif", "image/webp",
				"video/mp4", "video/webm",
				"application/pdf", "application/zip",
				"text/plain", "text/csv",
				"audio/mpeg", "audio/wav", "audio/ogg",
			},
		},
		Security: Security{
			RateLimitReq:      getenvInt("RATE_LIMIT_REQUESTS", 120),
			RateLimitWindow:   getenvDur("RATE_LIMIT_WINDOW", time.Minute),
			WsMsgPerMin:       getenvInt("WS_MSG_PER_MIN", 60),
			WsMsgBurst:        getenvInt("WS_MSG_BURST", 10),
			MaxMessageSize:    getenvInt64("WS_MAX_MESSAGE_SIZE", 64*1024),
			BcryptCost:        getenvInt("BCRYPT_COST", 12),
			PendingSessionTTL: getenvDur("PENDING_SESSION_TTL", 15*time.Minute),
			TrustedProxies:    getenv("TRUSTED_PROXIES", ""),
		},
		Background: Background{
			Dir:     getenv("BACKGROUND_DIR", "./web/static/backgrounds"),
			Poll:    getenvDur("BACKGROUND_POLL", 30*time.Second),
			MaxSize: getenvInt64("BACKGROUND_MAX_SIZE", 20*1024*1024),
		},
		StaticDir: getenv("STATIC_DIR", "./web/static"),
		Env:       getenv("APP_ENV", "development"),
		LogLevel:  getenv("LOG_LEVEL", "info"),
	}
	cfg.VerifiedUsers = splitList(getenv("VERIFIED_USERS", ""))

	switch cfg.Database.Driver {
	case "postgres", "mysql", "mariadb", "sqlite":
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER %q (use postgres|mysql|mariadb|sqlite)", cfg.Database.Driver)
	}

	if cfg.Database.Port == 0 {
		if cfg.Database.Driver == "postgres" {
			cfg.Database.Port = 5432
		} else if cfg.Database.Driver != "sqlite" {
			cfg.Database.Port = 3306
		}
	}

	if len(cfg.JWT.Secret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 chars (256-bit for HS256)")
	}

	if cfg.JWT.Secret == "kradchat-super-secret-change-me" {
		return nil, fmt.Errorf("JWT_SECRET is still the default value; set a strong random secret")
	}

	if cfg.Security.BcryptCost < 10 || cfg.Security.BcryptCost > 15 {
		return nil, fmt.Errorf("BCRYPT_COST must be between 10 and 15")
	}

	if cfg.Env == "production" && cfg.Database.Driver != "sqlite" && cfg.Database.Password == "kradchat" {
		return nil, fmt.Errorf("DB_PASSWORD is still the default value; set a strong database password in production")
	}

	return cfg, nil
}

func (d Database) DSN() string {
	switch d.Driver {
	case "postgres":
		port := d.Port
		if port == 0 {
			port = 5432
		}
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			d.Host, port, d.User, pgQuote(d.Password), d.Name, d.SSLMode)
	case "mysql", "mariadb":
		mc := mysql.NewConfig()
		mc.Net = "tcp"
		host := d.Host
		port := d.Port
		if port == 0 {
			port = 3306
		}
		mc.Addr = fmt.Sprintf("%s:%d", host, port)
		mc.User = d.User
		mc.Passwd = d.Password
		mc.DBName = d.Name
		mc.ParseTime = true
		mc.Params = map[string]string{
			"charset":   "utf8mb4",
			"collation": "utf8mb4_unicode_ci",
		}
		return mc.FormatDSN()
	case "sqlite":
		p := d.Path
		if p == "" {
			p = "./kradchat.db"
		}
		return fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
			strings.ReplaceAll(p, "\\", "/"))
	default:
		return ""
	}
}

func (d Database) DialectName() string {
	if d.Driver == "mariadb" {
		return "mysql"
	}
	return d.Driver
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// pgQuote — postgres key=value DSN uchun parolni xavfsiz quote'laydi.
func pgQuote(s string) string {
	if s == "" {
		return "''"
	}
	r := strings.ReplaceAll(s, `\`, `\\`)
	r = strings.ReplaceAll(r, `'`, `\'`)
	if strings.ContainsAny(r, " \t'\\") {
		return "'" + r + "'"
	}
	return r
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
		slog.Warn("invalid int env, using default", "key", key, "default", def)
	}
	return def
}

func getenvInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
		slog.Warn("invalid int env, using default", "key", key, "default", def)
	}
	return def
}

func getenvDur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		slog.Warn("invalid duration env, using default", "key", key, "default", def.String())
	}
	return def
}

// splitList — vergul bilan ajratilgan ro'yxatni tozalangan slice'ga aylantiradi.
func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
