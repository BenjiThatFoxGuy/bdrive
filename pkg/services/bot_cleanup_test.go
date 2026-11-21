package services

import (
	"errors"
	"testing"

	"github.com/gotd/td/tgerr"
)

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
