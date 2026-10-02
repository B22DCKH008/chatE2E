package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type verifierFunc func(context.Context, string) (Principal, error)

func (f verifierFunc) Verify(ctx context.Context, token string) (Principal, error) {
	return f(ctx, token)
}

func TestRequire(t *testing.T) {
	valid := verifierFunc(func(context.Context, string) (Principal, error) { return Principal{"user", "device", "session"}, nil })
	for _, tt := range []struct {
		name, header string
		verifier     Verifier
		want         int
	}{
		{"no credentials", "", valid, 401},
		{"malformed", "Bearer one two", valid, 401},
		{"missing verifier", "Bearer token", nil, 503},
		{"expired", "Bearer token", verifierFunc(func(context.Context, string) (Principal, error) { return Principal{}, ErrInvalidToken }), 401},
		{"backend unavailable", "Bearer token", verifierFunc(func(context.Context, string) (Principal, error) { return Principal{}, errors.New("DB password") }), 503},
		{"incomplete principal", "Bearer token", verifierFunc(func(context.Context, string) (Principal, error) { return Principal{UserID: "user"}, nil }), 401},
		{"valid", "bearer token", valid, 204},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			h := Require(tt.verifier, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				p, ok := FromContext(r.Context())
				if !ok || p.DeviceID != "device" {
					t.Error("missing verified principal")
				}
				w.WriteHeader(204)
			}))
			r := httptest.NewRequest("POST", "/", nil)
			r.Header.Set("Authorization", tt.header)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want || called != (tt.want == 204) {
				t.Fatalf("status=%d handler_called=%v", w.Code, called)
			}
		})
	}
}
