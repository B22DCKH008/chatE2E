package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment       string
	HTTPAddr          string
	DatabaseURL       string
	RedisURL          string
	NATSURL           string
	AllowedOrigins    []string
	LogLevel          slog.Level
	MaxBodyBytes      int64
	DependencyTimeout time.Duration
	ShutdownTimeout   time.Duration
}

func Load() (Config, error) { return load(os.Getenv) }

func load(get func(string) string) (Config, error) {
	value := func(key, fallback string) string {
		if v := strings.TrimSpace(get(key)); v != "" {
			return v
		}
		return fallback
	}
	c := Config{
		Environment: value("APP_ENV", "development"),
		HTTPAddr:    value("HTTP_ADDR", ":8080"),
		DatabaseURL: value("DATABASE_URL", ""),
		RedisURL:    value("REDIS_URL", ""),
		NATSURL:     value("NATS_URL", ""),
	}
	if c.Environment != "development" && c.Environment != "test" && c.Environment != "production" {
		return c, fmt.Errorf("APP_ENV must be development, test or production")
	}
	_, port, err := net.SplitHostPort(c.HTTPAddr)
	if err != nil {
		return c, fmt.Errorf("HTTP_ADDR must be host:port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return c, fmt.Errorf("HTTP_ADDR has an invalid port")
	}
	for _, item := range []struct {
		key, raw string
		schemes  []string
	}{
		{"DATABASE_URL", c.DatabaseURL, []string{"postgres", "postgresql"}},
		{"REDIS_URL", c.RedisURL, []string{"redis", "rediss"}},
		{"NATS_URL", c.NATSURL, []string{"nats", "tls"}},
	} {
		u, parseErr := url.Parse(item.raw)
		valid := parseErr == nil && u.Hostname() != ""
		matched := false
		if parseErr == nil {
			for _, scheme := range item.schemes {
				matched = matched || u.Scheme == scheme
			}
		}
		if !valid || !matched {
			return c, fmt.Errorf("%s must be a valid service URL", item.key)
		}
	}
	if err := c.LogLevel.UnmarshalText([]byte(value("LOG_LEVEL", "INFO"))); err != nil {
		return c, fmt.Errorf("LOG_LEVEL must be DEBUG, INFO, WARN or ERROR")
	}
	for _, origin := range strings.Split(value("CORS_ALLOWED_ORIGINS", "http://localhost:5173"), ",") {
		origin = strings.TrimSpace(origin)
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return c, fmt.Errorf("CORS_ALLOWED_ORIGINS requires exact http(s) origins without paths")
		}
		c.AllowedOrigins = append(c.AllowedOrigins, origin)
	}
	c.MaxBodyBytes, err = strconv.ParseInt(value("MAX_BODY_BYTES", "1048576"), 10, 64)
	if err != nil || c.MaxBodyBytes < 1024 || c.MaxBodyBytes > 16*1024*1024 {
		return c, fmt.Errorf("MAX_BODY_BYTES must be between 1024 and 16777216")
	}
	for _, item := range []struct {
		key, fallback string
		target        *time.Duration
	}{
		{"DEPENDENCY_TIMEOUT", "3s", &c.DependencyTimeout},
		{"SHUTDOWN_TIMEOUT", "10s", &c.ShutdownTimeout},
	} {
		d, err := time.ParseDuration(value(item.key, item.fallback))
		if err != nil || d <= 0 || d > time.Minute {
			return c, fmt.Errorf("%s must be > 0 and <= 1m", item.key)
		}
		*item.target = d
	}
	return c, nil
}
