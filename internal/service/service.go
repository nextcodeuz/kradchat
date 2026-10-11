package service

import (
	"context"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/repository"
	"kradchat/internal/ws"
)

// Services — barcha biznes logika agregati.
type Services struct {
	Auth       *AuthService
	TwoFA      *TwoFAService
	Chat       *ChatService
	Upload     *UploadService
	Background *BackgroundService
	Presence   *PresenceService
	Blocks     *BlockService

	cfg *config.Config
}

func New(cfg *config.Config, repo *repository.Repositories, hub *ws.Hub) *Services {
	auth := NewAuthService(cfg, repo)
	presence := NewPresenceService(cfg, repo, hub)
	twofa := NewTwoFAService(cfg, repo)
	blocks := NewBlockService(repo)
	presence.SetBlocks(blocks)
	chat := NewChatService(cfg, repo, hub, presence, blocks)
	upload := NewUploadService(cfg, repo)
	bg := NewBackgroundService(cfg, repo, hub)
	return &Services{
		Auth:       auth,
		TwoFA:      twofa,
		Chat:       chat,
		Upload:     upload,
		Background: bg,
		Presence:   presence,
		Blocks:     blocks,
		cfg:        cfg,
	}
}

func (s *Services) Config() *config.Config { return s.cfg }

func (s *Services) Start(ctx context.Context) {
	s.Auth.SeedVerified(ctx)
	s.Background.Start(ctx)
	s.Presence.Start()
	s.Blocks.Start(ctx)
	s.Upload.StartTmpCleanup(ctx)
	s.Upload.StartOrphanGC(ctx, time.Hour, 24*time.Hour)
}

func (s *Services) Stop() {
	s.Background.Stop()
	s.Presence.Stop()
	s.Chat.Stop()
}
