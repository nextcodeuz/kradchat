package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/model"
	"kradchat/internal/repository"
	"kradchat/internal/security"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
)

var (
	ErrInvalidFile    = errors.New("invalid file type")
	ErrFileTooBig     = errors.New("file too large")
	ErrChunkOrder     = errors.New("chunk out of order")
	ErrBadFilename    = errors.New("invalid filename")
	ErrTooManyUploads = errors.New("too many active uploads")

	maxUploadSessions = 5000
	// maxUploadSessionsPerOwner — bitta akkauntning bir vaqtda ochiq upload
	// sessiyalari (M4: capsiz sessiyalar bilan tmp diskini to'ldirish oldi).
	maxUploadSessionsPerOwner = 5
	// maxChunkBytes — bitta request'da yoziladigan chunk hajmi chegarasi
	// (handler 2MB gacha o'qiydi; shu bilan mos).
	maxChunkBytes = int64(2 * 1024 * 1024)
	// maxAvatarBytes — profil rasmi uchun eng katta hajm.
	maxAvatarBytes = int64(5 * 1024 * 1024)

	// Saqlanadigan rasm uchun piksel chegaralari (M1/M2): hajm limiti (50MB)
	// yaxshi siqiladigan gigapiksel PNG'ni to'xtatmaydi — uni ko'rgan har bir
	// mijoz brauzeri GB'lab RAM yeydi (client-side decompression bomb).
	// Shuning uchun saqlash vaqtida ham header-only o'lcham tekshiriladi.
	maxStoreImageDim     = 12000
	maxStoreImagePixels  = int64(50000000) // ~50 MP
	maxAvatarImageDim    = 4000
	maxAvatarImagePixels = int64(16000000) // ~16 MP
)

// previewSlots — parallel preview dekod semafori (M5): guard'dan o'tgan 30MP
// rasm to'liq dekodda ~120MB RAM yeydi; cheksiz paralellik OOM beradi.
// Bo'sh slot yo'q bo'lsa preview o'tkazib yuboriladi (fayl saqlanadi).
var previewSlots = make(chan struct{}, 2)

// UploadService chunked upload'ni boshqaradi.
// Fayl `uploads/tmp/<upload_id>/NNN.bin` chunklariga bo'linadi,
// oxirgi chunk kelganda merge + tekshiruv + preview.
// Tugallangan har bir fayl `uploads` registry'ga yoziladi (egalik, ro'yxat,
// o'chirish va orphan GC uchun). repo nil bo'lsa registry o'tkazib yuboriladi.
type UploadService struct {
	cfg  *config.Config
	repo *repository.Repositories
	mu   sync.Mutex
	tmp  map[string]*uploadState
}

type uploadState struct {
	ID            string
	OwnerID       string
	Name          string
	Size          int64
	ChunkSize     int64
	Total         int
	Received      int
	ReceivedBytes int64
	Path          string
	Mime          string
	seen          map[int]bool // qabul qilingan chunk indekslari (retry xavfsiz)
	started       time.Time
	merging       bool // oxirgi chunk merge boshlagan (double-merge oldi)
}

type chunkInfo struct {
	UploadID string `json:"upload_id"`
	Index    int    `json:"index"`
	Total    int    `json:"total"`
	Filename string `json:"filename"`
}

func NewUploadService(cfg *config.Config, repo *repository.Repositories) *UploadService {
	return &UploadService{cfg: cfg, repo: repo, tmp: make(map[string]*uploadState)}
}

// StartTmpCleanup — uploads/tmp papkasidagi qarovsiz (1 soatdan eski) fayllarni
// davriy tozalaydi. Crash yoki uzilgan seanslardan qolgan fayllar (in-memory
// sessiya yo'qolgan bo'lsa ham) shu yo'l bilan o'chiriladi.
func (u *UploadService) StartTmpCleanup(ctx context.Context) {
	go func() {
		tick := time.NewTicker(15 * time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				u.cleanupTmpDir()
			}
		}
	}()
}

func (u *UploadService) cleanupTmpDir() {
	tmpRoot := filepath.Join(u.cfg.Upload.Dir, "tmp")
	entries, err := os.ReadDir(tmpRoot)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("upload tmp cleanup", "err", err)
		}
		return
	}
	cutoff := time.Now().Add(-time.Hour)
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, active := u.tmp[e.Name()]; active {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(tmpRoot, e.Name()))
			slog.Info("upload tmp cleanup", "removed", e.Name())
		}
	}
}

