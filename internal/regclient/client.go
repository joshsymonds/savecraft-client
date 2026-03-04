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
