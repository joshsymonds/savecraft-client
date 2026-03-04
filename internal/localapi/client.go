// Package localapi provides the daemon's localhost HTTP API server and client.
// The server exposes daemon state, link info, logs, and control endpoints (shutdown, restart).
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

// Link returns the source link code and URL. The HTTP status code is
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

// Logs returns the daemon's captured log entries.
func (c *Client) Logs(ctx context.Context) ([]LogEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/logs", nil)
	if err != nil {
		return nil, fmt.Errorf("build logs request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("logs request: %w", err)
	}
	defer resp.Body.Close()

	var result LogsResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&result); decErr != nil {
		return nil, fmt.Errorf("decode logs response: %w", decErr)
	}

	return result.Entries, nil
}

// Shutdown requests a graceful daemon shutdown via POST /shutdown.
func (c *Client) Shutdown(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/shutdown", nil)
	if err != nil {
		return fmt.Errorf("build shutdown request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("shutdown request: %w", err)
	}
	defer resp.Body.Close()

	var result OKResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&result); decErr != nil {
		return fmt.Errorf("decode shutdown response: %w", decErr)
	}

	if !result.OK {
		return fmt.Errorf("shutdown: %s", result.Error)
	}

	return nil
}

// Restart requests a daemon restart via POST /restart.
func (c *Client) Restart(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/restart", nil)
	if err != nil {
		return fmt.Errorf("build restart request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("restart request: %w", err)
	}
	defer resp.Body.Close()

	var result OKResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&result); decErr != nil {
		return fmt.Errorf("decode restart response: %w", decErr)
	}

	if !result.OK {
		return fmt.Errorf("restart: %s", result.Error)
	}

	return nil
}
