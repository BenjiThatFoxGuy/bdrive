package services

import (
	"errors"
	"testing"

	"github.com/gotd/td/tgerr"
)

func TestBotIDFromToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stored int64
		token  string
		want   int64
	}{
		{"stored id wins", 42, "999:secret", 42},
		{"derived from the token prefix", 0, "999:secret", 999},
		{"no colon keeps the stored value", 0, "not-a-token", 0},
		{"non numeric prefix keeps the stored value", 0, "abc:secret", 0},
		{"empty token keeps the stored value", 0, "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := botIDFromToken(tt.stored, tt.token); got != tt.want {
				t.Fatalf("botIDFromToken(%d, %q) = %d, want %d", tt.stored, tt.token, got, tt.want)
			}
		})
	}
}

func TestIsDefiniteBotAuthFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "auth key invalid",
			err:  tgerr.New(401, "AUTH_KEY_INVALID"),
			want: true,
		},
		{
			name: "auth token invalid",
			err:  tgerr.New(401, "AUTH_TOKEN_INVALID"),
			want: true,
		},
		{
			name: "auth key unregistered",
			err:  tgerr.New(401, "AUTH_KEY_UNREGISTERED"),
			want: true,
		},
		{
			name: "bot deactivated message",
			err:  errors.New("BOT_INVALID: bot was deactivated by the user"),
			want: true,
		},
		{
			name: "access token expired is a removal reason",
			err:  tgerr.New(400, "ACCESS_TOKEN_EXPIRED"),
			want: true,
		},
		{
			name: "access token invalid is a removal reason",
			err:  tgerr.New(400, "ACCESS_TOKEN_INVALID"),
			want: true,
		},
		{
			name: "flood wait is not a removal reason",
			err:  tgerr.New(420, "FLOOD_WAIT_30"),
			want: false,
		},
		{
			name: "network timeout is not a removal reason",
			err:  errors.New("dial tcp 145.102.0.154:123: i/o timeout"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isDefiniteBotAuthFailure(tt.err); got != tt.want {
				t.Fatalf("isDefiniteBotAuthFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
