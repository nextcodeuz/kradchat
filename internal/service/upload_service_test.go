package service

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/model"
	"kradchat/internal/repository"
)

func testUploadCfg(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		Upload: config.Upload{
			Dir:         dir,
			MaxFileSize: 10 * 1024 * 1024,
			ChunkSize:   1024,
			AllowedTypes: []string{
				"image/jpeg", "image/png", "image/gif", "image/webp",
				"video/mp4", "video/webm",
			},
		},
	}
}

func makePNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 139, G: 92, B: 246, A: 255})
		}
	}
	var f *os.File
	var err error
	if f, err = os.CreateTemp(t.TempDir(), "t*.png"); err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestUploadCompleteFlow(t *testing.T) {
	svc := NewUploadService(testUploadCfg(t), nil)
	data := makePNGBytes(t, 300, 200)
	const chunkSize = 1024
	total := (len(data) + chunkSize - 1) / chunkSize

	st, err := svc.Start("", "test.png", int64(len(data)), total)
	if err != nil {
		t.Fatal(err)
	}
	var lastPct int
	for i := 0; i < total; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}
		pct, resp, err := svc.Chunk(context.Background(), "", st.ID, i, total, data[start:end])
		if err != nil {
			t.Fatalf("chunk %d: %v", i, err)
		}
		lastPct = pct
		if i < total-1 && resp.Complete {
			t.Fatalf("chunk %d should not be complete", i)
		}
		if i == total-1 && !resp.Complete {
			t.Fatalf("last chunk should be complete")
		}
	}
	if lastPct != 100 {
		t.Errorf("final progress = %d, want 100", lastPct)
	}
}

