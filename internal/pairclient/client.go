// Package pairclient provides an HTTP client for the device pairing claim endpoint.
package pairclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// ClaimResult holds the response from a successful pairing code claim.
type ClaimResult struct {
	Token     string `json:"token"`
	ServerURL string `json:"serverUrl"`
}

// ClaimCode exchanges a 6-digit pairing code for an API token and server URL.
func ClaimCode(baseURL, code string) (*ClaimResult, error) {
	payload, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		return nil, fmt.Errorf("marshal claim request: %w", err)
	}

	endpoint := baseURL + "/api/v1/pair/claim"

	//nolint:noctx // CLI one-shot call, no context needed.
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("claim request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}

		if decErr := json.NewDecoder(resp.Body).Decode(&errBody); decErr == nil && errBody.Error != "" {
			return nil, fmt.Errorf("claim failed (%d): %s", resp.StatusCode, errBody.Error)
		}

		return nil, fmt.Errorf("claim failed with status %d", resp.StatusCode)
	}

	var result ClaimResult

	decErr := json.NewDecoder(resp.Body).Decode(&result)
	if decErr != nil {
		return nil, fmt.Errorf("decode claim response: %w", decErr)
	}

	return &result, nil
}
