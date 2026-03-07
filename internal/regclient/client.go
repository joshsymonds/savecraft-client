// Package regclient provides an HTTP client for source self-registration.
package regclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrSourceNotFound is returned when the server reports that the source
// does not exist (HTTP 404). This typically means the database was reset
// and the locally-stored credentials are stale.
var ErrSourceNotFound = errors.New("source not found")

// RegisterResult holds the response from a successful source registration.
// JSON tags use snake_case to match the server API wire format.
type RegisterResult struct {
	SourceUUID        string `json:"source_uuid"`
	Token             string `json:"source_token"`
	LinkCode          string `json:"link_code"`
	LinkCodeExpiresAt string `json:"link_code_expires_at"`
}

// LinkCodeResult holds a link code and its expiration from the server.
type LinkCodeResult struct {
	LinkCode  string `json:"link_code"`
	ExpiresAt string `json:"expires_at"`
}

// StatusResult holds the response from GET /api/v1/source/status.
type StatusResult struct {
	Linked            bool   `json:"linked"`
	LinkCode          string `json:"link_code,omitempty"`
	LinkCodeExpiresAt string `json:"link_code_expires_at,omitempty"`
}

// Status calls GET /api/v1/source/status to check whether the source is linked.
func Status(ctx context.Context, baseURL, authToken string) (*StatusResult, error) {
	endpoint := baseURL + "/api/v1/source/status"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create status request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("status request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrSourceNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status request failed with status %d", resp.StatusCode)
	}

	var result StatusResult

	if decErr := json.NewDecoder(resp.Body).Decode(&result); decErr != nil {
		return nil, fmt.Errorf("decode status response: %w", decErr)
	}

	return &result, nil
}

// Unlink calls POST /api/v1/source/unlink to clear the user association
// and generate a fresh link code. The source identity is preserved.
func Unlink(ctx context.Context, baseURL, authToken string) (*LinkCodeResult, error) {
	return authedPost(ctx, baseURL+"/api/v1/source/unlink", authToken)
}

// Deregister calls POST /api/v1/source/deregister to permanently delete the
// source and all associated data (D1, SourceHub DO, UserHub state).
func Deregister(ctx context.Context, baseURL, authToken string) error {
	endpoint := baseURL + "/api/v1/source/deregister"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create deregister request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("deregister request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}

		if decErr := json.NewDecoder(resp.Body).Decode(&errBody); decErr == nil && errBody.Error != "" {
			return fmt.Errorf("deregister failed (%d): %s", resp.StatusCode, errBody.Error)
		}

		return fmt.Errorf("deregister failed with status %d", resp.StatusCode)
	}

	return nil
}

// RefreshLinkCode calls POST /api/v1/source/link-code to generate a new
// link code with a fresh TTL, replacing any existing code.
func RefreshLinkCode(ctx context.Context, baseURL, authToken string) (*LinkCodeResult, error) {
	return authedPost(ctx, baseURL+"/api/v1/source/link-code", authToken)
}

// authedPost makes an authenticated POST request and decodes a LinkCodeResult.
func authedPost(ctx context.Context, endpoint, authToken string) (*LinkCodeResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", endpoint, err)
	}

	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}

		if decErr := json.NewDecoder(resp.Body).Decode(&errBody); decErr == nil && errBody.Error != "" {
			return nil, fmt.Errorf("%s failed (%d): %s", endpoint, resp.StatusCode, errBody.Error)
		}

		return nil, fmt.Errorf("%s failed with status %d", endpoint, resp.StatusCode)
	}

	// The server returns link_code in both endpoints. The expiry field
	// differs: "link_code_expires_at" for unlink, "expires_at" for link-code.
	// Decode into a flexible struct to handle both.
	var raw struct {
		LinkCode          string `json:"link_code"`
		ExpiresAt         string `json:"expires_at"`
		LinkCodeExpiresAt string `json:"link_code_expires_at"`
	}

	if decErr := json.NewDecoder(resp.Body).Decode(&raw); decErr != nil {
		return nil, fmt.Errorf("decode response from %s: %w", endpoint, decErr)
	}

	expiresAt := raw.ExpiresAt
	if expiresAt == "" {
		expiresAt = raw.LinkCodeExpiresAt
	}

	return &LinkCodeResult{
		LinkCode:  raw.LinkCode,
		ExpiresAt: expiresAt,
	}, nil
}
