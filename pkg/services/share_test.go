package services

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateShortCode(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{name: "plain alphanumeric", code: "a1b2c3d", wantErr: false},
		{name: "dot in the middle", code: "my.file", wantErr: false},
		{name: "dash in the middle", code: "my-file", wantErr: false},
		{name: "underscore in the middle", code: "my_file", wantErr: false},
		{name: "mixed separators", code: "v2.1-beta_final", wantErr: false},
		{name: "minimum length", code: "abcd", wantErr: false},
		{name: "maximum length", code: strings.Repeat("a", shortCodeMaxLength), wantErr: false},
		{
			name:    "a realistic long filename, well past the old 32-char cap",
			code:    "IMG_20260802_Family-Vacation_Beach-Sunset-Photo.jpg",
			wantErr: false,
		},
		{
			name:    "36 characters but not uuid-shaped is still fine",
			code:    strings.Repeat("a", 36),
			wantErr: false,
		},

		{name: "too short", code: "abc", wantErr: true},
		{name: "too long", code: strings.Repeat("a", shortCodeMaxLength+1), wantErr: true},
		{name: "a real uuid is rejected regardless of length", code: "123e4567-e89b-12d3-a456-426614174000", wantErr: true},
		{name: "uppercase uuid is rejected too", code: "123E4567-E89B-12D3-A456-426614174000", wantErr: true},
		{name: "leading dot", code: ".myfile", wantErr: true},
		{name: "trailing dot", code: "myfile.", wantErr: true},
		{name: "leading dash", code: "-myfile", wantErr: true},
		{name: "trailing dash", code: "myfile-", wantErr: true},
		{name: "leading underscore", code: "_myfile", wantErr: true},
		{name: "trailing underscore", code: "myfile_", wantErr: true},
		{name: "space", code: "my file", wantErr: true},
		{name: "slash", code: "my/file", wantErr: true},
		{name: "unicode", code: "café1234", wantErr: true},

		{name: "reserved word", code: "share", wantErr: true},
		{name: "reserved word is case-insensitive", code: "SHARE", wantErr: true},
		{name: "reserved word with a dot, now reachable with dots allowed", code: "favicon.ico", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShortCode(tt.code)
			if tt.wantErr && err == nil {
				t.Errorf("validateShortCode(%q) = nil, want an error", tt.code)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateShortCode(%q) = %v, want nil", tt.code, err)
			}
			if err != nil && !errors.Is(err, ErrShortCodeInvalid) {
				t.Errorf("validateShortCode(%q) error = %v, want it to wrap ErrShortCodeInvalid", tt.code, err)
			}
		})
	}
}