// Start — yangi upload seansini boshlaydi (progress uchun).
// ownerID — seans egasi (keyingi chunklar shu foydalanuvchiga bog'lanadi;
// boshqa user boshqasining upload_id'sini ishlatib yozolmaydi).
func (u *UploadService) Start(ownerID, filename string, size int64, totalChunks int) (*uploadState, error) {
	if size > u.cfg.Upload.MaxFileSize {
		return nil, ErrFileTooBig
	}
	if totalChunks < 1 || totalChunks > 2048 {
		return nil, ErrChunkOrder
	}
	id := uuid.NewString()
	dir := filepath.Join(u.cfg.Upload.Dir, "tmp", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	st := &uploadState{
		ID:        id,
		OwnerID:   ownerID,
		Name:      sanitizeFilename(filename),
		Size:      size,
		ChunkSize: u.cfg.Upload.ChunkSize,
		Total:     totalChunks,
		Path:      dir,
		seen:      make(map[int]bool),
		started:   time.Now(),
	}
	u.mu.Lock()
	u.pruneLocked()
	if len(u.tmp) >= maxUploadSessions {
		// Eng eski sessiyani chiqaramiz (tasodifiy emas — adolatli eviction).
		var oldest string
		var oldestT time.Time
		first := true
		for k, st := range u.tmp {
			if first || st.started.Before(oldestT) {
				oldest, oldestT, first = k, st.started, false
			}
		}
		if oldest != "" {
			_ = os.RemoveAll(u.tmp[oldest].Path)
			delete(u.tmp, oldest)
		}
	}
	// Per-owner cap — bitta akkaunt minglab sessiya ochib tmp'ni
	// to'ldira olmaydi (har sessiya 50MB gacha).
	owned := 0
	for _, st := range u.tmp {
		if st.OwnerID != "" && st.OwnerID == ownerID {
			owned++
		}
	}
	if owned >= maxUploadSessionsPerOwner {
		u.mu.Unlock()
		return nil, ErrTooManyUploads
	}
	u.tmp[id] = st
	u.mu.Unlock()
	return st, nil
}

// pruneLocked — 1 soatdan ko'proq faolsiz turgan upload sessiyalarini tozalaydi.
func (u *UploadService) pruneLocked() {
	cutoff := time.Now().Add(-time.Hour)
	for id, st := range u.tmp {
		if st.started.Before(cutoff) {
			_ = os.RemoveAll(st.Path)
			delete(u.tmp, id)
		}
	}
}

// Chunk — bitta chunkni qabul qiladi; oxirgi bo'lsa merge+preview.
// Chunklarni tartibsiz yoki parallel kelishiga bardoshli (mutex himoyasi).
// seen flag faqat diskka muvaffaqiyatli yozilgandan keyin qo'yiladi —
// aks holda retry "qabul qilindi" deb faylsiz qaytib buzuq merge bo'lardi.
func (u *UploadService) Chunk(ctx context.Context, ownerID, id string, index, total int, data []byte) (int, *model.UploadResponse, error) {
	u.mu.Lock()
	st, ok := u.tmp[id]
	if !ok {
		u.mu.Unlock()
		return 0, nil, errors.New("unknown upload")
	}
	if st.OwnerID != "" && ownerID != st.OwnerID {
		u.mu.Unlock()
		return 0, nil, errors.New("unknown upload")
	}
	if index < 0 || index >= st.Total {
		u.mu.Unlock()
		return 0, nil, ErrChunkOrder
	}
	if total != st.Total {
		u.mu.Unlock()
		return 0, nil, ErrChunkOrder
	}
	if int64(len(data)) > maxChunkBytes {
		u.mu.Unlock()
		return 0, nil, ErrChunkOrder
	}
	if st.Received >= st.Total || st.merging {
		u.mu.Unlock()
		return 0, nil, errors.New("upload already complete")
	}
	// Agregat hajm chegarasi: yig'ilib borayotgan baytlar MaxFileSize'dan
	// oshsa sessiya darhol bekor qilinadi (disk va merge-xotira to'yib ketishi
	// oldi — tekshiruv faqat merge'da bo'lsa juda kech bo'lar edi).
	if st.ReceivedBytes+int64(len(data)) > u.cfg.Upload.MaxFileSize {
		delete(u.tmp, id)
		u.mu.Unlock()
		_ = os.RemoveAll(st.Path)
		return 0, nil, ErrFileTooBig
	}
	if st.seen[index] {
		// Retry — chunk allaqachon qabul qilingan; qayta sanamaymiz.
		received := st.Received
		u.mu.Unlock()
		pct := int(float64(received) / float64(total) * 100)
		if pct > 100 {
			pct = 100
		}
		return pct, &model.UploadResponse{UploadID: id, Complete: false}, nil
	}
	chunkPath := filepath.Join(st.Path, fmt.Sprintf("%08d.bin", index))
	u.mu.Unlock()

	// Diskka atomik yozish (tmp + rename) — yarim yozilgan chunk merge'ga
	// tushmaydi. seen flag faqat muvaffaqiyatdan keyin qo'yiladi.
	tmpPath := chunkPath + ".part"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return 0, nil, err
	}
	if err := os.Rename(tmpPath, chunkPath); err != nil {
		_ = os.Remove(tmpPath)
		return 0, nil, err
	}

	u.mu.Lock()
	// Poyga: boshqa goroutine shu index'ni yozib ulgurgan bo'lsa — ikki marta
	// sanamaymiz (ortiqcha fayl shunchaki ustiga yozilgan, zararsiz).
	if !st.seen[index] {
		st.seen[index] = true
		st.Received++
		st.ReceivedBytes += int64(len(data))
	}
	received := st.Received
	if received < total {
		u.mu.Unlock()
		pct := int(float64(received) / float64(total) * 100)
		if pct > 100 {
			pct = 100
		}
		return pct, &model.UploadResponse{UploadID: id, Complete: false}, nil
	}
	// Oxirgi chunk — faqat bitta goroutine merge qiladi.
	if st.merging {
		u.mu.Unlock()
		return 100, &model.UploadResponse{UploadID: id, Complete: false}, nil
	}
	st.merging = true
	u.mu.Unlock()

	pct := 100
	// Merge.
	finalPath, mime, written, err := u.merge(ctx, st)
	if err != nil {
		u.mu.Lock()
		delete(u.tmp, id)
		u.mu.Unlock()
		_ = os.RemoveAll(st.Path)
		return pct, nil, err
	}
	u.mu.Lock()
	delete(u.tmp, id)
	u.mu.Unlock()
	_ = os.RemoveAll(st.Path)

	preview := ""
	if isImageMime(mime) {
		preview, err = makePreview(u.cfg.Upload.Dir, finalPath)
		if err != nil {
			slog.Warn("preview", "err", err)
		}
	}

	rel := toStaticPath(u.cfg.Upload.Dir, finalPath)
	previewRel := ""
	if preview != "" {
		previewRel = "/static/uploads/" + toStaticPath(u.cfg.Upload.Dir, preview)
	}
	resp := &model.UploadResponse{
		URL:        "/static/uploads/" + filepath.ToSlash(rel),
		PreviewURL: previewRel,
		Size:       written,
		Mime:       mime,
		UploadID:   id,
		Complete:   true,
	}
	// Registry: egalik va keyingi boshqaruv (ro'yxat/o'chirish/GC) uchun.
	u.recordFile(st, resp)
	return 100, resp, nil
}

