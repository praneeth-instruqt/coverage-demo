// Package config loads server settings from the environment.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	Addr           string
	JWTSecret      string
	TokenTTL       time.Duration
	HashIterations int
}

// Load reads settings using getenv (typically os.Getenv).
//
//	ADDR             listen address        (default ":8080")
//	JWT_SECRET       HMAC signing key      (>= 32 bytes; random per start if unset)
//	TOKEN_TTL        token lifetime        (default "24h")
//	HASH_ITERATIONS  PBKDF2 iterations     (default 600000)
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:      getenv("ADDR"),
		JWTSecret: getenv("JWT_SECRET"),
		TokenTTL:  24 * time.Hour,
	}
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.JWTSecret == "" {
		// Without a configured secret, tokens are only valid until restart.
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		cfg.JWTSecret = hex.EncodeToString(b)
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET must be at least 32 bytes long")
	}
	if v := getenv("TOKEN_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("TOKEN_TTL must be a positive duration, got %q", v)
		}
		cfg.TokenTTL = d
	}
	if v := getenv("HASH_ITERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("HASH_ITERATIONS must be a positive integer, got %q", v)
		}
		cfg.HashIterations = n
	}
	return cfg, nil
}
