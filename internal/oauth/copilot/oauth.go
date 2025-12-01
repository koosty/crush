package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"time"
)

// OAuth Client ID for GitHub Copilot Chat (same as VS Code extension).
// This is a public client ID and safe to include in source code.
const clientID = "Iv1.b507a08c87ecfe98"

// API endpoints.
const (
	deviceCodeURL   = "https://github.com/login/device/code"
	tokenURL        = "https://github.com/login/oauth/access_token"
	copilotTokenURL = "https://api.github.com/copilot_internal/v2/token"
)

// CopilotHeaders are required headers to mimic VS Code's Copilot extension.
var CopilotHeaders = map[string]string{
	"User-Agent":             "GitHubCopilotChat/0.32.4",
	"Editor-Version":         "vscode/1.105.1",
	"Editor-Plugin-Version":  "copilot-chat/0.32.4",
	"Copilot-Integration-Id": "vscode-chat",
}

// DeviceFlowResponse represents the response from the device code endpoint.
type DeviceFlowResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// CopilotToken represents the short-lived Copilot API token.
type CopilotToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"` // Unix timestamp in seconds
}

// IsExpired checks if the token is expired or about to expire (within 60 seconds).
func (t *CopilotToken) IsExpired() bool {
	if t == nil || t.Token == "" {
		return true
	}
	// Add 60 second buffer to avoid edge cases.
	return time.Now().Unix() >= (t.ExpiresAt - 60)
}

// StartDeviceFlow initiates the GitHub OAuth device flow.
func StartDeviceFlow(ctx context.Context) (*DeviceFlowResponse, error) {
	// GitHub's device code endpoint requires application/x-www-form-urlencoded.
	formData := url.Values{}
	formData.Set("client_id", clientID)
	formData.Set("scope", "read:user")

	req, err := http.NewRequestWithContext(ctx, "POST", deviceCodeURL, bytes.NewBufferString(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create device flow request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to start device flow: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read device flow response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device flow failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result DeviceFlowResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse device flow response: %w", err)
	}

	return &result, nil
}

// PollForToken polls the GitHub token endpoint until the user authorizes or times out.
// Returns the GitHub OAuth token (gho_xxx) on success.
func PollForToken(ctx context.Context, deviceCode string, interval int) (string, error) {
	if interval < 5 {
		interval = 5 // Minimum 5 seconds as per GitHub docs.
	}

	// Poll immediately on first call, then wait for interval.
	for i := 0; ; i++ {
		if i > 0 {
			// Wait for the current interval before polling again.
			slog.Info("Copilot polling: waiting before retry", "interval", interval)
			select {
			case <-ctx.Done():
				slog.Info("Copilot polling: context cancelled")
				return "", ctx.Err()
			case <-time.After(time.Duration(interval) * time.Second):
			}
		}

		slog.Info("Copilot polling: checking authorization", "attempt", i+1)
		token, newInterval, err := pollOnce(ctx, deviceCode)
		if err != nil {
			// Check for expected polling errors.
			if oauthErr, ok := err.(*OAuthError); ok {
				if oauthErr.Code == "authorization_pending" {
					slog.Info("Copilot polling: authorization pending, will retry")
					continue
				}
				if oauthErr.Code == "slow_down" {
					// GitHub is asking us to slow down - use the new interval.
					if newInterval > interval {
						interval = newInterval
					} else {
						interval += 5 // Add 5 seconds as fallback.
					}
					slog.Info("Copilot polling: slow_down received, increasing interval", "new_interval", interval)
					continue
				}
			}
			slog.Error("Copilot polling: error", "error", err)
			return "", err
		}
		if token != "" {
			slog.Info("Copilot polling: got token!")
			return token, nil
		}
	}
}

func pollOnce(ctx context.Context, deviceCode string) (string, int, error) {
	// GitHub's token endpoint requires application/x-www-form-urlencoded, not JSON.
	formData := url.Values{}
	formData.Set("client_id", clientID)
	formData.Set("device_code", deviceCode)
	formData.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, bytes.NewBufferString(formData.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("failed to poll for token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read token response: %w", err)
	}

	slog.Debug("Copilot token response", "status", resp.StatusCode, "body", string(body))

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
		Interval    int    `json:"interval"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("failed to parse token response: %w", err)
	}

	if result.Error != "" {
		return "", result.Interval, &OAuthError{Code: result.Error, Description: result.ErrorDesc}
	}

	return result.AccessToken, 0, nil
}

// ExchangeForCopilotToken exchanges a GitHub OAuth token for a short-lived Copilot API token.
func ExchangeForCopilotToken(ctx context.Context, githubToken string) (*CopilotToken, error) {
	headers := maps.Clone(CopilotHeaders)
	headers["Authorization"] = "Bearer " + githubToken

	resp, err := doRequest(ctx, "GET", copilotTokenURL, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange for copilot token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read copilot token response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// Success.
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("github authentication failed: invalid or expired token")
	case http.StatusForbidden:
		return nil, fmt.Errorf("no copilot access: your GitHub account doesn't have an active Copilot subscription")
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("rate limited: please wait and try again")
	default:
		return nil, fmt.Errorf("copilot token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result CopilotToken
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse copilot token response: %w", err)
	}

	return &result, nil
}

// ValidateToken checks if a GitHub OAuth token has Copilot access.
func ValidateToken(ctx context.Context, githubToken string) error {
	_, err := ExchangeForCopilotToken(ctx, githubToken)
	return err
}

// OAuthError represents an OAuth error response.
type OAuthError struct {
	Code        string
	Description string
}

func (e *OAuthError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Description)
	}
	return e.Code
}

func doRequest(ctx context.Context, method, url string, body any, headers map[string]string) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}
