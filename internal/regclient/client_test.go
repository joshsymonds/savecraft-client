package regclient_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/regclient"
)

func TestRegister(t *testing.T) {
	t.Parallel()

	t.Run("successful registration returns credentials", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", req.Method)
			}

			if req.URL.Path != "/api/v1/device/register" {
				t.Errorf("path = %s, want /api/v1/device/register", req.URL.Path)
			}

			var body struct {
				DeviceName string `json:"device_name"` //nolint:tagliatelle // wire format.
			}

			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}

			if body.DeviceName != "test-host" {
				t.Errorf("device_name = %q, want test-host", body.DeviceName)
			}

			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(rw).Encode(map[string]string{
				"device_uuid":          "d1e2f3a4-5678-90ab-cdef-1234567890ab",
				"token":                "dvt_testtoken123",
				"link_code":            "482913",
				"link_code_expires_at": "2026-03-03T12:20:00Z",
			}); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		result, err := regclient.Register(srv.URL, "test-host")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.DeviceUUID != "d1e2f3a4-5678-90ab-cdef-1234567890ab" {
			t.Errorf("device_uuid = %q, want d1e2f3a4-...", result.DeviceUUID)
		}

		if result.Token != "dvt_testtoken123" {
			t.Errorf("token = %q, want dvt_testtoken123", result.Token)
		}

		if result.LinkCode != "482913" {
			t.Errorf("link_code = %q, want 482913", result.LinkCode)
		}

		if result.LinkCodeExpiresAt != "2026-03-03T12:20:00Z" {
			t.Errorf("link_code_expires_at = %q, want 2026-03-03T12:20:00Z", result.LinkCodeExpiresAt)
		}
	})

	t.Run("server error returns wrapped error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusInternalServerError)

			if err := json.NewEncoder(rw).Encode(map[string]string{
				"error": "internal server error",
			}); err != nil {
				t.Fatalf("encode: %v", err)
			}
		}))
		defer srv.Close()

		_, err := regclient.Register(srv.URL, "test-host")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("network error returns wrapped error", func(t *testing.T) {
		t.Parallel()

		_, err := regclient.Register("http://localhost:1", "test-host")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("malformed JSON response returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusCreated)
			rw.Write([]byte("not json"))
		}))
		defer srv.Close()

		_, err := regclient.Register(srv.URL, "test-host")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
