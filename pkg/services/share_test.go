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

		{name: "too short", code: "abc", wantErr: true},
		{name: "too long", code: strings.Repeat("a", shortCodeMaxLength+1), wantErr: true},
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
