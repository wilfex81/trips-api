package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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
	AuthBurst       int
	AuthRate        float64 // tokens added per second
	APIBurst        int
	APIRate         float64
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
	authBurst, err := getInt("AUTH_RATE_BURST", 5)
	if err != nil {
		return Config{}, err
	}
	authRate, err := getFloat("AUTH_RATE_PER_SEC", 0.1)
	if err != nil {
		return Config{}, err
	}
	apiBurst, err := getInt("API_RATE_BURST", 60)
	if err != nil {
		return Config{}, err
	}
	apiRate, err := getFloat("API_RATE_PER_SEC", 1)
	if err != nil {
		return Config{}, err
	}
	return Config{Addr: getenv("ADDR", ""), TrustedProxies: proxies, DatabaseURL: dsn, ShutdownTimeout: d, JWTSecret: secret, TokenTTL: ttl, AuthBurst: authBurst, AuthRate: authRate, APIBurst: apiBurst, APIRate: apiRate}, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) (int, error) {
	s := os.Getenv(key)
	if s == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func getFloat(key string, fallback float64) (float64, error) {
	s := os.Getenv(key)
	if s == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return f, nil
}
