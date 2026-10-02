package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"secureai/backend/internal/platform/config"
)

type Check func(context.Context) error

type HealthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// Business routes are added here when their modules are implemented.
// No planned route reports fake success or bypasses authentication.
func NewRouter(cfg config.Config, logger *slog.Logger, checks map[string]Check) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", getOnly(func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, HealthResponse{Status: "ok"})
	}))
	mux.HandleFunc("/health/ready", getOnly(readiness(checks, cfg.DependencyTimeout)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		Error(w, r, http.StatusNotFound, "NOT_FOUND", "Route not found")
	})
	return Middleware(logger, cfg.AllowedOrigins, cfg.MaxBodyBytes, mux)
}

func getOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			Error(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}
		next(w, r)
	}
}

func readiness(checks map[string]Check, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		type result struct {
			name string
			err  error
		}
		results := make(chan result, len(checks))
		var wg sync.WaitGroup
		for name, check := range checks {
			wg.Add(1)
			go func() { defer wg.Done(); results <- result{name, check(ctx)} }()
		}
		go func() { wg.Wait(); close(results) }()
		body := HealthResponse{Status: "ready", Checks: make(map[string]string, len(checks))}
		status := http.StatusOK
		for name := range checks {
			body.Checks[name] = "unavailable"
		}
		// An empty checker set must never make a production server ready.
		if len(checks) == 0 {
			body.Status = "not_ready"
			status = http.StatusServiceUnavailable
		}
		for remaining := len(checks); remaining > 0; remaining-- {
			select {
			case result := <-results:
				if result.err == nil {
					body.Checks[result.name] = "ok"
				} else {
					status = http.StatusServiceUnavailable
				}
			case <-ctx.Done():
				status = http.StatusServiceUnavailable
				remaining = 1
			}
		}
		if status != http.StatusOK {
			body.Status = "not_ready"
		}
		JSON(w, status, body)
	}
}
