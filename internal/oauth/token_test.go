package oauth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestToken_SetExpiresAt(t *testing.T) {
	t.Parallel()

	token := &Token{
		ExpiresIn: 3600,
	}

	before := time.Now().Unix()
	token.SetExpiresAt()
	after := time.Now().Unix()

	require.GreaterOrEqual(t, token.ExpiresAt, before+3600)
	require.LessOrEqual(t, token.ExpiresAt, after+3600)
}

func TestToken_IsExpired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		expiresAt int64
		expiresIn int
		want      bool
	}{
		{
			name:      "not expired",
			expiresAt: time.Now().Add(time.Hour).Unix(),
			expiresIn: 3600,
			want:      false,
		},
		{
			name:      "expired",
			expiresAt: time.Now().Add(-time.Hour).Unix(),
			expiresIn: 3600,
			want:      true,
		},
		{
			name:      "within 10% buffer",
			expiresAt: time.Now().Add(5 * time.Minute).Unix(), // 5 min left of 1 hour = within 10%
			expiresIn: 3600,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			token := &Token{
				ExpiresAt: tt.expiresAt,
				ExpiresIn: tt.expiresIn,
			}
			require.Equal(t, tt.want, token.IsExpired())
		})
	}
}

func TestToken_IsCopilotTokenExpired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token *Token
		want  bool
	}{
		{
			name:  "nil token",
			token: nil,
			want:  true,
		},
		{
			name:  "empty copilot token",
			token: &Token{CopilotToken: ""},
			want:  true,
		},
		{
			name: "valid token not expired",
			token: &Token{
				CopilotToken:     "tid=abc123",
				CopilotExpiresAt: time.Now().Add(time.Hour).Unix(),
			},
			want: false,
		},
		{
			name: "expired token",
			token: &Token{
				CopilotToken:     "tid=abc123",
				CopilotExpiresAt: time.Now().Add(-time.Hour).Unix(),
			},
			want: true,
		},
		{
			name: "within 60 second buffer",
			token: &Token{
				CopilotToken:     "tid=abc123",
				CopilotExpiresAt: time.Now().Add(30 * time.Second).Unix(), // 30 sec left, within 60 sec buffer
			},
			want: true,
		},
		{
			name: "just outside 60 second buffer",
			token: &Token{
				CopilotToken:     "tid=abc123",
				CopilotExpiresAt: time.Now().Add(90 * time.Second).Unix(), // 90 sec left, outside buffer
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.token.IsCopilotTokenExpired())
		})
	}
}
