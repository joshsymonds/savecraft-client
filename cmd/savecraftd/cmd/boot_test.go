package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBootHandler_InitialState(t *testing.T) {
	bs := newBootStatus()

	rec := httptest.NewRecorder()
	bs.bootHandler(rec, httptest.NewRequest(http.MethodGet, "/boot", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != "starting" {
		t.Errorf("state = %q, want starting", resp.State)
	}
}

func TestBootHandler_Registering(t *testing.T) {
	bs := newBootStatus()
	bs.setState("registering")

	rec := httptest.NewRecorder()
	bs.bootHandler(rec, httptest.NewRequest(http.MethodGet, "/boot", nil))

	var resp struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != "registering" {
		t.Errorf("state = %q, want registering", resp.State)
	}
}

func TestBootHandler_Error(t *testing.T) {
	bs := newBootStatus()
	bs.setError("connection refused")

	rec := httptest.NewRecorder()
	bs.bootHandler(rec, httptest.NewRequest(http.MethodGet, "/boot", nil))

	var resp struct {
		State string `json:"state"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != "error" {
		t.Errorf("state = %q, want error", resp.State)
	}
	if resp.Error != "connection refused" {
		t.Errorf("error = %q, want connection refused", resp.Error)
	}
}

func TestLinkHandler_BeforeRegistration(t *testing.T) {
	bs := newBootStatus()

	rec := httptest.NewRecorder()
	bs.linkHandler(rec, httptest.NewRequest(http.MethodGet, "/link", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	var resp struct {
		Error string `json:"error"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != "starting" {
		t.Errorf("state = %q, want starting", resp.State)
	}
}

func TestLinkHandler_AfterRegistration(t *testing.T) {
	bs := newBootStatus()
	bs.setRegistered("482913", "https://savecraft.gg/link/482913", "2026-03-03T12:20:00Z")

	rec := httptest.NewRecorder()
	bs.linkHandler(rec, httptest.NewRequest(http.MethodGet, "/link", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		LinkCode  string `json:"linkCode"`
		LinkURL   string `json:"linkUrl"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.LinkCode != "482913" {
		t.Errorf("linkCode = %q, want 482913", resp.LinkCode)
	}
	if resp.LinkURL != "https://savecraft.gg/link/482913" {
		t.Errorf("linkUrl = %q, want https://savecraft.gg/link/482913", resp.LinkURL)
	}
	if resp.ExpiresAt != "2026-03-03T12:20:00Z" {
		t.Errorf("expiresAt = %q, want 2026-03-03T12:20:00Z", resp.ExpiresAt)
	}
}

func TestLinkHandler_AlreadyRegistered(t *testing.T) {
	bs := newBootStatus()
	bs.setState("running")

	rec := httptest.NewRecorder()
	bs.linkHandler(rec, httptest.NewRequest(http.MethodGet, "/link", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestBootStatus_BuildLinkURL(t *testing.T) {
	got := buildLinkURL("https://savecraft.gg", "482913")
	want := "https://savecraft.gg/link/482913"
	if got != want {
		t.Errorf("buildLinkURL = %q, want %q", got, want)
	}
}

func TestBootStatus_BuildLinkURL_TrailingSlash(t *testing.T) {
	got := buildLinkURL("https://savecraft.gg/", "482913")
	want := "https://savecraft.gg/link/482913"
	if got != want {
		t.Errorf("buildLinkURL = %q, want %q", got, want)
	}
}
