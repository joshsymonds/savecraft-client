package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joshsymonds/savecraft.gg/internal/localapi"
)

func TestUpdatePluginsCommand(t *testing.T) {
	t.Parallel()

	t.Run("succeeds with updated plugins", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", r.Method)
			}
			if r.URL.Path != "/update-plugins" {
				t.Errorf("path = %s, want /update-plugins", r.URL.Path)
			}
			json.NewEncoder(w).Encode(localapi.UpdatePluginsResponse{
				Updated: []string{"d2r", "rimworld"},
			})
		}))
		defer srv.Close()

		port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
		cmd := buildUpdatePluginsCommand(port)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})

	t.Run("succeeds with none updated", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(localapi.UpdatePluginsResponse{
				Updated: []string{},
			})
		}))
		defer srv.Close()

		port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
		cmd := buildUpdatePluginsCommand(port)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})

	t.Run("returns error on failure", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(localapi.UpdatePluginsResponse{
				Error: "plugin update failed",
			})
		}))
		defer srv.Close()

		port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
		cmd := buildUpdatePluginsCommand(port)

		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
