package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/config"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/httpapi"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/state"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/store"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	transactionStore := chooseTransactionStore(cfg, logger)

	registry := state.NewRegistry()
	app := httpapi.NewServer(cfg, logger, registry, transactionStore)

	server := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           app.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)

	go func() {
		logger.Info("starting OCPPHAL Go compatibility server", "addr", server.Addr)
		errCh <- server.ListenAndServe()
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stopCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("shutdown complete")
}

func chooseTransactionStore(cfg config.Config, logger *slog.Logger) store.TransactionStore {
	if cfg.HasDatabase() {
		postgresStore, err := store.NewPostgresStore(cfg)
		if err == nil {
			logger.Info("using PostgreSQL transaction store", "db_host", cfg.DBHost, "db_name", cfg.DBName)
			return postgresStore
		}

		logger.Warn("failed to connect PostgreSQL; using in-memory transaction store for this run", "error", err)
		return store.NewMemoryStore()
	}

	logger.Warn("DB env not configured; using in-memory transaction store")
	return store.NewMemoryStore()
}
