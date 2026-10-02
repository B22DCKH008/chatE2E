// Package auth owns accounts, device-scoped sessions and access-token validation (developer 1).
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"secureai/backend/internal/platform/httpapi"
)

type Principal struct{ UserID, DeviceID, SessionID string }

var ErrInvalidToken = errors.New("invalid access token")

// Verifier must validate signature, issuer, audience, expiry AND session/device revocation.
// It is implemented in step 2; never use an unverified JWT parser here.
type Verifier interface {
	Verify(context.Context, string) (Principal, error)
}

type principalKey struct{}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

func Require(verifier Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields := strings.Fields(r.Header.Get("Authorization"))
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpapi.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
			return
		}
		if verifier == nil {
			httpapi.Error(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "Authentication is unavailable")
			return
		}
		p, err := verifier.Verify(r.Context(), fields[1])
		if err != nil {
			if errors.Is(err, ErrInvalidToken) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				httpapi.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired credentials")
			} else {
				httpapi.Error(w, r, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "Authentication is unavailable")
			}
			return
		}
		if p.UserID == "" || p.DeviceID == "" || p.SessionID == "" {
			httpapi.Error(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid credentials")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}
