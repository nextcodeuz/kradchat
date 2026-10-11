package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/database"
	"kradchat/internal/handler"
	"kradchat/internal/repository"
	"kradchat/internal/service"
	"kradchat/internal/ws"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parseLogLevel(os.Getenv("LOG_LEVEL"))})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}

// run — composition root: barcha qatlamlarni yig'adi va serverni boshqaradi.
// Xato qaytarsa defer'lar (db.Close, svc.Stop, StopGC) kafolatli ishlaydi.
func run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// === Database ===
	db, err := database.Open(cfg)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	if err := database.Migrate(ctx, db, cfg.Database.DialectName()); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// === Layers (Handler → Service → Repository → DB) ===
	repo, err := repository.New(db, cfg)
	if err != nil {
		return fmt.Errorf("repository: %w", err)
	}

	hub := ws.NewHub()
	svc := service.New(cfg, repo, hub)
	svc.Start(ctx)
	defer svc.Stop()

	router := handler.NewRouter(cfg, svc, hub)
	router.StartGC()
	defer router.StopGC()

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           router.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("KRADCHAT server started", "addr", "http://"+cfg.Addr())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		return fmt.Errorf("listen: %w", err)
	}

	slog.Info("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	slog.Info("server stopped")
	return nil
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
