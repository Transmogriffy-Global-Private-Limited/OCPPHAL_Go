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

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/chargerdir"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/config"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/hooks"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/httpapi"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/ocpp16hal"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/state"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/store"
)

func main() {
	cfg := config.Load()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	chargerDirectory := chargerdir.NewService(cfg, logger)
	ocpp16hal.SetChargerDirectory(chargerDirectory, logger)
	httpapi.SetChargerDirectory(chargerDirectory)

	txStore, err := chooseTransactionStore(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize transaction store", "error", err)
		os.Exit(1)
	}

	hookManager := hooks.NewManager(cfg, txStore, logger)
	hookManager.Start()

	registry := state.NewRegistry()
	hal := ocpp16hal.New(registry, txStore, hookManager, logger)
	hookManager.SetLimitStopper(hal)

	go func() {
		hal.Start(cfg.OCPPListenPort, cfg.OCPPListenPath)
	}()

	go func() {
		for err := range hal.Errors() {
			logger.Error("ocpp-go error", "error", err)
		}
	}()

	api := httpapi.NewServer(cfg, logger, registry, hal)

	restServer := &http.Server{
		Addr:              cfg.RESTListenAddr(),
		Handler:           api.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)

	go func() {
		logger.Info("starting REST API", "addr", restServer.Addr)
		errCh <- restServer.ListenAndServe()
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stopCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("REST server failed", "error", err)
			os.Exit(1)
		}
	}

	hal.Stop()
	hookManager.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := restServer.Shutdown(ctx); err != nil {
		logger.Error("REST shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("shutdown complete")
}

func chooseTransactionStore(cfg config.Config, logger *slog.Logger) (store.TransactionStore, error) {
	if cfg.HasDatabase() {
		pgStore, err := store.NewPostgresStore(cfg)
		if err == nil {
			logger.Info("using PostgreSQL transaction store", "db_host", cfg.DBHost, "db_name", cfg.DBName)
			return pgStore, nil
		}

		return nil, fmt.Errorf("connect PostgreSQL: %w", err)
	}

	logger.Warn("DB env not configured; using in-memory store")
	return store.NewMemoryStore(), nil
}
