package copilot

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCopilotTokenIsExpired(t *testing.T) {
	t.Parallel()

	t.Run("nil token is expired", func(t *testing.T) {
		t.Parallel()
		var token *CopilotToken
		require.True(t, token.IsExpired())
	})

	t.Run("empty token is expired", func(t *testing.T) {
		t.Parallel()
		token := &CopilotToken{Token: "", ExpiresAt: time.Now().Add(time.Hour).Unix()}
		require.True(t, token.IsExpired())
	})

	t.Run("expired token", func(t *testing.T) {
		t.Parallel()
		token := &CopilotToken{
			Token:     "test",
			ExpiresAt: time.Now().Add(-time.Hour).Unix(),
		}
		require.True(t, token.IsExpired())
	})

	t.Run("token expiring within buffer", func(t *testing.T) {
		t.Parallel()
		// Token expires in 30 seconds, but buffer is 60 seconds.
		token := &CopilotToken{
			Token:     "test",
			ExpiresAt: time.Now().Add(30 * time.Second).Unix(),
		}
		require.True(t, token.IsExpired())
	})

	t.Run("valid token", func(t *testing.T) {
		t.Parallel()
		token := &CopilotToken{
			Token:     "test",
			ExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
		}
		require.False(t, token.IsExpired())
	})
}

func TestOAuthError(t *testing.T) {
	t.Parallel()

	t.Run("error with description", func(t *testing.T) {
		t.Parallel()
		err := &OAuthError{Code: "test_error", Description: "test description"}
		require.Equal(t, "test_error: test description", err.Error())
	})

	t.Run("error without description", func(t *testing.T) {
		t.Parallel()
		err := &OAuthError{Code: "test_error"}
		require.Equal(t, "test_error", err.Error())
	})
}

func TestCopilotHeaders(t *testing.T) {
	t.Parallel()

	t.Run("required headers are set", func(t *testing.T) {
		t.Parallel()
		require.NotEmpty(t, CopilotHeaders["User-Agent"])
		require.NotEmpty(t, CopilotHeaders["Editor-Version"])
		require.NotEmpty(t, CopilotHeaders["Editor-Plugin-Version"])
		require.NotEmpty(t, CopilotHeaders["Copilot-Integration-Id"])
	})

	t.Run("user agent matches vscode pattern", func(t *testing.T) {
		t.Parallel()
		require.Contains(t, CopilotHeaders["User-Agent"], "GitHubCopilotChat")
	})
}

func TestProviderID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "github-copilot", ProviderID)
}

// TestDeviceFlowResponseParsing tests the device flow response JSON parsing.
func TestDeviceFlowResponseParsing(t *testing.T) {
	t.Parallel()

	t.Run("parses valid response", func(t *testing.T) {
		t.Parallel()

		jsonData := `{
			"device_code": "abc123",
			"user_code": "TEST-1234",
			"verification_uri": "https://github.com/login/device",
			"expires_in": 900,
			"interval": 5
		}`

		var resp DeviceFlowResponse
		err := json.Unmarshal([]byte(jsonData), &resp)
		require.NoError(t, err)
		require.Equal(t, "abc123", resp.DeviceCode)
		require.Equal(t, "TEST-1234", resp.UserCode)
		require.Equal(t, "https://github.com/login/device", resp.VerificationURI)
		require.Equal(t, 900, resp.ExpiresIn)
		require.Equal(t, 5, resp.Interval)
	})

	t.Run("handles missing optional fields", func(t *testing.T) {
		t.Parallel()

		jsonData := `{
			"device_code": "abc123",
			"user_code": "TEST-1234"
		}`

		var resp DeviceFlowResponse
		err := json.Unmarshal([]byte(jsonData), &resp)
		require.NoError(t, err)
		require.Equal(t, "abc123", resp.DeviceCode)
		require.Equal(t, "TEST-1234", resp.UserCode)
		require.Empty(t, resp.VerificationURI)
		require.Equal(t, 0, resp.ExpiresIn)
		require.Equal(t, 0, resp.Interval)
	})
}

