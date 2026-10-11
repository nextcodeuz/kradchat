package service

import (
	"context"
	"log/slog"
	"sync"

	"kradchat/internal/repository"
)

// BlockService — bloklash holatini in-memory cache bilan boshqaradi.
// Broadcast paytida har bir client uchun DB so'rovi qilmaslik uchun
// barcha bloklashlar xotiraga yuklanadi; o'zgarishlar ham cache'ni
// darhol yangilaydi.
type BlockService struct {
	repo *repository.Repositories

	mu        sync.RWMutex
	byBlocker map[string]map[string]bool
}

func NewBlockService(repo *repository.Repositories) *BlockService {
	return &BlockService{
		repo:      repo,
		byBlocker: make(map[string]map[string]bool),
	}
}

// Start — barcha bloklashlarni xotiraga yuklaydi.
func (b *BlockService) Start(ctx context.Context) {
	pairs, err := b.repo.Blocks.All(ctx)
	if err != nil {
		slog.Warn("blocks: load failed", "err", err)
		return
	}
	b.mu.Lock()
	for _, p := range pairs {
		if b.byBlocker[p.BlockerID] == nil {
			b.byBlocker[p.BlockerID] = make(map[string]bool)
		}
		b.byBlocker[p.BlockerID][p.BlockedID] = true
	}
	b.mu.Unlock()
	slog.Info("blocks: cache loaded", "n", len(pairs))
}

// Block — user'ni bloklaydi (DB + cache).
func (b *BlockService) Block(ctx context.Context, blockerID, blockedID string) error {
	if blockerID == blockedID {
		return repository.ErrBadID
	}
	if err := b.repo.Blocks.Block(ctx, blockerID, blockedID); err != nil {
		return err
	}
	b.mu.Lock()
	if b.byBlocker[blockerID] == nil {
		b.byBlocker[blockerID] = make(map[string]bool)
	}
	b.byBlocker[blockerID][blockedID] = true
	b.mu.Unlock()
	return nil
}

// Unblock — bloklashni olib tashlaydi (DB + cache).
func (b *BlockService) Unblock(ctx context.Context, blockerID, blockedID string) error {
	if err := b.repo.Blocks.Unblock(ctx, blockerID, blockedID); err != nil {
		return err
	}
	b.mu.Lock()
	if m := b.byBlocker[blockerID]; m != nil {
		delete(m, blockedID)
		if len(m) == 0 {
			delete(b.byBlocker, blockerID)
		}
	}
	b.mu.Unlock()
	return nil
}

// Blocks — blocker foydalanuvchisi blocked foydalanuvchini bloklaganmi (cache).
func (b *BlockService) Blocks(blockerID, blockedID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	m := b.byBlocker[blockerID]
	if m == nil {
		return false
	}
	return m[blockedID]
}

// BlocksAny — ikki user orasida bloklash bor-yo'qligi (istalgan yo'nalishda).
func (b *BlockService) BlocksAny(u1, u2 string) bool {
	return b.Blocks(u1, u2) || b.Blocks(u2, u1)
}

// skipFilter — broadcast uchun filtr qaytaradi: xabar muallifi (sender)
// tomonidan... Aksincha: oluvchi (client) sender'ni bloklagan bo'lsa o'tkazib
// yuboramiz. Massiv chatda O(1) cache tekshiruvi.
func (b *BlockService) skipFilter(senderID string) func(userID string) bool {
	return func(recipientID string) bool {
		if recipientID == senderID {
			return false
		}
		return b.Blocks(recipientID, senderID)
	}
}
