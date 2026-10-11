package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/model"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrBadID    = errors.New("bad id")
	// errDecryption — shifrlangan secret ochilmadi (buzilgan yozuv yoki
	// almashgan kalit). Fail-closed: chaqiruvchi invalid deb qaraydi.
	errDecryption = errors.New("secret decryption failed")
)

type UserRepo interface {
	Create(ctx context.Context, u *model.User) error
	ByUsername(ctx context.Context, username string) (*model.User, error)
	ByID(ctx context.Context, id string) (*model.User, error)
	Update(ctx context.Context, u *model.User) error
	UpdatePassword(ctx context.Context, userID, hash string) error
	SetLastSeen(ctx context.Context, userID string, t time.Time) error
	SetVerified(ctx context.Context, userID string, verified bool) error
}

type MessageRepo interface {
	Insert(ctx context.Context, m *model.Message) error
	BulkInsert(ctx context.Context, msgs []*model.Message) error
	ByClientID(ctx context.Context, clientMsgID string) (*model.Message, error)
	ByID(ctx context.Context, id int64) (*model.Message, error)
	// ByIDs — bir nechta xabarni BITTA so'rovda (tarix reply N+1 oldi).
	// O'chirilganlar qaytmaydi (topilmagan → ReplyDeleted).
	ByIDs(ctx context.Context, ids []int64) (map[int64]*model.Message, error)
	History(ctx context.Context, room string, before int64, limit int) ([]*model.Message, error)
	Conversations(ctx context.Context, userID string) ([]ConversationItem, error)
	MarkDeleted(ctx context.Context, id int64) error
	// UpdateContentByFileURL — o'z fayl xabarlarining ko'rinadigan nomini
	// yangilaydi (rename oqimi; 0 qator ham ok).
	UpdateContentByFileURL(ctx context.Context, ownerID, fileURL, content string) error
	// OwnsFileURL — userning o'chiril-magan xabari shu faylga havolami
	// (legacy fayllarni claim qilish uchun egalik isboti).
	OwnsFileURL(ctx context.Context, ownerID, fileURL string) (bool, error)
}

type BlockRepo interface {
	Block(ctx context.Context, blockerID, blockedID string) error
	Unblock(ctx context.Context, blockerID, blockedID string) error
	Blocks(ctx context.Context, blockerID, blockedID string) (bool, error)
	BlocksAny(ctx context.Context, a, b string) (bool, error)
	All(ctx context.Context) ([]BlockPair, error)
}

// BlockPair — bloklash yozuvi (in-memory cache'ni to'ldirish uchun).
type BlockPair struct {
	BlockerID string
	BlockedID string
}

type DeviceRepo interface {
	Create(ctx context.Context, d *model.Device) error
	Update(ctx context.Context, d *model.Device) error
	ByID(ctx context.Context, id string) (*model.Device, error)
	ByUser(ctx context.Context, userID string) ([]*model.Device, error)
	ByFingerprint(ctx context.Context, userID, fingerprint string) (*model.Device, error)
	Deactivate(ctx context.Context, id, userID string) error
	CountActive(ctx context.Context, userID string) (int, error)
}

type SessionRepo interface {
	Create(ctx context.Context, s *model.Session) error
	ByRefreshHash(ctx context.Context, hash string) (*model.Session, error)
	Revoke(ctx context.Context, id string) error
	RevokeAllForUser(ctx context.Context, userID string) error
	RevokeDevice(ctx context.Context, deviceID string) error
}

type TwoFARepo interface {
	SetEnabled(ctx context.Context, userID string, enabled bool, secret string) error
	GetSecret(ctx context.Context, userID string) (string, error)
	LogPasswordChange(ctx context.Context, userID, ip, userAgent string) error
	LatestPasswordChange(ctx context.Context, userID string) (time.Time, error)
}

type BackgroundRepo interface {
	AllActive(ctx context.Context) ([]*model.Background, error)
	ByPath(ctx context.Context, path string) (*model.Background, error)
	Create(ctx context.Context, b *model.Background) error
	Get(ctx context.Context, id string) (*model.Background, error)
	SetUserBG(ctx context.Context, userID, backgroundID, room string) error
	ClearUserBG(ctx context.Context, userID, room string) error
	UserBGByRoom(ctx context.Context, userID, room string) (string, error)
}

