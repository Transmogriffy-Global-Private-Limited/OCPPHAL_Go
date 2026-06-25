package config

import (
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Host     string
	Port     string
	APIKey   string
	LogLevel slog.Level

	DatabaseURL string

	DBName     string
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     int
	DBSSLMode  string
}

func Load() Config {
	return Config{
		Host:     env("F_SERVER_HOST", "127.0.0.1"),
		Port:     env("F_SERVER_PORT", "18080"),
		APIKey:   os.Getenv("API_KEY"),
		LogLevel: parseLogLevel(env("LOG_LEVEL", "info")),

		DatabaseURL: os.Getenv("DATABASE_URL"),

		DBName:     os.Getenv("DB_NAME"),
		DBUser:     os.Getenv("DB_USER"),
		DBPassword: os.Getenv("DB_PASSWORD"),
		DBHost:     env("DB_HOST", "127.0.0.1"),
		DBPort:     envInt("DB_PORT", 5432),
		DBSSLMode:  env("DB_SSLMODE", "disable"),
	}
}

func (c Config) ListenAddr() string {
	return net.JoinHostPort(c.Host, c.Port)
}

func (c Config) HasDatabase() bool {
	if strings.TrimSpace(c.DatabaseURL) != "" {
		return true
	}

	return strings.TrimSpace(c.DBName) != "" &&
		strings.TrimSpace(c.DBUser) != "" &&
		strings.TrimSpace(c.DBHost) != "" &&
		c.DBPort > 0
}

func env(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return value
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
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
