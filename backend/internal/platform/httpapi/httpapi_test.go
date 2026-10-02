package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"secureai/backend/internal/platform/config"
)

func testConfig() config.Config {
	return config.Config{AllowedOrigins: []string{"http://localhost:5173"}, MaxBodyBytes: 1024, DependencyTimeout: 30 * time.Millisecond}
}

func TestHealthAndErrors(t *testing.T) {
	for _, tt := range []struct {
		name, method, path string
		checks             map[string]Check
		want               int
	}{
		{"live", "GET", "/health/live", nil, 200},
		{"method", "POST", "/health/live", nil, 405},
		{"missing route", "GET", "/v1/messages", nil, 404},
		{"ready", "GET", "/health/ready", map[string]Check{"db": func(context.Context) error { return nil }}, 200},
		{"unavailable", "GET", "/health/ready", map[string]Check{"db": func(context.Context) error { return errors.New("secret DSN") }}, 503},
		{"no checks", "GET", "/health/ready", nil, 503},
		{"timeout", "GET", "/health/ready", map[string]Check{"db": func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}, 503},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			router := NewRouter(testConfig(), slog.New(slog.NewJSONHandler(&logs, nil)), tt.checks)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
			if w.Code != tt.want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if !json.Valid(w.Body.Bytes()) {
				t.Fatal("response must be JSON")
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("dependency error leaked")
			}
			if len(w.Header().Get("X-Request-ID")) != 32 {
				t.Fatal("missing request ID")
			}
			if tt.want == 404 || tt.want == 405 {
				var body ErrorResponse
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				if body.Error.RequestID != w.Header().Get("X-Request-ID") {
					t.Fatal("request IDs differ")
				}
			}
		})
	}
}

func TestMiddlewarePrivacyAndRecovery(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := Middleware(logger, nil, 1024, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("SECRET_PANIC") }))
	r := httptest.NewRequest("POST", "/SECRET_PATH?token=SECRET_QUERY", strings.NewReader("SECRET_BODY"))
	r.Header.Set("Authorization", "Bearer SECRET_TOKEN")
	r.Header.Set("X-Request-ID", "SECRET_INJECTED_ID")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatalf("status %d", w.Code)
	}
	if strings.Contains(logs.String()+w.Body.String(), "SECRET") {
		t.Fatal("sensitive data was logged or reflected")
	}
}

func TestCORS(t *testing.T) {
	for _, tt := range []struct {
		origin, method, headers string
		want                    int
	}{
		{"http://localhost:5173", "POST", "authorization, content-type", 204},
		{"https://evil.example", "POST", "", 403},
		{"http://localhost:5173", "TRACE", "", 403},
		{"http://localhost:5173", "POST", "x-unsupported", 403},
	} {
		r := httptest.NewRequest("OPTIONS", "/v1/messages", nil)
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Access-Control-Request-Method", tt.method)
		r.Header.Set("Access-Control-Request-Headers", tt.headers)
		w := httptest.NewRecorder()
		NewRouter(testConfig(), slog.Default(), nil).ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Fatalf("origin %s: status %d", tt.origin, w.Code)
		}
		if tt.want == 204 && w.Header().Get("Access-Control-Allow-Origin") != tt.origin {
			t.Fatal("origin not echoed")
		}
		if tt.origin == "https://evil.example" && w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("untrusted origin allowed")
		}
	}
}

func TestDecodeJSON(t *testing.T) {
	for _, tt := range []struct {
		name, contentType, body string
		chunked                 bool
		want                    int
	}{
		{"valid", "application/json", `{"value":"ok"}`, false, 204},
		{"unknown field", "application/json", `{"unexpected":"SECRET"}`, false, 400},
		{"trailing JSON", "application/json", `{"value":"a"}{"value":"b"}`, false, 400},
		{"null", "application/json", `null`, false, 400},
		{"wrong type", "application/json", `[]`, false, 400},
		{"empty", "application/json", ``, false, 400},
		{"wrong media", "text/plain", `{}`, false, 415},
		{"known oversized", "application/json", `{"value":"` + strings.Repeat("x", 2048) + `"}`, false, 413},
		{"chunked oversized", "application/json", `{"value":"` + strings.Repeat("x", 2048) + `"}`, true, 413},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := Middleware(slog.Default(), nil, 1024, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Value string `json:"value"`
				}
				if DecodeJSON(w, r, &body) {
					w.WriteHeader(204)
				}
			}))
			r := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			r.Header.Set("Content-Type", tt.contentType)
			if tt.chunked {
				r.ContentLength = -1
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "SECRET") {
				t.Fatal("input leaked")
			}
		})
	}
}
