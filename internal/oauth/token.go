package oauth

import (
	"time"
)

// Token represents an OAuth2 token from Claude Code Max.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`

	// CopilotToken stores the short-lived Copilot API token (tid=xxx).
	// This is used by GitHub Copilot provider to cache the API token.
	CopilotToken string `json:"copilot_token,omitempty"`
	// CopilotExpiresAt is the Unix timestamp when CopilotToken expires.
	CopilotExpiresAt int64 `json:"copilot_expires_at,omitempty"`
}

// SetExpiresAt calculates and sets the ExpiresAt field based on the current time and ExpiresIn.
func (t *Token) SetExpiresAt() {
	t.ExpiresAt = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second).Unix()
}

// IsExpired checks if the token is expired or about to expire (within 10% of its lifetime).
func (t *Token) IsExpired() bool {
	return time.Now().Unix() >= (t.ExpiresAt - int64(t.ExpiresIn)/10)
}

// IsCopilotTokenExpired checks if the Copilot token is expired or about to expire.
// Returns true if the token is missing, empty, or will expire within 60 seconds.
func (t *Token) IsCopilotTokenExpired() bool {
	if t == nil || t.CopilotToken == "" {
		return true
	}
	// Add 60 second buffer to avoid edge cases.
	return time.Now().Unix() >= (t.CopilotExpiresAt - 60)
}
