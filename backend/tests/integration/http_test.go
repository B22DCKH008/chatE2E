package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"secureai/backend/internal/platform/httpapi"
)

// Exercise the built server over the Compose network, not an in-process router.
func TestHTTPFoundation(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 with the development services running")
	}
	baseURL := strings.TrimRight(os.Getenv("API_BASE_URL"), "/")
	if baseURL == "" {
		t.Fatal("API_BASE_URL is required for integration tests")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	for _, test := range []struct {
		path, status string
	}{
		{"/health/live", "ok"},
		{"/health/ready", "ready"},
	} {
		t.Run(test.path, func(t *testing.T) {
			response, err := client.Get(baseURL + test.path)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("unexpected HTTP status: %d", response.StatusCode)
			}
			var body httpapi.HealthResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Status != test.status {
				t.Fatalf("status = %q, want %q", body.Status, test.status)
			}
			if test.path == "/health/ready" {
				for _, name := range []string{"postgres", "redis", "jetstream", "schema"} {
					if body.Checks[name] != "ok" {
						t.Errorf("dependency %s = %q, want ok", name, body.Checks[name])
					}
				}
			}
			if response.Header.Get("X-Request-ID") == "" {
				t.Error("missing request ID")
			}
		})
	}
	t.Run("HEAD has no body", func(t *testing.T) {
		response, err := client.Head(baseURL + "/health/live")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || len(body) != 0 || response.StatusCode != http.StatusOK {
			t.Fatalf("HEAD status=%d bytes=%d err=%v", response.StatusCode, len(body), err)
		}
	})
	t.Run("planned route remains unavailable", func(t *testing.T) {
		response, err := client.Get(baseURL + "/v1/messages")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var body httpapi.ErrorResponse
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusNotFound || body.Error.Code != "NOT_FOUND" {
			t.Fatalf("status=%d code=%s", response.StatusCode, body.Error.Code)
		}
		if body.Error.RequestID == "" || body.Error.RequestID != response.Header.Get("X-Request-ID") {
			t.Error("error request ID does not match header")
		}
	})
}
