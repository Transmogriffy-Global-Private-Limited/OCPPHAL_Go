package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	addr := env("MOCK_HOOK_ADDR", "127.0.0.1:19090")

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339Nano),
		})
	})

	registerStartHook(mux, logger, "/users/checkstartresponse", "main")
	registerCompletedHook(mux, logger, "/users/deductcalculate", "main")

	registerStartHook(mux, logger, "/single/checkstartresponse", "single")
	registerCompletedHook(mux, logger, "/single/deductcalculate", "single")

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("starting mock hook server", "addr", addr)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("mock hook server failed", "error", err)
		os.Exit(1)
	}
}

func registerStartHook(mux *http.ServeMux, logger *slog.Logger, path string, mode string) {
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, _ := io.ReadAll(r.Body)

		logger.Info(
			"received start transaction hook",
			"mode", mode,
			"path", r.URL.Path,
			"apiauthkey", r.Header.Get("apiauthkey"),
			"body", string(body),
		)

		writeJSON(w, http.StatusOK, map[string]any{
			"max_kwh": envFloat("MOCK_START_MAX_KWH", 7.5),
		})
	})
}

func registerCompletedHook(mux *http.ServeMux, logger *slog.Logger, path string, mode string) {
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, _ := io.ReadAll(r.Body)

		logger.Info(
			"received completed transaction hook",
			"mode", mode,
			"path", r.URL.Path,
			"apiauthkey", r.Header.Get("apiauthkey"),
			"body", string(body),
		)

		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
		})
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}

	return parsed
}
