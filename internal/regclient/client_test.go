package regclient_test

import (
	"context"
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

			if req.URL.Path != "/api/v1/source/register" {
				t.Errorf("path = %s, want /api/v1/source/register", req.URL.Path)
			}

			var body struct {
				SourceName string `json:"source_name"`
			}

			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}

			if body.SourceName != "test-host" {
				t.Errorf("source_name = %q, want test-host", body.SourceName)
			}

			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(rw).Encode(map[string]string{
				"source_uuid":          "d1e2f3a4-5678-90ab-cdef-1234567890ab",
				"token":                "dvt_testtoken123",
				"link_code":            "482913",
				"link_code_expires_at": "2026-03-03T12:20:00Z",
			}); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		result, err := regclient.Register(context.Background(), srv.URL, "test-host")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SourceUUID != "d1e2f3a4-5678-90ab-cdef-1234567890ab" {
			t.Errorf("source_uuid = %q, want d1e2f3a4-...", result.SourceUUID)
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

		_, err := regclient.Register(context.Background(), srv.URL, "test-host")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("network error returns wrapped error", func(t *testing.T) {
		t.Parallel()

		_, err := regclient.Register(context.Background(), "http://localhost:1", "test-host")
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

		_, err := regclient.Register(context.Background(), srv.URL, "test-host")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestStatus(t *testing.T) {
	t.Parallel()

	t.Run("returns linked status", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", req.Method)
			}

			if req.URL.Path != "/api/v1/source/status" {
				t.Errorf("path = %s, want /api/v1/source/status", req.URL.Path)
			}

			auth := req.Header.Get("Authorization")
			if auth != "Bearer sct_testtoken" {
				t.Errorf("Authorization = %q, want Bearer sct_testtoken", auth)
			}

			rw.Header().Set("Content-Type", "application/json")
			json.NewEncoder(rw).Encode(map[string]any{
				"linked": true,
			})
		}))
		defer srv.Close()

		result, err := regclient.Status(context.Background(), srv.URL, "sct_testtoken")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result.Linked {
			t.Error("expected linked=true")
		}
	})

	t.Run("returns unlinked status with code", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			json.NewEncoder(rw).Encode(map[string]any{
				"linked":                false,
				"link_code":             "123456",
				"link_code_expires_at":  "2026-03-03T12:20:00Z",
			})
		}))
		defer srv.Close()

		result, err := regclient.Status(context.Background(), srv.URL, "sct_testtoken")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Linked {
			t.Error("expected linked=false")
		}

		if result.LinkCode != "123456" {
			t.Errorf("link_code = %q, want 123456", result.LinkCode)
		}
	})

	t.Run("server error returns wrapped error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := regclient.Status(context.Background(), srv.URL, "sct_testtoken")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestUnlink(t *testing.T) {
	t.Parallel()

	t.Run("successful unlink returns link code", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", req.Method)
			}

			if req.URL.Path != "/api/v1/source/unlink" {
				t.Errorf("path = %s, want /api/v1/source/unlink", req.URL.Path)
			}

			auth := req.Header.Get("Authorization")
			if auth != "Bearer sct_testtoken" {
				t.Errorf("Authorization = %q, want Bearer sct_testtoken", auth)
			}

			rw.Header().Set("Content-Type", "application/json")

			if err := json.NewEncoder(rw).Encode(map[string]string{
				"link_code":            "123456",
				"link_code_expires_at": "2026-03-03T12:20:00Z",
			}); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		result, err := regclient.Unlink(context.Background(), srv.URL, "sct_testtoken")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.LinkCode != "123456" {
			t.Errorf("link_code = %q, want 123456", result.LinkCode)
		}

		if result.ExpiresAt != "2026-03-03T12:20:00Z" {
			t.Errorf("expires_at = %q, want 2026-03-03T12:20:00Z", result.ExpiresAt)
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

		_, err := regclient.Unlink(context.Background(), srv.URL, "sct_testtoken")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestRefreshLinkCode(t *testing.T) {
	t.Parallel()

	t.Run("successful refresh returns link code", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", req.Method)
			}

			if req.URL.Path != "/api/v1/source/link-code" {
				t.Errorf("path = %s, want /api/v1/source/link-code", req.URL.Path)
			}

			auth := req.Header.Get("Authorization")
			if auth != "Bearer sct_testtoken" {
				t.Errorf("Authorization = %q, want Bearer sct_testtoken", auth)
			}

			rw.Header().Set("Content-Type", "application/json")

			if err := json.NewEncoder(rw).Encode(map[string]string{
				"link_code":  "654321",
				"expires_at": "2026-03-03T12:40:00Z",
			}); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		result, err := regclient.RefreshLinkCode(context.Background(), srv.URL, "sct_testtoken")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.LinkCode != "654321" {
			t.Errorf("link_code = %q, want 654321", result.LinkCode)
		}

		if result.ExpiresAt != "2026-03-03T12:40:00Z" {
			t.Errorf("expires_at = %q, want 2026-03-03T12:40:00Z", result.ExpiresAt)
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

		_, err := regclient.RefreshLinkCode(context.Background(), srv.URL, "sct_testtoken")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