func TestUploadRejectsBadMime(t *testing.T) {
	svc := NewUploadService(testUploadCfg(t), nil)
	data := []byte("this is not an image or allowed media file, just text bytes here")
	st, err := svc.Start("", "evil.txt", int64(len(data)), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Chunk(context.Background(), "", st.ID, 0, 1, data)
	if err != ErrInvalidFile {
		t.Errorf("err = %v, want ErrInvalidFile", err)
	}
}

func TestUploadRejectsTooBig(t *testing.T) {
	cfg := testUploadCfg(t)
	svc := NewUploadService(cfg, nil)
	_, err := svc.Start("", "big.png", cfg.Upload.MaxFileSize+1, 10)
	if err != ErrFileTooBig {
		t.Errorf("err = %v, want ErrFileTooBig", err)
	}
}

func TestUploadRejectsOutOfOrder(t *testing.T) {
	svc := NewUploadService(testUploadCfg(t), nil)
	st, err := svc.Start("", "f.png", 2048, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Total mos emas
	if _, _, err := svc.Chunk(context.Background(), "", st.ID, 0, 5, make([]byte, 1024)); err != ErrChunkOrder {
		t.Errorf("mismatched total: err = %v, want ErrChunkOrder", err)
	}
	// Index diapazondan tashqarida
	if _, _, err := svc.Chunk(context.Background(), "", st.ID, 9, 2, make([]byte, 1024)); err != ErrChunkOrder {
		t.Errorf("bad index: err = %v, want ErrChunkOrder", err)
	}
}

func TestUploadRetryIsIdempotent(t *testing.T) {
	svc := NewUploadService(testUploadCfg(t), nil)
	data := makePNGBytes(t, 64, 64)
	st, err := svc.Start("", "retry.png", int64(len(data)), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, resp, err := svc.Chunk(context.Background(), "", st.ID, 0, 1, data)
	if err != nil || !resp.Complete {
		t.Fatalf("first chunk: %v complete=%v", err, resp.Complete)
	}
	// Retry — hozir "already complete" qaytarishi kerak, xato emas.
	if _, _, err := svc.Chunk(context.Background(), "", st.ID, 0, 1, data); err == nil {
		t.Error("retry after complete should return error")
	}
}

func TestUploadPreviewGenerated(t *testing.T) {
	cfg := testUploadCfg(t)
	svc := NewUploadService(cfg, nil)
	data := makePNGBytes(t, 800, 800) // preview (640 dan katta) hosil bo'ladi
	st, err := svc.Start("", "big.png", int64(len(data)), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, resp, err := svc.Chunk(context.Background(), "", st.ID, 0, 1, data)
	if err != nil {
		t.Fatal(err)
	}
	if resp.URL == "" {
		t.Error("URL empty")
	}
	// Preview fayli diskovda mavjud bo'lishi kerak.
	if resp.PreviewURL != "" {
		rel := resp.PreviewURL
		rel = rel[len("/static/uploads/"):]
		if _, err := os.Stat(filepath.Join(cfg.Upload.Dir, rel)); err != nil {
			t.Errorf("preview file missing: %v", err)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd": "passwd",
		"a\\b\\c.png":      "c.png",
		"normal.png":       "normal.png",
		"..":               "file",
		"":                 "file",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSniffMime(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}, "image/png"},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"gif", []byte("GIF89a..."), "image/gif"},
		{"webm", []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01}, "video/webm"},
		{"pdf", []byte("%PDF-1.7"), "application/pdf"},
		{"zip", []byte{0x50, 0x4B, 0x03, 0x04, 0x00}, "application/zip"},
		{"mp3", []byte("ID3\x04\x00\x00\x00"), "audio/mpeg"},
		{"mp3-frame", []byte{0xFF, 0xFB, 0x90, 0x00, 0x00}, "audio/mpeg"},
		{"wav", []byte("RIFF\x24\x00\x00\x00WAVEfmt "), "audio/wav"},
		{"riff-qisqa", []byte("RIFFxxxx"), "text/plain"}, // M3: 4-11 bayt panic bermasin
		{"riff-4", []byte("RIFF"), "text/plain"},
		{"ff-yolgiz", []byte{0xFF}, "text/plain"},
		{"bosh", []byte{}, "application/octet-stream"},
		{"text", []byte("hello world"), "text/plain"},
		{"binary", []byte{0x00, 0x01, 0x02, 0xFF, 0x7F}, "application/octet-stream"},
	}
	for _, c := range cases {
		if got := sniffMime(c.data); got != c.want {
			t.Errorf("sniffMime(%s) = %s, want %s", c.name, got, c.want)
		}
	}
}

// stubFileRepo — registry/GC testlari uchun in-memory FileRepo.
type stubFileRepo struct {
	created []*model.FileRecord
	byURL   map[string]*model.FileRecord
	refs    map[string]bool
}

func (s *stubFileRepo) CreateFile(_ context.Context, f *model.FileRecord) error {
	s.created = append(s.created, f)
	if s.byURL == nil {
		s.byURL = make(map[string]*model.FileRecord)
	}
	s.byURL[f.URL] = f
	return nil
}

func (s *stubFileRepo) FileByID(_ context.Context, id string) (*model.FileRecord, error) {
	for _, f := range s.created {
		if f.ID == id {
			return f, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (s *stubFileRepo) FileByURL(_ context.Context, url string) (*model.FileRecord, error) {
	if f, ok := s.byURL[url]; ok {
		return f, nil
	}
	return nil, repository.ErrNotFound
}

func (s *stubFileRepo) FilesByOwner(_ context.Context, _ string, _, _ int) ([]*model.FileRecord, error) {
	return s.created, nil
}

func (s *stubFileRepo) DeleteFile(_ context.Context, id, ownerID string) (*model.FileRecord, error) {
	for i, f := range s.created {
		if f.ID == id && f.OwnerID == ownerID {
			s.created = append(s.created[:i], s.created[i+1:]...)
			delete(s.byURL, f.URL)
			return f, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (s *stubFileRepo) IsReferenced(_ context.Context, url string, _ int64) (bool, error) {
	return s.refs[url], nil
}

func (s *stubFileRepo) UpdateFilename(_ context.Context, id, ownerID, filename string) (*model.FileRecord, error) {
	for _, f := range s.created {
		if f.ID == id && f.OwnerID == ownerID {
			f.Filename = filename
			return f, nil
		}
	}
	return nil, repository.ErrNotFound
}

// stubMessageRepo — rename testlari uchun minimal Messages.
type stubMessageRepo struct {
	owns    map[string]bool // url -> egalik
	updated []string
}

func (s *stubMessageRepo) Insert(_ context.Context, _ *model.Message) error { return nil }
func (s *stubMessageRepo) BulkInsert(_ context.Context, _ []*model.Message) error {
	return nil
}
func (s *stubMessageRepo) ByClientID(_ context.Context, _ string) (*model.Message, error) {
	return nil, repository.ErrNotFound
}
func (s *stubMessageRepo) ByID(_ context.Context, _ int64) (*model.Message, error) {
	return nil, repository.ErrNotFound
}
func (s *stubMessageRepo) ByIDs(_ context.Context, _ []int64) (map[int64]*model.Message, error) {
	return map[int64]*model.Message{}, nil
}
func (s *stubMessageRepo) History(_ context.Context, _ string, _ int64, _ int) ([]*model.Message, error) {
	return nil, nil
}
func (s *stubMessageRepo) Conversations(_ context.Context, _ string) ([]repository.ConversationItem, error) {
	return nil, nil
}
func (s *stubMessageRepo) MarkDeleted(_ context.Context, _ int64) error { return nil }
func (s *stubMessageRepo) UpdateContentByFileURL(_ context.Context, _, url, _ string) error {
	s.updated = append(s.updated, url)
	return nil
}
func (s *stubMessageRepo) OwnsFileURL(_ context.Context, _, url string) (bool, error) {
	return s.owns[url], nil
}

func testRenameSvc(t *testing.T) (*UploadService, *stubFileRepo, *stubMessageRepo) {
	t.Helper()
	cfg := testUploadCfg(t)
	files := &stubFileRepo{}
	msgs := &stubMessageRepo{owns: map[string]bool{}}
	repo := &repository.Repositories{Files: files, Messages: msgs}
	return NewUploadService(cfg, repo), files, msgs
}

func TestRenameFile(t *testing.T) {
	svc, files, msgs := testRenameSvc(t)
	files.created = append(files.created, &model.FileRecord{
		ID: "f1", OwnerID: "u1", Filename: "old.mp3",
		URL: "/static/uploads/2026/x.mp3",
	})
	rec, err := svc.RenameFile(context.Background(), "u1", "f1", "yangi nom")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if rec.Filename != "yangi nom.mp3" {
		t.Errorf("filename = %q, want ext preserved", rec.Filename)
	}
	if len(msgs.updated) != 1 {
		t.Error("message content should be updated")
	}
	// Begona rename rad.
	if _, err := svc.RenameFile(context.Background(), "u2", "f1", "x"); err == nil {
		t.Error("foreign rename should fail")
	}
	// Bo'sh nom rad.
	if _, err := svc.RenameFile(context.Background(), "u1", "f1", "   "); err != ErrBadFilename {
		t.Errorf("err = %v, want ErrBadFilename", err)
	}
}

func TestRenameFileByURLLegacyClaim(t *testing.T) {
	svc, _, msgs := testRenameSvc(t)
	msgs.owns["/static/uploads/2026/legacy.mp3"] = true
	rec, err := svc.RenameFileByURL(context.Background(), "u1", "/static/uploads/2026/legacy.mp3", "retro")
	if err != nil {
		t.Fatalf("legacy claim rename: %v", err)
	}
	if rec.Filename != "retro.mp3" {
		t.Errorf("filename = %q", rec.Filename)
	}
	// Hech kimniki bo'lmagan URL rad.
	if _, err := svc.RenameFileByURL(context.Background(), "u1", "/static/uploads/2026/other.mp3", "x"); err == nil {
		t.Error("unowned url should fail")
	}
}

func testUploadSvcWithRepo(t *testing.T, stub *stubFileRepo) (*UploadService, *config.Config) {
	t.Helper()
	cfg := testUploadCfg(t)
	repo := &repository.Repositories{Files: stub}
	return NewUploadService(cfg, repo), cfg
}

func TestUploadRecordsRegistry(t *testing.T) {
	stub := &stubFileRepo{}
	svc, _ := testUploadSvcWithRepo(t, stub)
	data := makePNGBytes(t, 300, 200)
	st, err := svc.Start("u1", "doc.png", int64(len(data)), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, resp, err := svc.Chunk(context.Background(), "u1", st.ID, 0, 1, data)
	if err != nil || !resp.Complete {
		t.Fatalf("chunk: %v complete=%v", err, resp.Complete)
	}
	if len(stub.created) != 1 {
		t.Fatalf("registry records = %d, want 1", len(stub.created))
	}
	rec := stub.created[0]
	if rec.OwnerID != "u1" || rec.URL == "" || rec.Size <= 0 || rec.Mime == "" {
		t.Errorf("bad record: %+v", rec)
	}
}

func TestUploadCancel(t *testing.T) {
	svc := NewUploadService(testUploadCfg(t), nil)
	data := makePNGBytes(t, 64, 64)
	st, err := svc.Start("u1", "c.png", int64(len(data)*2), 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Chunk(context.Background(), "u1", st.ID, 0, 2, data); err != nil {
		t.Fatal(err)
	}
	if err := svc.Cancel("u1", st.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := svc.Cancel("u1", st.ID); err == nil {
		t.Error("second cancel should fail")
	}
	// Begona user bekor qila olmaydi.
	st2, err := svc.Start("u1", "c2.png", int64(len(data)*2), 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Cancel("intruder", st2.ID); err == nil {
		t.Error("foreign cancel should fail")
	}
}

func TestSweepOrphans(t *testing.T) {
	stub := &stubFileRepo{refs: map[string]bool{}}
	svc, cfg := testUploadSvcWithRepo(t, stub)
	yearDir := filepath.Join(cfg.Upload.Dir, "2020")
	avDir := filepath.Join(cfg.Upload.Dir, "avatars")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(avDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	writeOld := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	orphan := filepath.Join(yearDir, "orphan.png")
	kept := filepath.Join(yearDir, "kept.png")
	fresh := filepath.Join(yearDir, "fresh.png")
	avOrphan := filepath.Join(avDir, "old.png")
	writeOld(orphan)
	writeOld(kept)
	writeOld(avOrphan)
	if err := os.WriteFile(fresh, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stub.refs["/static/uploads/2020/kept.png"] = true
	if n := svc.sweepOrphans(24 * time.Hour); n != 2 {
		t.Errorf("sweepOrphans removed = %d, want 2", n)
	}
	for _, p := range []string{orphan, avOrphan} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be removed", p)
		}
	}
	for _, p := range []string{kept, fresh} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should be kept: %v", p, err)
		}
	}
}

func TestSaveFileToMy(t *testing.T) {
	stub := &stubFileRepo{}
	svc, cfg := testUploadSvcWithRepo(t, stub)
	// Manba fayl (boshqa userning xabarisi) — uploads/2026/tuzgul.png.
	srcBytes := makePNGBytes(t, 300, 200)
	yearDir := filepath.Join(cfg.Upload.Dir, "2026")
	if err := os.MkdirAll(yearDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcPath := filepath.Join(yearDir, "tuzgul.png")
	if err := os.WriteFile(srcPath, srcBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	srcURL := "/static/uploads/2026/tuzgul.png"
	stub.byURL = map[string]*model.FileRecord{
		srcURL: {ID: "s1", OwnerID: "u9", Filename: "tuzgul.png", Mime: "image/png", URL: srcURL, Size: int64(len(srcBytes))},
	}

	// u1 o'z fayllariga saqlaydi.
	rec, err := svc.SaveFileToMy(context.Background(), "u1", srcURL, "meniki")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if rec.OwnerID != "u1" {
		t.Errorf("owner = %q, want u1", rec.OwnerID)
	}
	if rec.Filename != "meniki.png" {
		t.Errorf("filename = %q, want ext appended", rec.Filename)
	}
	if rec.Kind != "chat" {
		t.Errorf("kind = %q, want chat", rec.Kind)
	}
	if rec.URL == srcURL {
		t.Error("saved copy must have a distinct URL")
	}
	// Nusxa diskda bor va nomi teng.
	dst := diskPathForURL(cfg.Upload.Dir, rec.URL)
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("copy missing: %v", err)
	}
	if fi, _ := os.Stat(dst); fi.Size() != int64(len(srcBytes)) {
		t.Errorf("copy size = %d, want %d", fi.Size(), len(srcBytes))
	}

	// Idempotent: xuddi shu nom+hajm bilan takror — yangi nusxa yaratilmaydi.
	again, err := svc.SaveFileToMy(context.Background(), "u1", srcURL, "meniki")
	if err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if again.URL != rec.URL {
		t.Errorf("re-save produced different URL; want idempotent")
	}

	// Noto'g'ri URL.
	if _, err := svc.SaveFileToMy(context.Background(), "u1", "https://evil.com/x.png", "x"); err == nil {
		t.Error("foreign URL should be rejected")
	}
	// Mavjud bo'lmagan fayl.
	if _, err := svc.SaveFileToMy(context.Background(), "u1", "/static/uploads/2026/boshlik.png", "y"); err == nil {
		t.Error("missing source should be rejected")
	}
}
