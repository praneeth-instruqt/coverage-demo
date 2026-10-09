// Command server runs the todo API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/praneeth-instruqt/coverage-demo/internal/api"
	"github.com/praneeth-instruqt/coverage-demo/internal/auth"
	"github.com/praneeth-instruqt/coverage-demo/internal/config"
	"github.com/praneeth-instruqt/coverage-demo/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger, os.Getenv); err != nil {
		logger.Error("server exited", "err", err)
		os.Exit(1)
	}
}

// run serves until ctx is cancelled, then shuts down gracefully.
func run(ctx context.Context, logger *slog.Logger, getenv func(string) string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	tokens, err := auth.NewTokenManager([]byte(cfg.JWTSecret), cfg.TokenTTL)
	if err != nil {
		return fmt.Errorf("token manager: %w", err)
	}
	srv, err := api.NewServer(store.NewMemory(), tokens, auth.NewPasswordHasher(cfg.HashIterations), logger)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err // ListenAndServe only returns ErrServerClosed after Shutdown
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
