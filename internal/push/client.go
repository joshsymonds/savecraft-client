// Package push provides the HTTP client for pushing parsed game state to the server.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
)

const (
	maxErrorBody      = 512
	pushClientTimeout = 30 * time.Second
)

// Client pushes parsed game state to the Savecraft API.
type Client struct {
	httpClient *http.Client
	baseURL    *url.URL
	authToken  string
}

// New creates a push Client targeting the given server URL with the given auth token.
func New(serverURL, authToken string) (*Client, error) {
	baseURL, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}
	return &Client{
		httpClient: &http.Client{Timeout: pushClientTimeout},
		baseURL:    baseURL,
		authToken:  authToken,
	}, nil
}

// Push sends the parsed GameState to the server via POST /api/v1/push.
func (c *Client) Push(
	ctx context.Context,
	gameID string,
	state *daemon.GameState,
	parsedAt time.Time,
) (*daemon.PushResult, error) {
	body, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal state: %w", err)
	}

	pushURL := c.baseURL.JoinPath("/api/v1/push")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pushURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.authToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Game", gameID)
	req.Header.Set("X-Parsed-At", parsedAt.UTC().Format(time.RFC3339))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send push: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, readErrorResponse(resp)
	}

	var result daemon.PushResult
	if decodeErr := json.NewDecoder(resp.Body).Decode(&result); decodeErr != nil {
		return nil, fmt.Errorf("decode response: %w", decodeErr)
	}

	return &result, nil
}

func readErrorResponse(resp *http.Response) error {
	errBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if readErr != nil {
		return &daemon.PushStatusError{
			StatusCode: resp.StatusCode,
			Body:       "(body unreadable)",
		}
	}
	return &daemon.PushStatusError{
		StatusCode: resp.StatusCode,
		Body:       string(bytes.TrimSpace(errBody)),
	}
}
