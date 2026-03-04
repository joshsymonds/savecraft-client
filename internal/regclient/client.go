// Package regclient provides an HTTP client for device self-registration.
package regclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// RegisterResult holds the response from a successful device registration.
// JSON tags use snake_case to match the server API wire format.
type RegisterResult struct {
	DeviceUUID        string `json:"device_uuid"` //nolint:tagliatelle // wire format.
	Token             string `json:"token"`
	LinkCode          string `json:"link_code"`            //nolint:tagliatelle // wire format.
	LinkCodeExpiresAt string `json:"link_code_expires_at"` //nolint:tagliatelle // wire format.
}

// Register calls POST /api/v1/device/register to create a new device.
// The deviceName is a human-readable label (typically the hostname).
func Register(baseURL, deviceName string) (*RegisterResult, error) {
	payload, err := json.Marshal(map[string]string{"device_name": deviceName})
	if err != nil {
		return nil, fmt.Errorf("marshal register request: %w", err)
	}

	endpoint := baseURL + "/api/v1/device/register"

	//nolint:noctx // CLI one-shot call, no context needed.
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
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