// FileRepo — uploads registry: egalik, ro'yxat, o'chirish, orphan GC.
type FileRepo interface {
	CreateFile(ctx context.Context, f *model.FileRecord) error
	FileByID(ctx context.Context, id string) (*model.FileRecord, error)
	FileByURL(ctx context.Context, url string) (*model.FileRecord, error)
	FilesByOwner(ctx context.Context, ownerID string, limit, offset int) ([]*model.FileRecord, error)
	// DeleteFile faqat egasiga ruxsat (id+owner). Qaytaradi: o'chirilgan yozuv
	// (disk tozalash uchun). Topilmasa/begona bo'lsa ErrNotFound.
	DeleteFile(ctx context.Context, id, ownerID string) (*model.FileRecord, error)
	// UpdateFilename — fayl nomini yangilaydi (faqat egasi).
	UpdateFilename(ctx context.Context, id, ownerID, filename string) (*model.FileRecord, error)
	// IsReferenced — fayl hali biror joyda ishlatiladimi (uploads/messages/users).
	// excludeMsgID>0 bo'lsa shu xabar hisobga olinmaydi (o'chirish oqimi).
	IsReferenced(ctx context.Context, url string, excludeMsgID int64) (bool, error)
}

// Repositories — barcha repo interfeyslari agregati.
type Repositories struct {
	Users       UserRepo
	Messages    MessageRepo
	Devices     DeviceRepo
	Sessions    SessionRepo
	TwoFA       TwoFARepo
	Backgrounds BackgroundRepo
	Blocks      BlockRepo
	Files       FileRepo
}

// base — umumiy sql.DB + dialektni olib yuradi.
type base struct {
	db      *sql.DB
	dialect string
	// encKey — TOTP secret shifrlash kaliti (nil bo'lsa shifrlanmaydi).
	encKey *[32]byte
}

// nowExpr — DB-ga mos "hozirgi vaqt" SQL ifodasi.
// SQLite'da now() yo'q — CURRENT_TIMESTAMP ishlatiladi.
func (b *base) nowExpr() string {
	if b.dialect == "sqlite" {
		return "CURRENT_TIMESTAMP"
	}
	return "now()"
}

type userRepo struct{ base }
type messageRepo struct{ base }
type deviceRepo struct{ base }
type sessionRepo struct{ base }
type twoFARepo struct{ base }
type backgroundRepo struct{ base }
type blockRepo struct{ base }
type fileRepo struct{ base }

// New — db va konfiguratsiyadan Repositories quradi.
func New(db *sql.DB, cfg *config.Config) (*Repositories, error) {
	if err := db.PingContext(context.Background()); err != nil {
		return nil, err
	}
	dialect := cfg.Database.DialectName()
	b := base{db: db, dialect: dialect}
	if cfg.JWT.Secret != "" {
		b.encKey = deriveSecretKey(cfg.JWT.Secret)
	}
	return &Repositories{
		Users:       &userRepo{b},
		Messages:    &messageRepo{b},
		Devices:     &deviceRepo{b},
		Sessions:    &sessionRepo{b},
		TwoFA:       &twoFARepo{b},
		Backgrounds: &backgroundRepo{b},
		Blocks:      &blockRepo{b},
		Files:       &fileRepo{b},
	}, nil
}

func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	msg := strings.ToLower(err.Error())
	// Driver xato kodlari + matn (hujumga chidamliroq aniqlash):
	// pg 23505, mysql 1062, sqlite UNIQUE constraint.
	if strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate entry") || strings.Contains(msg, "23505") ||
		strings.Contains(msg, "1062") || strings.Contains(msg, "unique_") ||
		strings.Contains(msg, "already exists") {
		return ErrConflict
	}
	return err
}

// execResult — UPDATE/DELETE 0 qatorga tegsa ErrNotFound qaytaradi.
// Silent no-op (o'zganing ID'siga "muvaffaqiyat") oldini oladi.
func execResult(res sql.Result, err error) error {
	if err != nil {
		return wrapErr(err)
	}
	if n, rerr := res.RowsAffected(); rerr == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}
