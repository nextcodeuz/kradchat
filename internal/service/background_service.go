package service

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/model"
	"kradchat/internal/repository"
	"kradchat/internal/ws"
)

// BackgroundService — fonlar katalogini skanerlaydi va yangi rasmlarni
// avtomatik chat_backgrounds ga qo'shadi. User +1 yoki +4 rasm qo'shsa —
// barcha onlayn userlarga bg_list avtomatik yangilanadi.
type BackgroundService struct {
	cfg  *config.Config
	repo *repository.Repositories
	hub  *ws.Hub

	stopCh   chan struct{}
	stopOnce sync.Once
	doneCh   chan struct{}
}

var allowedBGExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true,
}

func NewBackgroundService(cfg *config.Config, repo *repository.Repositories, hub *ws.Hub) *BackgroundService {
	return &BackgroundService{
		cfg:    cfg,
		repo:   repo,
		hub:    hub,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (b *BackgroundService) Start(ctx context.Context) {
	go b.loop(ctx)
}

func (b *BackgroundService) Stop() {
	b.stopOnce.Do(func() { close(b.stopCh) })
	<-b.doneCh
}

func (b *BackgroundService) loop(ctx context.Context) {
	defer close(b.doneCh)
	b.scan(ctx)
	tick := time.NewTicker(b.cfg.Background.Poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.stopCh:
			return
		case <-tick.C:
			b.scan(ctx)
		}
	}
}

// scan katalog bo'ylab yuradi va yangi fayllarni DB ga qo'shadi.
func (b *BackgroundService) scan(ctx context.Context) {
	root := b.cfg.Background.Dir
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("background scan root", "err", err)
		}
		return
	}

	// Root'dagi to'g'ridan-to'g'ri rasmlar — "default" kategoriyasi.
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b.scanFile(ctx, e.Name(), "default")
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		category := e.Name()
		if category == "." || strings.HasPrefix(category, "_") {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, category))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			b.scanFile(ctx, filepath.Join(category, f.Name()), category)
		}
	}
}

// scanFile bitta rasm faylini tekshiradi va yangi bo'lsa DB ga qo'shadi.
func (b *BackgroundService) scanFile(ctx context.Context, rel, category string) {
	ext := strings.ToLower(filepath.Ext(rel))
	if !allowedBGExt[ext] {
		return
	}
	info, err := os.Stat(filepath.Join(b.cfg.Background.Dir, rel))
	if err != nil {
		return
	}
	if info.Size() > b.cfg.Background.MaxSize {
		return
	}
	rel = filepath.ToSlash(rel)
	bg := &model.Background{
		Name:     strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)),
		Category: category,
		FilePath: rel,
	}
	if _, err := b.repo.Backgrounds.ByPath(ctx, rel); err == nil {
		return // allaqachon mavjud
	}
	if err := b.repo.Backgrounds.Create(ctx, bg); err != nil {
		slog.Warn("create background", "path", rel, "err", err)
		return
	}
	slog.Info("new background", "name", bg.Name, "category", category)
	b.broadcastList(ctx)
}

func (b *BackgroundService) List(ctx context.Context) ([]*model.Background, error) {
	return b.repo.Backgrounds.AllActive(ctx)
}

// ListPayloads — WS ready/bg_list xabarlariga qo'shish uchun payload ro'yxati.
func (b *BackgroundService) ListPayloads(ctx context.Context) []*ws.BGPayload {
	bgs, err := b.List(ctx)
	if err != nil {
		return nil
	}
	var payloads []*ws.BGPayload
	for _, bg := range bgs {
		payloads = append(payloads, &ws.BGPayload{
			ID:       bg.ID,
			Name:     bg.Name,
			Category: bg.Category,
			URL:      "/static/backgrounds/" + bg.FilePath,
		})
	}
	return payloads
}

// SetBackground — fon tanlaydi. bgID "" yoki "none" bo'lsa tanlovni o'chiradi.
func (b *BackgroundService) SetBackground(ctx context.Context, userID, bgID, room string) error {
	if bgID == "" || bgID == "none" {
		return b.repo.Backgrounds.ClearUserBG(ctx, userID, room)
	}
	if _, err := b.repo.Backgrounds.Get(ctx, bgID); err != nil {
		return err
	}
	return b.repo.Backgrounds.SetUserBG(ctx, userID, bgID, room)
}

func (b *BackgroundService) UserBackground(ctx context.Context, userID, room string) (string, error) {
	return b.repo.Backgrounds.UserBGByRoom(ctx, userID, room)
}

func (b *BackgroundService) broadcastList(ctx context.Context) {
	payloads := b.ListPayloads(ctx)
	msg := &ws.ServerMsg{Type: ws.STypeBGList, Backgrounds: payloads}
	// Faqat global broadcast — barcha ulangan client global xonada turadi.
	// (Avval unicast ham qo'shilgandi — har client bg_list'ni 2 marta olardi.)
	b.hub.BroadcastToRoom(model.RoomGlobal, msg)
}
