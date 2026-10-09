package config

import (
	"strings"
	"testing"
	"time"
)

const secret = "0123456789abcdef0123456789abcdef"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"JWT_SECRET": secret}))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Addr: ":8080", JWTSecret: secret, TokenTTL: 24 * time.Hour}
	if cfg != want {
		t.Errorf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"ADDR":            "127.0.0.1:9000",
		"JWT_SECRET":      secret,
		"TOKEN_TTL":       "15m",
		"HASH_ITERATIONS": "1000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Addr: "127.0.0.1:9000", JWTSecret: secret, TokenTTL: 15 * time.Minute, HashIterations: 1000}
	if cfg != want {
		t.Errorf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := map[string]struct {
		env  map[string]string
		want string
	}{
		"missing secret":  {map[string]string{}, "JWT_SECRET"},
		"short secret":    {map[string]string{"JWT_SECRET": "short"}, "JWT_SECRET"},
		"bad ttl":         {map[string]string{"JWT_SECRET": secret, "TOKEN_TTL": "soon"}, "TOKEN_TTL"},
		"negative ttl":    {map[string]string{"JWT_SECRET": secret, "TOKEN_TTL": "-1h"}, "TOKEN_TTL"},
		"bad iterations":  {map[string]string{"JWT_SECRET": secret, "HASH_ITERATIONS": "many"}, "HASH_ITERATIONS"},
		"zero iterations": {map[string]string{"JWT_SECRET": secret, "HASH_ITERATIONS": "0"}, "HASH_ITERATIONS"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Load(env(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want mention of %s", err, tt.want)
			}
		})
	}
}