// TestTokenResponseParsing tests the token response JSON parsing.
func TestTokenResponseParsing(t *testing.T) {
	t.Parallel()

	t.Run("parses access token response", func(t *testing.T) {
		t.Parallel()

		jsonData := `{
			"access_token": "ghu_xxxxxxxxxxxx",
			"token_type": "bearer",
			"scope": ""
		}`

		var resp struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
			Scope       string `json:"scope"`
			Error       string `json:"error"`
			ErrorDesc   string `json:"error_description"`
		}
		err := json.Unmarshal([]byte(jsonData), &resp)
		require.NoError(t, err)
		require.Equal(t, "ghu_xxxxxxxxxxxx", resp.AccessToken)
		require.Equal(t, "bearer", resp.TokenType)
		require.Empty(t, resp.Error)
	})

	t.Run("parses authorization_pending error", func(t *testing.T) {
		t.Parallel()

		jsonData := `{
			"error": "authorization_pending",
			"error_description": "The authorization request is still pending."
		}`

		var resp struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
			ErrorDesc   string `json:"error_description"`
			Interval    int    `json:"interval"`
		}
		err := json.Unmarshal([]byte(jsonData), &resp)
		require.NoError(t, err)
		require.Empty(t, resp.AccessToken)
		require.Equal(t, "authorization_pending", resp.Error)
		require.Equal(t, "The authorization request is still pending.", resp.ErrorDesc)
	})

	t.Run("parses slow_down response with interval", func(t *testing.T) {
		t.Parallel()

		jsonData := `{
			"error": "slow_down",
			"error_description": "Too many requests.",
			"interval": 10
		}`

		var resp struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
			ErrorDesc   string `json:"error_description"`
			Interval    int    `json:"interval"`
		}
		err := json.Unmarshal([]byte(jsonData), &resp)
		require.NoError(t, err)
		require.Equal(t, "slow_down", resp.Error)
		require.Equal(t, 10, resp.Interval)
	})

	t.Run("parses access_denied error", func(t *testing.T) {
		t.Parallel()

		jsonData := `{
			"error": "access_denied",
			"error_description": "The user has denied your application access."
		}`

		var resp struct {
			Error     string `json:"error"`
			ErrorDesc string `json:"error_description"`
		}
		err := json.Unmarshal([]byte(jsonData), &resp)
		require.NoError(t, err)
		require.Equal(t, "access_denied", resp.Error)
	})
}

// TestCopilotTokenResponseParsing tests the Copilot token response parsing.
func TestCopilotTokenResponseParsing(t *testing.T) {
	t.Parallel()

	t.Run("parses valid copilot token", func(t *testing.T) {
		t.Parallel()

		jsonData := `{"token": "tid=xxxxxxxxxxxx", "expires_at": 1735689600}`

		var token CopilotToken
		err := json.Unmarshal([]byte(jsonData), &token)
		require.NoError(t, err)
		require.Equal(t, "tid=xxxxxxxxxxxx", token.Token)
		require.Equal(t, int64(1735689600), token.ExpiresAt)
	})

	t.Run("token marshals correctly", func(t *testing.T) {
		t.Parallel()

		token := CopilotToken{
			Token:     "test-token",
			ExpiresAt: 1735689600,
		}

		data, err := json.Marshal(token)
		require.NoError(t, err)
		require.Contains(t, string(data), `"token":"test-token"`)
		require.Contains(t, string(data), `"expires_at":1735689600`)
	})
}

func TestClientIDConstant(t *testing.T) {
	t.Parallel()

	// Verify the client ID matches VS Code's Copilot extension.
	require.Equal(t, "Iv1.b507a08c87ecfe98", clientID)
}

func TestCopilotAPIBaseURL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://api.githubcopilot.com", CopilotAPIBaseURL)
}
