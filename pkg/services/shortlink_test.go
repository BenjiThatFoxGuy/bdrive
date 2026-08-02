package services

import (
	"testing"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/pkg/models"
)

func TestDecide(t *testing.T) {
	fileShareOf := func(block, always bool) *fileShare {
		return &fileShare{
			FileShare: models.FileShare{BlockDirectLink: block, AlwaysDirectLink: always},
			Type:      api.FileShareInfoTypeFile,
		}
	}
	folderShareOf := func(allowZip, block, always bool) *fileShare {
		return &fileShare{
			FileShare: models.FileShare{
				BlockDirectLink:  block,
				AlwaysDirectLink: always,
				AllowZipDownload: allowZip,
			},
			Type: api.FileShareInfoTypeFolder,
		}
	}

	cases := []struct {
		name    string
		share   *fileShare
		browser bool
		want    ShortlinkAction
	}{
		// file shares: default (neither toggle) follows UA
		{"file/default/browser", fileShareOf(false, false), true, ShortlinkViewer},
		{"file/default/non-browser", fileShareOf(false, false), false, ShortlinkDirect},
		// file shares: block forces viewer regardless of UA
		{"file/block/browser", fileShareOf(true, false), true, ShortlinkViewer},
		{"file/block/non-browser", fileShareOf(true, false), false, ShortlinkViewer},
		// file shares: always forces direct regardless of UA
		{"file/always/browser", fileShareOf(false, true), true, ShortlinkDirect},
		{"file/always/non-browser", fileShareOf(false, true), false, ShortlinkDirect},
		// file shares: if both somehow true (should never happen post-normalization), block wins
		{"file/both/browser", fileShareOf(true, true), true, ShortlinkViewer},
		{"file/both/non-browser", fileShareOf(true, true), false, ShortlinkViewer},

		// folder shares: zip not allowed -> always viewer, full stop
		{"folder/zip-off/default/browser", folderShareOf(false, false, false), true, ShortlinkViewer},
		{"folder/zip-off/default/non-browser", folderShareOf(false, false, false), false, ShortlinkViewer},
		{"folder/zip-off/always/non-browser", folderShareOf(false, false, true), false, ShortlinkViewer},

		// folder shares: zip allowed, default follows UA (browser always viewer)
		{"folder/zip-on/default/browser", folderShareOf(true, false, false), true, ShortlinkViewer},
		{"folder/zip-on/default/non-browser", folderShareOf(true, false, false), false, ShortlinkZip},
		// folder shares: zip allowed + block -> always viewer
		{"folder/zip-on/block/browser", folderShareOf(true, true, false), true, ShortlinkViewer},
		{"folder/zip-on/block/non-browser", folderShareOf(true, true, false), false, ShortlinkViewer},
		// folder shares: zip allowed + always -> zip for everyone, including browsers
		{"folder/zip-on/always/browser", folderShareOf(true, false, true), true, ShortlinkZip},
		{"folder/zip-on/always/non-browser", folderShareOf(true, false, true), false, ShortlinkZip},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decide(tc.share, tc.browser); got != tc.want {
				t.Errorf("decide(...) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShortlinkRedirectPath(t *testing.T) {
	cases := []struct {
		name string
		res  *ShortlinkResolution
		want string
	}{
		{
			"viewer",
			&ShortlinkResolution{Action: ShortlinkViewer, Share: &fileShare{}},
			"/share/abc123",
		},
		{
			"not found",
			&ShortlinkResolution{Action: ShortlinkNotFound},
			"/share/abc123",
		},
		{
			"direct",
			&ShortlinkResolution{Action: ShortlinkDirect, Share: &fileShare{
				FileShare: models.FileShare{FileId: "file-id"},
				Name:      "my file.pdf",
			}},
			"/api/shares/abc123/files/file-id/my%20file.pdf",
		},
		{
			"zip",
			&ShortlinkResolution{Action: ShortlinkZip, Share: &fileShare{}},
			"/api/shares/abc123/zip",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShortlinkRedirectPath("abc123", tc.res); got != tc.want {
				t.Errorf("ShortlinkRedirectPath(...) = %q, want %q", got, tc.want)
			}
		})
	}
}
