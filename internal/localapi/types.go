package localapi

import "strings"

// State represents the daemon's lifecycle phase.
type State string

// Daemon lifecycle states.
const (
	StateStarting    State = "starting"
	StateRegistering State = "registering"
	StateRegistered  State = "registered"
	StateRunning     State = "running"
	StateError       State = "error"
)

// BootResponse is the JSON body returned by GET /boot.
type BootResponse struct {
	State State  `json:"state"`
	Error string `json:"error,omitempty"`
}

// LinkResponse is the JSON body returned by GET /link.
type LinkResponse struct {
	LinkCode  string `json:"linkCode,omitempty"`
	LinkURL   string `json:"linkUrl,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	Error     string `json:"error,omitempty"`
	State     State  `json:"state,omitempty"`
}

// BuildLinkURL constructs the frontend link URL from the base URL and code.
func BuildLinkURL(frontendURL, linkCode string) string {
	return strings.TrimRight(frontendURL, "/") + "/link/" + linkCode
}
