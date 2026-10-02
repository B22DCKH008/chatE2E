package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL": "postgres://user:secret@localhost/db?sslmode=disable",
		"REDIS_URL":    "redis://localhost:6379/0", "NATS_URL": "nats://localhost:4222",
	}
	for _, tt := range []struct {
		name, key, value string
		fail             bool
	}{
		{"defaults", "", "", false},
		{"missing database", "DATABASE_URL", "", true},
		{"invalid database", "DATABASE_URL", "postgres://user:SECRET%xx@host/db", true},
		{"wildcard CORS", "CORS_ALLOWED_ORIGINS", "*", true},
		{"origin with path", "CORS_ALLOWED_ORIGINS", "https://example.com/path", true},
		{"negative limit", "MAX_BODY_BYTES", "-1", true},
		{"invalid duration", "DEPENDENCY_TIMEOUT", "later", true},
		{"zero duration", "SHUTDOWN_TIMEOUT", "0s", true},
		{"invalid port", "HTTP_ADDR", ":99999", true},
		{"multiple origins", "CORS_ALLOWED_ORIGINS", "https://a.example,https://b.example", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := load(func(key string) string {
				if key == tt.key {
					return tt.value
				}
				return base[key]
			})
			if (err != nil) != tt.fail {
				t.Fatalf("error = %v, want failure %v", err, tt.fail)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET") {
				t.Fatal("configuration error leaked credentials")
			}
			if err == nil && cfg.MaxBodyBytes != 1048576 {
				t.Fatalf("unexpected default limit: %d", cfg.MaxBodyBytes)
			}
		})
	}
}
