package copilot

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/stretchr/testify/require"
)

func TestNewTransport(t *testing.T) {
	t.Parallel()

	t.Run("creates transport with provider and saver", func(t *testing.T) {
		t.Parallel()

		tokenProvider := func() (*oauth.Token, error) {
			return &oauth.Token{RefreshToken: "test"}, nil
		}
		tokenSaver := func(_ *oauth.Token) error {
			return nil
		}

		transport := NewTransport(tokenProvider, tokenSaver)
		require.NotNil(t, transport)
	})

	t.Run("creates transport without saver", func(t *testing.T) {
		t.Parallel()

		tokenProvider := func() (*oauth.Token, error) {
			return &oauth.Token{RefreshToken: "test"}, nil
		}

		transport := NewTransport(tokenProvider, nil)
		require.NotNil(t, transport)
	})
}

func TestTransport_RoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("adds authorization header", func(t *testing.T) {
		t.Parallel()

		var capturedAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create a transport with a pre-cached valid token.
		transport := &Transport{
			tokenProvider: func() (*oauth.Token, error) {
				return &oauth.Token{RefreshToken: "ghu_test"}, nil
			},
			base: http.DefaultTransport,
			copilotToken: &CopilotToken{
				Token:     "cached-copilot-token",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
			},
		}

		req, err := http.NewRequest("GET", server.URL, nil)
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		defer resp.Body.Close()

		require.Equal(t, "Bearer cached-copilot-token", capturedAuth)
	})

	t.Run("adds copilot headers", func(t *testing.T) {
		t.Parallel()

		var capturedHeaders http.Header
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		transport := &Transport{
			tokenProvider: func() (*oauth.Token, error) {
				return &oauth.Token{RefreshToken: "ghu_test"}, nil
			},
			base: http.DefaultTransport,
			copilotToken: &CopilotToken{
				Token:     "cached-token",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
			},
		}

		req, err := http.NewRequest("GET", server.URL, nil)
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		defer resp.Body.Close()

		require.Equal(t, CopilotHeaders["User-Agent"], capturedHeaders.Get("User-Agent"))
		require.Equal(t, CopilotHeaders["Editor-Version"], capturedHeaders.Get("Editor-Version"))
		require.Equal(t, "conversation-edits", capturedHeaders.Get("Openai-Intent"))
		require.Equal(t, "user", capturedHeaders.Get("X-Initiator"))
	})

	t.Run("does not modify original request", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		transport := &Transport{
			tokenProvider: func() (*oauth.Token, error) {
				return &oauth.Token{RefreshToken: "ghu_test"}, nil
			},
			base: http.DefaultTransport,
			copilotToken: &CopilotToken{
				Token:     "cached-token",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
			},
		}

		req, err := http.NewRequest("GET", server.URL, nil)
		require.NoError(t, err)

		originalAuthHeader := req.Header.Get("Authorization")

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		defer resp.Body.Close()

		// Original request should not be modified.
		require.Equal(t, originalAuthHeader, req.Header.Get("Authorization"))
	})
}

func TestTransport_ClearCache(t *testing.T) {
	t.Parallel()

	t.Run("clears cached token", func(t *testing.T) {
		t.Parallel()

		transport := &Transport{
			copilotToken: &CopilotToken{
				Token:     "cached-token",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
			},
		}

		require.NotNil(t, transport.copilotToken)

		transport.ClearCache()

		require.Nil(t, transport.copilotToken)
	})
}

func TestTransport_SetBaseTransport(t *testing.T) {
	t.Parallel()

	t.Run("sets base transport", func(t *testing.T) {
		t.Parallel()

		transport := NewTransport(nil, nil)

		customTransport := &http.Transport{
			MaxIdleConns: 100,
		}

		transport.SetBaseTransport(customTransport)

		require.Equal(t, customTransport, transport.base)
	})
}

func TestTransport_TokenRefresh(t *testing.T) {
	t.Parallel()

	t.Run("uses cached token when valid", func(t *testing.T) {
		t.Parallel()

		providerCallCount := 0
		transport := &Transport{
			tokenProvider: func() (*oauth.Token, error) {
				providerCallCount++
				return &oauth.Token{RefreshToken: "ghu_test"}, nil
			},
			base: http.DefaultTransport,
			copilotToken: &CopilotToken{
				Token:     "cached-token",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Make multiple requests.
		for range 3 {
			req, err := http.NewRequest("GET", server.URL, nil)
			require.NoError(t, err)

			resp, err := transport.RoundTrip(req)
			require.NoError(t, err)
			resp.Body.Close()
		}

		// Token provider should not be called when cache is valid.
		require.Equal(t, 0, providerCallCount)
	})

	t.Run("token provider error propagates", func(t *testing.T) {
		t.Parallel()

		transport := &Transport{
			tokenProvider: func() (*oauth.Token, error) {
				return nil, errors.New("token provider error")
			},
			base:         http.DefaultTransport,
			copilotToken: nil, // No cached token.
		}

		req, err := http.NewRequest("GET", "http://example.com", nil)
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.Error(t, err)
		require.Nil(t, resp)
		require.Contains(t, err.Error(), "token provider error")
	})
}

func TestTransport_Concurrency(t *testing.T) {
	t.Parallel()

	t.Run("handles concurrent requests safely", func(t *testing.T) {
		t.Parallel()

		var requestCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requestCount.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		transport := &Transport{
			tokenProvider: func() (*oauth.Token, error) {
				return &oauth.Token{RefreshToken: "ghu_test"}, nil
			},
			base: http.DefaultTransport,
			copilotToken: &CopilotToken{
				Token:     "cached-token",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
			},
		}

		var wg sync.WaitGroup
		numRequests := 10

		for range numRequests {
			wg.Add(1)
			go func() {
				defer wg.Done()

				req, err := http.NewRequest("GET", server.URL, nil)
				require.NoError(t, err)

				resp, err := transport.RoundTrip(req)
				require.NoError(t, err)
				if resp != nil {
					resp.Body.Close()
				}
			}()
		}

		wg.Wait()

		require.Equal(t, int32(numRequests), requestCount.Load())
	})
}

func TestTransport_UsesPersistedCopilotToken(t *testing.T) {
	t.Parallel()

	t.Run("uses persisted copilot token from oauth.Token", func(t *testing.T) {
		t.Parallel()

		var capturedAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// OAuth token with persisted Copilot token.
		oauthToken := &oauth.Token{
			RefreshToken:     "ghu_github_token",
			CopilotToken:     "persisted-copilot-token",
			CopilotExpiresAt: time.Now().Add(time.Hour).Unix(),
		}

		transport := &Transport{
			tokenProvider: func() (*oauth.Token, error) {
				return oauthToken, nil
			},
			base:         http.DefaultTransport,
			copilotToken: nil, // No in-memory cache.
		}

		req, err := http.NewRequest("GET", server.URL, nil)
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		defer resp.Body.Close()

		// Should use the persisted Copilot token.
		require.Equal(t, "Bearer persisted-copilot-token", capturedAuth)
	})
}