// recordFile — tugallangan upload'ni registry'ga yozadi (best-effort).
func (u *UploadService) recordFile(st *uploadState, resp *model.UploadResponse) {
	if u.repo == nil || u.repo.Files == nil || resp.URL == "" {
		return
	}
	rec := &model.FileRecord{
		UploadID:   st.ID,
		OwnerID:    st.OwnerID,
		Filename:   st.Name,
		Mime:       resp.Mime,
		Size:       resp.Size,
		URL:        resp.URL,
		PreviewURL: resp.PreviewURL,
		Kind:       "chat",
	}
	if err := u.repo.Files.CreateFile(context.Background(), rec); err != nil {
		slog.Warn("upload registry record", "url", resp.URL, "err", err)
	}
}

// merge chunklarni stream qilib yig'adi (OOM'siz), magic bytes tekshiradi.
// Barcha chunklar 0..Total-1 oraliqda mavjud bo'lishi shart — yetishmayotgan
// chunk bo'lsa buzuq fayl o'rniga xato qaytaradi.
// Qaytaradi: finalPath, mime, yozilgan hajm (bayt).
func (u *UploadService) merge(ctx context.Context, st *uploadState) (string, string, int64, error) {
	dir := filepath.Join(u.cfg.Upload.Dir, time.Now().Format("2006"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", 0, err
	}
	tmpFinal := filepath.Join(dir, ".merge-"+st.ID+".bin")
	out, err := os.OpenFile(tmpFinal, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", "", 0, err
	}
	var (
		written  int64
		head     []byte // mime sniff uchun birinchi baytlar
		mergeErr error
	)
	for i := 0; i < st.Total; i++ {
		if ctx.Err() != nil {
			mergeErr = ctx.Err()
			break
		}
		p := filepath.Join(st.Path, fmt.Sprintf("%08d.bin", i))
		fi, serr := os.Stat(p)
		if serr != nil || fi.IsDir() {
			mergeErr = fmt.Errorf("missing chunk %d", i)
			break
		}
		f, oerr := os.Open(p)
		if oerr != nil {
			mergeErr = oerr
			break
		}
		n, cerr := ioCopyCapped(out, f, u.cfg.Upload.MaxFileSize-written, &head)
		f.Close()
		if cerr != nil {
			mergeErr = cerr
			break
		}
		written += n
	}
	_ = out.Close()
	if mergeErr != nil {
		_ = os.Remove(tmpFinal)
		return "", "", 0, mergeErr
	}
	if written == 0 {
		_ = os.Remove(tmpFinal)
		return "", "", 0, ErrInvalidFile
	}
	if written > u.cfg.Upload.MaxFileSize {
		_ = os.Remove(tmpFinal)
		return "", "", 0, ErrFileTooBig
	}

	mime := sniffMime(head)
	if !u.allowed(mime) {
		_ = os.Remove(tmpFinal)
		return "", "", 0, ErrInvalidFile
	}
	// Rasm saqlashda piksel limiti (faqat preview guard yetmaydi — original
	// URL xonada ulashiladi va mijoz brauzerlarini DoS qiladi).
	if isImageMime(mime) {
		if w, h, derr := imageDimensions(tmpFinal); derr == nil {
			if w > maxStoreImageDim || h > maxStoreImageDim ||
				int64(w)*int64(h) > maxStoreImagePixels {
				_ = os.Remove(tmpFinal)
				return "", "", 0, ErrFileTooBig
			}
		}
	}

	finalPath := filepath.Join(dir, uuid.NewString()+extForMime(mime))
	if err := os.Rename(tmpFinal, finalPath); err != nil {
		_ = os.Remove(tmpFinal)
		return "", "", 0, err
	}
	return finalPath, mime, written, nil
}

func (u *UploadService) allowed(mime string) bool {
	for _, m := range u.cfg.Upload.AllowedTypes {
		if m == mime {
			return true
		}
	}
	return false
}

// SaveAvatar — profil rasmini saqlaydi (yagona request, chunk yo'q).
// Faqat rasm fayllari qabul qilinadi; `uploads/avatars/` papkasiga yozadi.
// Qaytaradi: `/static/uploads/avatars/<fayl>` URL'i.
func (u *UploadService) SaveAvatar(ownerID string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", ErrInvalidFile
	}
	if int64(len(data)) > maxAvatarBytes {
		return "", ErrFileTooBig
	}
	mime := sniffMime(data)
	if !isImageMime(mime) {
		return "", ErrInvalidFile
	}
	// Avatar har bir xabarda barcha ishtirokchilarga yuklanadi — ulkan
	// o'lcham ommaviy client-DoS beradi. Header-only tekshiruv (arzon).
	if cfg, _, derr := image.DecodeConfig(bytes.NewReader(data)); derr == nil {
		if cfg.Width > maxAvatarImageDim || cfg.Height > maxAvatarImageDim ||
			int64(cfg.Width)*int64(cfg.Height) > maxAvatarImagePixels {
			return "", ErrFileTooBig
		}
	}
	if !u.allowed(mime) {
		return "", ErrInvalidFile
	}
	dir := filepath.Join(u.cfg.Upload.Dir, "avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := uuid.NewString() + extForMime(mime)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	url := "/static/uploads/avatars/" + name
	if u.repo != nil && u.repo.Files != nil {
		rec := &model.FileRecord{
			OwnerID:  ownerID,
			Filename: "avatar" + extForMime(mime),
			Mime:     mime,
			Size:     int64(len(data)),
			URL:      url,
			Kind:     "avatar",
		}
		if err := u.repo.Files.CreateFile(context.Background(), rec); err != nil {
			slog.Warn("avatar registry record", "url", url, "err", err)
		}
	}
	return url, nil
}

// Cancel — yarim qolgan upload sessiyasini bekor qiladi (tmp tozalanadi).
func (u *UploadService) Cancel(ownerID, id string) error {
	u.mu.Lock()
	st, ok := u.tmp[id]
	if !ok {
		u.mu.Unlock()
		return repository.ErrNotFound
	}
	if st.OwnerID != "" && ownerID != st.OwnerID {
		u.mu.Unlock()
		return repository.ErrNotFound
	}
	delete(u.tmp, id)
	u.mu.Unlock()
	_ = os.RemoveAll(st.Path)
	return nil
}

// ListFiles — foydalanuvchining fayllari (Fayllarim ro'yxati).
func (u *UploadService) ListFiles(ctx context.Context, ownerID string, limit, offset int) ([]*model.FileRecord, error) {
	if u.repo == nil || u.repo.Files == nil {
		return nil, nil
	}
	return u.repo.Files.FilesByOwner(ctx, ownerID, limit, offset)
}

// DeleteFile — o'z faylini o'chiradi (DB yozuv + diskdagi fayl va preview).
// Avatar-kind yozuv o'chsa, foydalanuvchi profili osilib qolmasligi uchun
// avatar_url ham tozalanadi (M11).
func (u *UploadService) DeleteFile(ctx context.Context, ownerID, id string) error {
	if u.repo == nil || u.repo.Files == nil {
		return repository.ErrNotFound
	}
	rec, err := u.repo.Files.DeleteFile(ctx, id, ownerID)
	if err != nil {
		return err
	}
	if rec.Kind == "avatar" && u.repo.Users != nil {
		if usr, uerr := u.repo.Users.ByID(ctx, ownerID); uerr == nil && usr != nil && usr.AvatarURL == rec.URL {
			usr.AvatarURL = ""
			if uerr := u.repo.Users.Update(ctx, usr); uerr != nil {
				slog.Warn("clear avatar url", "user", ownerID, "err", uerr)
			}
		}
	}
	u.removeAssets(rec.URL, rec.PreviewURL)
	return nil
}

// RenameFile — Fayllarim ekranidan nom o'zgartirish (id bo'yicha, faqat egasi).
// Xabarlarning ko'rinadigan nomi ham yangilanadi.
func (u *UploadService) RenameFile(ctx context.Context, ownerID, id, filename string) (*model.FileRecord, error) {
	if u.repo == nil || u.repo.Files == nil {
		return nil, repository.ErrNotFound
	}
	rec, err := u.repo.Files.FileByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.OwnerID != ownerID {
		return nil, repository.ErrNotFound
	}
	return u.applyRename(ctx, rec, filename)
}

// SaveFileToMy — chatdagi faylni o'z Fayllarimga saqlaydi.
// Xabar fayli diskda ko'chirilib, o'zimizga tegishli yangi URL'ga yoziladi —
// begona faylga havola emas, mustaqil nusxa (fayl boshqa userning o'chiradigan
// nusxasiga bog'liq bo'lmaydi). Idempotent: ro'yxatda shunday nom+hajm yozuvi
// bor bo'lgan fayl qayta ko'chirilmasdan qaytariladi.
// fileBaseNameOf nomi bilan ataylab fayl nomi saqlanadi; kengaytma mime'dan.
func (u *UploadService) SaveFileToMy(ctx context.Context, ownerID, url, filename string) (*model.FileRecord, error) {
	if u.repo == nil || u.repo.Files == nil {
		return nil, repository.ErrNotFound
	}
	if !security.ValidateFileURL(url) {
		return nil, security.ErrFileURL
	}
	// Manba faqat uploads papkasidagi mahalliy fayl bo'lishi mumkin.
	src := diskPathForURL(u.cfg.Upload.Dir, url)
	if src == "" {
		return nil, security.ErrFileURL
	}
	info, err := os.Stat(src)
	if err != nil || info.IsDir() {
		return nil, repository.ErrNotFound
	}
	if info.Size() > u.cfg.Upload.MaxFileSize {
		return nil, ErrFileTooBig
	}
	// MIME HAR DOIM diskdan qayta sniff qilinadi (M8): DB yozuvi eskirgan/
	// noto'g'ri bo'lishi mumkin, manba preview_url bo'lsa ham to'g'ri tip
	// topiladi. Farq bo'lsa log'ga yoziladi (audit izi).
	mime := sniffFileMime(src)
	if rec, err := u.repo.Files.FileByURL(ctx, url); err == nil && rec != nil && rec.Mime != "" && rec.Mime != mime {
		slog.Warn("save mime mismatch", "url", url, "db", rec.Mime, "disk", mime)
	}
	if !u.allowed(mime) {
		return nil, ErrInvalidFile
	}
	name := security.CleanFileName(filename)
	if name == "" {
		name = fileBaseNameOf(url)
	}
	if ext := filepath.Ext(name); ext == "" {
		name += extForMime(mime)
	}
	// Idempotent: shu foyzalanuvchi shu nom+hajmdagi yozuvni allaqachon
	// saqlagan bo'lsa — qayta ko'chirmaymiz.
	if list, err := u.repo.Files.FilesByOwner(ctx, ownerID, 100, 0); err == nil {
		for _, f := range list {
			if f != nil && f.Kind == "chat" && f.Filename == name && f.Size == info.Size() {
				return f, nil
			}
		}
	}
	dir := filepath.Join(u.cfg.Upload.Dir, time.Now().Format("2006"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dst := filepath.Join(dir, uuid.NewString()+extForMime(mime))
	if err := copyFile(src, dst); err != nil {
		return nil, err
	}
	preview := ""
	if isImageMime(mime) {
		if p, perr := makePreview(u.cfg.Upload.Dir, dst); perr == nil {
			preview = p
		} else {
			slog.Warn("save preview", "err", perr)
		}
	}
	previewRel := ""
	if preview != "" {
		previewRel = "/static/uploads/" + toStaticPath(u.cfg.Upload.Dir, preview)
	}
	rec := &model.FileRecord{
		OwnerID:    ownerID,
		Filename:   name,
		Mime:       mime,
		Size:       info.Size(),
		URL:        "/static/uploads/" + toStaticPath(u.cfg.Upload.Dir, dst),
		PreviewURL: previewRel,
		Kind:       "chat",
	}
	if err := u.repo.Files.CreateFile(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// sniffFileMime — diskdagi faylning bosh baytlaridan MIME aniqlaydi.
func sniffFileMime(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	h := make([]byte, 4096)
	n, _ := io.ReadFull(f, h)
	return sniffMime(h[:n])
}

// copyFile — faylni yangi joyga ko'chiradi (buzilgan bo'lsa tail chiqaradi).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

// RenameFileByURL — chatdan nom o'zgartirish (url bo'yicha).
// Registry'da yozuv bo'lmasa (legacy fayl): o'z xabarasi havola qilsa claim
// qilinib keyin rename; birovning fayli bo'lsa ErrNotFound.
func (u *UploadService) RenameFileByURL(ctx context.Context, ownerID, url, filename string) (*model.FileRecord, error) {
	if u.repo == nil || u.repo.Files == nil {
		return nil, repository.ErrNotFound
	}
	if !security.ValidateFileURL(url) {
		return nil, security.ErrFileURL
	}
	rec, err := u.repo.Files.FileByURL(ctx, url)
	if err != nil {
		owns, oerr := u.repo.Messages.OwnsFileURL(ctx, ownerID, url)
		if oerr != nil || !owns {
			return nil, repository.ErrNotFound
		}
		rec = &model.FileRecord{
			OwnerID:  ownerID,
			Filename: fileBaseNameOf(url),
			URL:      url,
			Kind:     "chat",
		}
		if cerr := u.repo.Files.CreateFile(ctx, rec); cerr != nil {
			return nil, cerr
		}
	}
	if rec.OwnerID != ownerID {
		return nil, repository.ErrNotFound
	}
	return u.applyRename(ctx, rec, filename)
}

// applyRename — nom validatsiya + kengaytma saqlash + DB yozuv + xabarlar.
func (u *UploadService) applyRename(ctx context.Context, rec *model.FileRecord, filename string) (*model.FileRecord, error) {
	clean := security.CleanFileName(filename)
	if clean == "" || clean == "file" {
		return nil, ErrBadFilename
	}
	// Kengaytma saqlanadi: foydalanuvchi uni o'chirsa — asl faylniki qaytariladi
	// (aks holda audio/rasm render buziladi).
	if filepath.Ext(clean) == "" {
		if ext := filepath.Ext(rec.Filename); ext != "" {
			clean += ext
		} else if ext := filepath.Ext(fileBaseNameOf(rec.URL)); ext != "" {
			clean += ext
		}
	}
	updated, err := u.repo.Files.UpdateFilename(ctx, rec.ID, rec.OwnerID, clean)
	if err != nil {
		return nil, err
	}
	if err := u.repo.Messages.UpdateContentByFileURL(ctx, rec.OwnerID, rec.URL, clean); err != nil {
		slog.Warn("rename message content", "url", rec.URL, "err", err)
	}
	return updated, nil
}

// fileBaseNameOf — URL'dan fayl nomi.
func fileBaseNameOf(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 && i < len(url)-1 {
		return url[i+1:]
	}
	return url
}

// removeAssets — URL'larni disk yo'liga o'tkazib o'chiradi.
// Faqat `/static/uploads/` prefixi qabul qilinadi (traversal yo'q).
func (u *UploadService) removeAssets(urls ...string) {
	for _, url := range urls {
		p := diskPathForURL(u.cfg.Upload.Dir, url)
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Warn("remove file asset", "path", p, "err", err)
		}
	}
}

// diskPathForURL — `/static/uploads/<rel>` → disk yo'li. Xavfsiz bo'lmasa "".
func diskPathForURL(uploadDir, url string) string {
	const prefix = "/static/uploads/"
	if !strings.HasPrefix(url, prefix) {
		return ""
	}
	rel := url[len(prefix):]
	if rel == "" || strings.Contains(rel, "..") || strings.ContainsAny(rel, "\\\x00") ||
		filepath.IsAbs(rel) {
		return ""
	}
	return filepath.Join(uploadDir, filepath.FromSlash(rel))
}

// StartOrphanGC — yetim (hech kim ishlatmaydigan) fayllarni davriy tozalaydi.
// Skanerlanadi: yillik papkalar (`2026/`) + `avatars/`. `tmp/`, `backgrounds/`
// va boshqa kataloglar tegilmaydi. Faqat maxAge dan eski fayllar o'chiriladi —
// yangi yuklangan (xabari hali yuborilmagan) fayllar xavfsiz.
func (u *UploadService) StartOrphanGC(ctx context.Context, interval, maxAge time.Duration) {
	if u.repo == nil || u.repo.Files == nil {
		return
	}
	if interval <= 0 {
		interval = time.Hour
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				u.sweepOrphans(maxAge)
			}
		}
	}()
}

// sweepOrphans — bitta tozalash o'tishi. Qaytaradi: o'chirilgan fayllar soni.
func (u *UploadService) sweepOrphans(maxAge time.Duration) int {
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	entries, err := os.ReadDir(u.cfg.Upload.Dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		name := e.Name()
		var urls []string
		switch {
		case isYearDir(name) && e.IsDir():
			urls = u.collectDirFiles(name, cutoff)
		case name == "avatars" && e.IsDir():
			urls = u.collectDirFiles(name, cutoff)
		default:
			continue
		}
		for _, url := range urls {
			if u.repo == nil || u.repo.Files == nil {
				return removed
			}
			ref, err := u.repo.Files.IsReferenced(context.Background(), url, 0)
			if err != nil {
				slog.Warn("orphan check", "url", url, "err", err)
				continue
			}
			if ref {
				continue
			}
			if p := diskPathForURL(u.cfg.Upload.Dir, url); p != "" {
				if err := os.Remove(p); err == nil {
					removed++
				}
			}
		}
	}
	if removed > 0 {
		slog.Info("orphan files cleaned", "removed", removed)
	}
	return removed
}

// collectDirFiles — katalogdagi maxAge dan eski oddiy fayllarni URL ro'yxati
// sifatida qaytaradi.
func (u *UploadService) collectDirFiles(dir string, cutoff time.Time) []string {
	files, err := os.ReadDir(filepath.Join(u.cfg.Upload.Dir, dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		out = append(out, "/static/uploads/"+dir+"/"+f.Name())
	}
	return out
}

// isYearDir — `2026` kabi 4 xonali yil papkasi.
func isYearDir(name string) bool {
	if len(name) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		if name[i] < '0' || name[i] > '9' {
			return false
		}
	}
	return true
}

// Status — progress statusini qaytaradi.
func (u *UploadService) Status(id string) (int, error) {
	return u.StatusFor("", id)
}

// StatusFor — progress statusini egasi tekshiruvi bilan qaytaradi.
func (u *UploadService) StatusFor(ownerID, id string) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	st, ok := u.tmp[id]
	if !ok {
		return 0, errors.New("unknown upload")
	}
	if ownerID != "" && st.OwnerID != "" && ownerID != st.OwnerID {
		return 0, errors.New("unknown upload")
	}
	return int(float64(st.Received) / float64(st.Total) * 100), nil
}

// ioCopyCapped — src'dan dst'ga cap baytdan ko'p bo'lmagan oqim bilan ko'chiradi.
// head'ga mime sniff uchun birinchi 4KB gacha bayt yig'iladi. OOM'siz.
func ioCopyCapped(dst io.Writer, src io.Reader, cap int64, head *[]byte) (int64, error) {
	if cap < 0 {
		return 0, ErrFileTooBig
	}
	buf := make([]byte, 64*1024)
	var written int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if written+int64(n) > cap {
				return written, ErrFileTooBig
			}
			if len(*head) < 4096 {
				need := 4096 - len(*head)
				if n < need {
					*head = append(*head, buf[:n]...)
				} else {
					*head = append(*head, buf[:need]...)
				}
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return written, werr
			}
			written += int64(n)
		}
		if rerr == io.EOF {
			return written, nil
		}
		if rerr != nil {
			return written, rerr
		}
	}
}

