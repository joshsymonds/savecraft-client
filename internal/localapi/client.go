// Package localapi provides the daemon's localhost HTTP API server and client.
// The server exposes boot status, link info, and extensible endpoints.
// The client is used by the tray app to poll daemon state.
package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Client is an HTTP client for the daemon's local API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates a client targeting the given base URL (e.g. "http://localhost:9182").
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{},
	}
}

// Boot returns the daemon's current boot state.
func (c *Client) Boot(ctx context.Context) (*BootResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/boot", nil)
	if err != nil {
		return nil, fmt.Errorf("build boot request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("boot request: %w", err)
	}
	defer resp.Body.Close()

	var result BootResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&result); decErr != nil {
		return nil, fmt.Errorf("decode boot response: %w", decErr)
	}

	return &result, nil
}

// Link returns the device link code and URL. The HTTP status code is
// returned alongside the response to distinguish 200/404/503.
func (c *Client) Link(ctx context.Context) (*LinkResponse, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/link", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build link request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("link request: %w", err)
	}
	defer resp.Body.Close()

	var result LinkResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&result); decErr != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode link response: %w", decErr)
	}

	return &result, resp.StatusCode, nil
}

// Status returns the daemon's runtime status as raw JSON.
// The caller can unmarshal into the appropriate type.
func (c *Client) Status(ctx context.Context) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/status", nil)
	if err != nil {
		return nil, fmt.Errorf("build status request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("status request: %w", err)
	}
	defer resp.Body.Close()

	var raw json.RawMessage
	if decErr := json.NewDecoder(resp.Body).Decode(&raw); decErr != nil {
		return nil, fmt.Errorf("decode status response: %w", decErr)
	}

	return raw, nil
}
