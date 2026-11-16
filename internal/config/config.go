package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Addr            string
	DatabaseURL     string
	ShutdownTimeout time.Duration
	JWTSecret       string
	TokenTTL        time.Duration
	TrustedProxies  []string
}

func Load() (Config, error) {
	var proxies []string
	for _, p := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	d, err := time.ParseDuration(getenv("SHUTDOWN_TIMEOUT", "5s"))
	if err != nil {
		return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return Config{}, errors.New("JWT_SECRET is required and must be atleast 32 bytes")
	}
	ttl, err := time.ParseDuration(getenv("TOKEN_TTL", "1h"))
	if err != nil {
		return Config{}, fmt.Errorf("TOKEN_TTL: %w", err)
	}
	return Config{Addr: getenv("ADDR", ""), TrustedProxies: proxies, DatabaseURL: dsn, ShutdownTimeout: d, JWTSecret: secret, TokenTTL: ttl}, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