// sniffMime — magic bytes asosida MIME aniqlaydi (faqat ruxsat etilgan toifalar).
func sniffMime(b []byte) string {
	switch {
	case len(b) >= 8 && bytes.HasPrefix(b, []byte{0x89, 'P', 'N', 'G'}):
		return "image/png"
	case len(b) >= 3 && bytes.Equal(b[:3], []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return "image/gif"
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		ftype := string(b[8:12])
		if ftype == "isom" || ftype == "mp42" || ftype == "avc1" || ftype == "mp41" || ftype == "dash" {
			return "video/mp4"
		}
		return "application/octet-stream"
	case len(b) >= 4 && bytes.Equal(b[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "video/webm"
	case len(b) >= 4 && bytes.Equal(b[:4], []byte{'%', 'P', 'D', 'F'}):
		return "application/pdf"
	case len(b) >= 4 && bytes.Equal(b[:4], []byte{0x50, 0x4B, 0x03, 0x04}):
		return "application/zip" // zip, docx, xlsx, ...
	case len(b) >= 3 && bytes.Equal(b[:3], []byte{0x49, 0x44, 0x33}):
		return "audio/mpeg" // mp3 (ID3 tag)
	case len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0:
		return "audio/mpeg" // mp3 (MPEG frame sync: 11 bit 1)
	case len(b) >= 12 && bytes.Equal(b[:4], []byte{'R', 'I', 'F', 'F'}) && string(b[8:12]) == "WAVE":
		return "audio/wav"
	case len(b) >= 4 && bytes.Equal(b[:4], []byte{'O', 'g', 'g', 'S'}):
		return "audio/ogg"
	case len(b) >= 4 && bytes.Equal(b[:4], []byte{0x7F, 'E', 'L', 'F'}):
		return "application/octet-stream" // ELF blok
	default:
		if isLikelyText(b) {
			return "text/plain"
		}
		return "application/octet-stream"
	}
}

// isLikelyText - birinchi blok deyarli to'liq bosma belgilardan iborat
// bo'lsa (yangi qatorlardan tashqari control char'lar yo'q) matn deb qaraydi.
func isLikelyText(b []byte) bool {
	n := len(b)
	if n > 1024 {
		n = 1024
	}
	if n == 0 {
		return false
	}
	for _, c := range b[:n] {
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			return false
		}
		if c == 0x7F {
			return false
		}
	}
	return true
}

func isImageMime(mime string) bool {
	return strings.HasPrefix(mime, "image/")
}

func extForMime(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	case "text/plain":
		return ".txt"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	default:
		return ".bin"
	}
}

// Decompression bomb himoyasi: juda katta o'lchamdagi rasm to'liq dekod
// qilinmaydi — preview generatsiyasidan oldin sarlavhadan (header) o'lcham
// o'qiladi. Limitdan oshgan rasmlarga preview yaratilmaydi.
const (
	maxPreviewDim    = 8000     // bitta tomon uchun piksel chegarasi
	maxPreviewPixels = 30000000 // umumiy piksel chegarasi (~30 MP)
)

// imageDimensions — header-only o'lcham o'qish (to'liq dekod yo'q, arzon).
func imageDimensions(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// makePreview rasm uchun thumbnail generatsiya qiladi (pure-Go imaging).
func makePreview(uploadDir, src string) (string, error) {
	// Parallel dekod semafori — slot bo'lmasa preview o'tkazib yuboriladi.
	select {
	case previewSlots <- struct{}{}:
		defer func() { <-previewSlots }()
	default:
		return "", nil
	}
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	imgCfg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil {
		return "", err
	}
	if imgCfg.Width > maxPreviewDim || imgCfg.Height > maxPreviewDim ||
		imgCfg.Width*imgCfg.Height > maxPreviewPixels {
		// O'lcham limitdan katta — xavfli/preview qilmaymiz (fayl saqlanadi,
		// lekin server xotirasida to'liq dekod qilinmaydi).
		return "", nil
	}
	img, err := imaging.Open(src, imaging.AutoOrientation(true))
	if err != nil {
		return "", err
	}
	bounds := img.Bounds()
	maxW, maxH := 640, 640
	if bounds.Dx() <= maxW && bounds.Dy() <= maxH {
		return "", nil
	}
	thumb := imaging.Fit(img, maxW, maxH, imaging.Lanczos)
	ext := filepath.Ext(src)
	outName := strings.TrimSuffix(filepath.Base(src), ext) + "_prev" + ext
	out := filepath.Join(uploadDir, time.Now().Format("2006"), outName)
	if err := imaging.Save(thumb, out, imaging.JPEGQuality(82)); err != nil {
		return "", err
	}
	return out, nil
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == ".." || name == "" || name == "/" {
		return "file"
	}
	return name
}

func toStaticPath(uploadDir, abs string) string {
	rel, err := filepath.Rel(uploadDir, abs)
	if err != nil {
		return filepath.Base(abs)
	}
	return filepath.ToSlash(rel)
}
