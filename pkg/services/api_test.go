package services

import (
	"context"
	"testing"

	"github.com/tgdrive/teldrive/internal/config"
)

// ConfigConfig is what /config reports to clients, and it's a plain field
// copy with no other signal that a field was forgotten - the zero value
// (false, or an unset OptString) is indistinguishable from a deliberately-off
// feature. This pins every field so an addition without a matching copy
// fails loudly instead of shipping a flag that's permanently stuck off
// regardless of server config.
func TestConfigConfig(t *testing.T) {
	tests := []struct {
		name              string
		zipEnabled        bool
		shortlinksEnabled bool
		shortlinkDomain   string
	}{
		{name: "all unset", zipEnabled: false, shortlinksEnabled: false, shortlinkDomain: ""},
		{name: "all set", zipEnabled: true, shortlinksEnabled: true, shortlinkDomain: "https://go.example.com"},
		{name: "zip only", zipEnabled: true, shortlinksEnabled: false, shortlinkDomain: ""},
		{name: "shortlinks enabled, no domain configured", zipEnabled: false, shortlinksEnabled: true, shortlinkDomain: ""},
		{name: "domain configured, shortlinks not enabled", zipEnabled: false, shortlinksEnabled: false, shortlinkDomain: "https://go.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &apiService{
				cnf: &config.ServerCmdConfig{
					Files: config.FilesConfig{EnableZipDownload: tt.zipEnabled},
					Shortlinks: config.ShortlinkConfig{
						Enabled: tt.shortlinksEnabled,
						Domain:  tt.shortlinkDomain,
					},
				},
			}

			got, err := a.ConfigConfig(context.Background())
			if err != nil {
				t.Fatalf("ConfigConfig() error = %v", err)
			}
			if got.ZipDownloadEnabled != tt.zipEnabled {
				t.Errorf("ZipDownloadEnabled = %v, want %v", got.ZipDownloadEnabled, tt.zipEnabled)
			}
			if got.ShortlinksEnabled != tt.shortlinksEnabled {
				t.Errorf("ShortlinksEnabled = %v, want %v", got.ShortlinksEnabled, tt.shortlinksEnabled)
			}
			wantSet := tt.shortlinkDomain != ""
			if got.ShortlinkDomain.Set != wantSet {
				t.Errorf("ShortlinkDomain.Set = %v, want %v", got.ShortlinkDomain.Set, wantSet)
			}
			if got.ShortlinkDomain.Value != tt.shortlinkDomain {
				t.Errorf("ShortlinkDomain.Value = %q, want %q", got.ShortlinkDomain.Value, tt.shortlinkDomain)
			}
		})
	}
}
