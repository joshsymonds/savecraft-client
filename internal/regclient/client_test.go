package regclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/regclient"
)

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
				"linked":               false,
				"link_code":            "123456",
				"link_code_expires_at": "2026-03-03T12:20:00Z",
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

	t.Run("404 returns ErrSourceNotFound", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusNotFound)
			json.NewEncoder(rw).Encode(map[string]string{"error": "Source not found"})
		}))
		defer srv.Close()

		_, err := regclient.Status(context.Background(), srv.URL, "sct_testtoken")
		if !errors.Is(err, regclient.ErrSourceNotFound) {
			t.Errorf("expected ErrSourceNotFound, got %v", err)
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

		if errors.Is(err, regclient.ErrSourceNotFound) {
			t.Error("500 should not be ErrSourceNotFound")
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

func TestDeregister(t *testing.T) {
	t.Parallel()

	t.Run("successful deregister", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", req.Method)
			}

			if req.URL.Path != "/api/v1/source/deregister" {
				t.Errorf("path = %s, want /api/v1/source/deregister", req.URL.Path)
			}

			auth := req.Header.Get("Authorization")
			if auth != "Bearer sct_testtoken" {
				t.Errorf("Authorization = %q, want Bearer sct_testtoken", auth)
			}

			rw.Header().Set("Content-Type", "application/json")

			if err := json.NewEncoder(rw).Encode(map[string]bool{"ok": true}); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		err := regclient.Deregister(context.Background(), srv.URL, "sct_testtoken")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("server error with body includes message", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusInternalServerError)

			if err := json.NewEncoder(rw).Encode(map[string]string{
				"error": "source not found",
			}); err != nil {
				t.Fatalf("encode: %v", err)
			}
		}))
		defer srv.Close()

		err := regclient.Deregister(context.Background(), srv.URL, "sct_testtoken")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		want := "deregister failed (500): source not found"
		if err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
	})

	t.Run("server error without body returns status", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		err := regclient.Deregister(context.Background(), srv.URL, "sct_testtoken")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("connection error returns error", func(t *testing.T) {
		t.Parallel()

		err := regclient.Deregister(context.Background(), "http://127.0.0.1:1", "sct_testtoken")
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
