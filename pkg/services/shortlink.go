package services

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/uaclass"
)

// ShortlinkAction is the outcome ResolveShortlink decides for a given
// shortlink code and User-Agent.
type ShortlinkAction int

const (
	// ShortlinkNotFound means the code doesn't resolve to a share (unknown
	// or expired) — callers should fall through to whatever they'd normally
	// do for an unrecognized path.
	ShortlinkNotFound ShortlinkAction = iota
	// ShortlinkViewer means the requester should see the normal web viewer.
	ShortlinkViewer
	// ShortlinkDirect means the requester should get the raw file bytes.
	ShortlinkDirect
	// ShortlinkZip means the requester should get a zip stream of the share.
	ShortlinkZip
)

// ShortlinkResolution is the result of resolving a shortlink code.
type ShortlinkResolution struct {
	Action ShortlinkAction
	Share  *fileShare // nil when Action == ShortlinkNotFound
}

// ShortlinkResolver is implemented by *apiService. It's exported as an
// interface (rather than exporting apiService itself) purely so cmd/run.go
// and cmd/shortlink_server.go can pass the same service instance between the
// two HTTP entry points without needing to name the unexported concrete type.
type ShortlinkResolver interface {
	ResolveShortlink(ctx context.Context, code, userAgent string) (*ShortlinkResolution, error)
}

// decide is the pure decision tree behind ResolveShortlink, split out so it
// can be unit-tested without a database or cache.
//
// File-type shares: BlockDirectLink and AlwaysDirectLink are mutually
// exclusive (enforced at write time in FilesCreateShare/FilesEditShare);
// with neither set, the outcome follows the User-Agent.
//
// Folder-type shares: AllowZipDownload is the master gate. Off, the share
// is viewer-only for everyone, full stop — there is no folder equivalent of
// AlwaysDirectLink that would force a zip on a browser. On, the same
// block/always pair applies, now targeting the zip stream instead of a raw
// file.
func decide(share *fileShare, isBrowser bool) ShortlinkAction {
	directTarget := ShortlinkDirect
	if share.Type == api.FileShareInfoTypeFolder {
		if !share.AllowZipDownload {
			return ShortlinkViewer
		}
		directTarget = ShortlinkZip
	}

	switch {
	case share.BlockDirectLink:
		return ShortlinkViewer
	case share.AlwaysDirectLink:
		return directTarget
	case !isBrowser:
		return directTarget
	default:
		return ShortlinkViewer
	}
}

// ResolveShortlink is the single decision function used by both public
// entry points (the main-domain /share/{token} interceptor and the
// standalone alt-domain listener). It looks up code as a shortlink only —
// never falling back to a bare uuid — and returns ShortlinkNotFound for an
// unknown or expired code so callers can fall through to their normal
// behavior instead of erroring.
func (a *apiService) ResolveShortlink(ctx context.Context, code, userAgent string) (*ShortlinkResolution, error) {
	share, err := cache.FetchArg(ctx, a.cache, cache.KeyShare(code), 0, a.shareGetByCode, code)
	if err != nil {
		if errors.Is(err, ErrShareNotFound) || errors.Is(err, ErrShareExpired) {
			return &ShortlinkResolution{Action: ShortlinkNotFound}, nil
		}
		return nil, err
	}

	action := decide(share, uaclass.LooksLikeBrowser(userAgent))
	return &ShortlinkResolution{Action: action, Share: share}, nil
}

// ShortlinkRedirectPath returns the relative path a resolved shortlink
// action should redirect (or pass through) to. The main-domain entry point
// uses this as-is; the standalone alt-domain listener prepends its
// configured base URL. It deliberately redirects using the shortlink token
// rather than the share's real uuid, keeping that id out of URLs the
// visitor sees — safe because /api/shares/{id} already accepts either.
func ShortlinkRedirectPath(token string, res *ShortlinkResolution) string {
	switch res.Action {
	case ShortlinkDirect:
		return fmt.Sprintf("/api/shares/%s/files/%s/%s", token, res.Share.FileId, url.PathEscape(res.Share.Name))
	case ShortlinkZip:
		return fmt.Sprintf("/api/shares/%s/zip", token)
	default: // ShortlinkViewer, ShortlinkNotFound
		return "/share/" + token
	}
}
