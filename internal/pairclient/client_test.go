package pairclient_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/pairclient"
)

func TestClaimCode(t *testing.T) {
	t.Parallel()

	t.Run("successful claim returns token and server URL", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", req.Method)
			}

			if req.URL.Path != "/api/v1/pair/claim" {
				t.Errorf("path = %s, want /api/v1/pair/claim", req.URL.Path)
			}

			var body struct {
				Code string `json:"code"`
			}

			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}

			if body.Code != "123456" {
				t.Errorf("code = %s, want 123456", body.Code)
			}

			rw.Header().Set("Content-Type", "application/json")

			if err := json.NewEncoder(rw).Encode(map[string]string{
				"token":     "sav_testtoken",
				"serverUrl": "https://api.savecraft.gg",
			}); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		result, err := pairclient.ClaimCode(srv.URL, "123456")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Token != "sav_testtoken" {
			t.Errorf("token = %q, want %q", result.Token, "sav_testtoken")
		}

		if result.ServerURL != "https://api.savecraft.gg" {
			t.Errorf("serverUrl = %q, want %q", result.ServerURL, "https://api.savecraft.gg")
		}
	})

	t.Run("invalid code returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusUnauthorized)

			if err := json.NewEncoder(rw).Encode(map[string]string{
				"error": "Invalid or expired code",
			}); err != nil {
				t.Fatalf("encode: %v", err)
			}
		}))
		defer srv.Close()

		_, err := pairclient.ClaimCode(srv.URL, "000000")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("rate limited returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusTooManyRequests)

			if err := json.NewEncoder(rw).Encode(map[string]string{
				"error": "Too many attempts",
			}); err != nil {
				t.Fatalf("encode: %v", err)
			}
		}))
		defer srv.Close()

		_, err := pairclient.ClaimCode(srv.URL, "999999")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("network error returns wrapped error", func(t *testing.T) {
		t.Parallel()

		_, err := pairclient.ClaimCode("http://localhost:1", "123456")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("malformed JSON response returns error", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.Write([]byte("not json"))
		}))
		defer srv.Close()

		_, err := pairclient.ClaimCode(srv.URL, "123456")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
