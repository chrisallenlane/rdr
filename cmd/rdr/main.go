package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	rdr "github.com/chrisallenlane/rdr"
	"github.com/chrisallenlane/rdr/internal/config"
	"github.com/chrisallenlane/rdr/internal/database"
	"github.com/chrisallenlane/rdr/internal/handler"
	"github.com/chrisallenlane/rdr/internal/poller"
)

var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	srv, err := handler.NewServer(db, rdr.StaticFiles, rdr.TemplateFiles, cfg.FaviconsDir)
	if err != nil {
		slog.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start background feed poller.
	var wg sync.WaitGroup
	p := poller.NewPoller(db, cfg.PollInterval, cfg.RetentionDays, cfg.FaviconsDir)
	srv.SetSyncFunc(p.TriggerSync)
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.Start(ctx)
	}()

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	slog.Info("starting rdr", "version", version, "addr", cfg.ListenAddr, "poll_interval", cfg.PollInterval)
	<-ctx.Done()
	slog.Info("shutting down")

	// Wait for poller to finish its current cycle.
	wg.Wait()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
