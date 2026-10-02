package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type requestIDKey struct{}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	// Interim responses do not commit the final response status.
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func Middleware(logger *slog.Logger, origins []string, maxBody int64, next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idBytes := make([]byte, 16)
		_, _ = rand.Read(idBytes)
		id := hex.EncodeToString(idBytes)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		rw := &responseWriter{ResponseWriter: w}
		started := time.Now()
		defer func() {
			// Never log request paths, query parameters, bodies, headers or panic values.
			// A future route template may contain names but must never contain IDs.
			status := rw.status
			if status == 0 {
				status = http.StatusOK
			}
			logger.Info("http_request", "request_id", id, "status", status, "duration_ms", time.Since(started).Milliseconds())
		}()
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				logger.Error("http_panic", "request_id", id)
				if rw.status != 0 {
					panic(http.ErrAbortHandler)
				}
				Error(rw, r, http.StatusInternalServerError, "INTERNAL_ERROR", "An internal error occurred")
			}
		}()
		w.Header().Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" {
			if !allowed[origin] {
				Error(rw, r, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "Origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
				method := r.Header.Get("Access-Control-Request-Method")
				if !strings.Contains(",GET,POST,PUT,PATCH,DELETE,HEAD,", ","+method+",") {
					Error(rw, r, http.StatusForbidden, "CORS_METHOD_NOT_ALLOWED", "Method is not allowed")
					return
				}
				for _, header := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
					switch strings.ToLower(strings.TrimSpace(header)) {
					case "", "authorization", "content-type", "idempotency-key":
					default:
						Error(rw, r, http.StatusForbidden, "CORS_HEADER_NOT_ALLOWED", "Header is not allowed")
						return
					}
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, HEAD")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
				w.Header().Set("Access-Control-Max-Age", "600")
				rw.WriteHeader(http.StatusNoContent)
				return
			}
		}
		if r.ContentLength > maxBody {
			Error(rw, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Request body exceeds the limit")
			return
		}
		r.Body = http.MaxBytesReader(rw, r.Body, maxBody)
		next.ServeHTTP(rw, r)
	})
}
