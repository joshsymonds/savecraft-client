// Package regclient provides an HTTP client for source self-registration.
package regclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// RegisterResult holds the response from a successful source registration.
// JSON tags use snake_case to match the server API wire format.
type RegisterResult struct {
	SourceUUID        string `json:"source_uuid"`
	Token             string `json:"token"`
	LinkCode          string `json:"link_code"`
	LinkCodeExpiresAt string `json:"link_code_expires_at"`
}

// LinkCodeResult holds a link code and its expiration from the server.
type LinkCodeResult struct {
	LinkCode  string `json:"link_code"`
	ExpiresAt string `json:"expires_at"`
}

// Register calls POST /api/v1/source/register to create a new source.
// The sourceName is a human-readable label (typically the hostname).
func Register(ctx context.Context, baseURL, sourceName string) (*RegisterResult, error) {
	payload, err := json.Marshal(map[string]string{"source_name": sourceName})
	if err != nil {
		return nil, fmt.Errorf("marshal register request: %w", err)
	}

	endpoint := baseURL + "/api/v1/source/register"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create register request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errBody struct {
			Error string `json:"error"`
		}

		if decErr := json.NewDecoder(resp.Body).Decode(&errBody); decErr == nil && errBody.Error != "" {
			return nil, fmt.Errorf("register failed (%d): %s", resp.StatusCode, errBody.Error)
		}

		return nil, fmt.Errorf("register failed with status %d", resp.StatusCode)
	}

	var result RegisterResult

	if decErr := json.NewDecoder(resp.Body).Decode(&result); decErr != nil {
		return nil, fmt.Errorf("decode register response: %w", decErr)
	}

	return &result, nil
}

// Unlink calls POST /api/v1/source/unlink to clear the user association
// and generate a fresh link code. The source identity is preserved.
func Unlink(ctx context.Context, baseURL, authToken string) (*LinkCodeResult, error) {
	return authedPost(ctx, baseURL+"/api/v1/source/unlink", authToken)
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
