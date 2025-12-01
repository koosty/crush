package copilot

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/charmbracelet/crush/internal/oauth"
)

// CopilotAPIBaseURL is the base URL for the GitHub Copilot API.
const CopilotAPIBaseURL = "https://api.githubcopilot.com"

// TokenProvider is a function that returns the GitHub OAuth token.
type TokenProvider func() (*oauth.Token, error)

// TokenSaver is a function that saves the updated OAuth token after a Copilot
// token exchange. This allows persisting the short-lived Copilot token.
type TokenSaver func(token *oauth.Token) error

// Transport implements http.RoundTripper and handles automatic Copilot token
// management. It exchanges the long-lived GitHub OAuth token for short-lived
// Copilot API tokens and refreshes them as needed.
type Transport struct {
	tokenProvider TokenProvider
	tokenSaver    TokenSaver
	base          http.RoundTripper

	mu           sync.RWMutex
	copilotToken *CopilotToken
}

// NewTransport creates a new Transport with the given token provider and saver.
// The tokenSaver is optional and can be nil if persistence is not needed.
func NewTransport(tokenProvider TokenProvider, tokenSaver TokenSaver) *Transport {
	return &Transport{
		tokenProvider: tokenProvider,
		tokenSaver:    tokenSaver,
		base:          http.DefaultTransport,
	}
}

// RoundTrip implements http.RoundTripper. It automatically handles Copilot
// token acquisition and refresh.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Get a valid Copilot token.
	token, err := t.getValidToken(req.Context())
	if err != nil {
		return nil, err
	}

	// Clone the request to avoid modifying the original.
	reqCopy := req.Clone(req.Context())

	// Set Authorization header with Copilot token.
	reqCopy.Header.Set("Authorization", "Bearer "+token)

	// Set required Copilot headers.
	for key, value := range CopilotHeaders {
		reqCopy.Header.Set(key, value)
	}

	// Set additional headers for chat requests.
	reqCopy.Header.Set("Openai-Intent", "conversation-edits")
	reqCopy.Header.Set("X-Initiator", "user")

	return t.base.RoundTrip(reqCopy)
}

// getValidToken returns a valid Copilot API token, refreshing if necessary.
func (t *Transport) getValidToken(ctx context.Context) (string, error) {
	// Check if we have a valid cached token in memory.
	t.mu.RLock()
	if t.copilotToken != nil && !t.copilotToken.IsExpired() {
		token := t.copilotToken.Token
		t.mu.RUnlock()
		return token, nil
	}
	t.mu.RUnlock()

	// Need to refresh the token.
	t.mu.Lock()
	defer t.mu.Unlock()

	// Double-check after acquiring write lock.
	if t.copilotToken != nil && !t.copilotToken.IsExpired() {
		return t.copilotToken.Token, nil
	}

	// Get the GitHub OAuth token.
	oauthToken, err := t.tokenProvider()
	if err != nil {
		return "", err
	}

	if oauthToken == nil || oauthToken.RefreshToken == "" {
		return "", &OAuthError{Code: "no_token", Description: "no GitHub OAuth token available"}
	}

	// Check if the persisted Copilot token is still valid.
	if !oauthToken.IsCopilotTokenExpired() {
		t.copilotToken = &CopilotToken{
			Token:     oauthToken.CopilotToken,
			ExpiresAt: oauthToken.CopilotExpiresAt,
		}
		return oauthToken.CopilotToken, nil
	}

	// Exchange for Copilot token.
	// Note: For Copilot, we store the GitHub OAuth token in RefreshToken field
	// since it acts as the long-lived token used to obtain short-lived Copilot tokens.
	copilotToken, err := ExchangeForCopilotToken(ctx, oauthToken.RefreshToken)
	if err != nil {
		return "", err
	}

	t.copilotToken = copilotToken

	// Persist the new Copilot token if a saver is configured.
	if t.tokenSaver != nil {
		oauthToken.CopilotToken = copilotToken.Token
		oauthToken.CopilotExpiresAt = copilotToken.ExpiresAt
		if err := t.tokenSaver(oauthToken); err != nil {
			slog.Warn("Failed to persist Copilot token", "error", err)
			// Don't fail - token is still usable in memory.
		}
	}

	return copilotToken.Token, nil
}

// ClearCache clears the cached Copilot token, forcing a refresh on next request.
func (t *Transport) ClearCache() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.copilotToken = nil
}

// SetBaseTransport sets the underlying transport. Useful for testing or debugging.
func (t *Transport) SetBaseTransport(base http.RoundTripper) {
	t.base = base
}
