package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/pkg/services"
	"go.uber.org/zap"
)

// fakeResolver implements services.ShortlinkResolver without touching a
// database, returning canned resolutions by code.
type fakeResolver struct {
	resolutions map[string]*services.ShortlinkResolution
}

func (f *fakeResolver) ResolveShortlink(_ context.Context, code, _ string) (*services.ShortlinkResolution, error) {
	if res, ok := f.resolutions[code]; ok {
		return res, nil
	}
	return &services.ShortlinkResolution{Action: services.ShortlinkNotFound}, nil
}

// Only ShortlinkZip and ShortlinkViewer/NotFound are exercised here:
// ShortlinkDirect needs a populated ShortlinkResolution.Share, whose type is
// unexported by package services, so it can't be constructed from a cmd
// package test. Zip and Direct share the exact proxy branch this test is
// pinning (case services.ShortlinkDirect, services.ShortlinkZip:), so this is
// full coverage of the new behavior even though Direct's own path-building in
// ShortlinkRedirectPath (pre-existing, unrelated to this change) isn't hit.
func TestShortlinkServerResolve(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			w.Header().Set("X-Seen-Authorization", got)
		}
		if got := r.Header.Get("Range"); got != "" {
			w.Header().Set("X-Seen-Range", got)
		}
		w.Header().Set("X-Seen-Path", r.URL.Path)
		w.Header().Set("Content-Disposition", `attachment; filename="archive.zip"`)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "upstream-body")
	}))
	defer upstream.Close()

	cfg := &config.ServerCmdConfig{
		Server:     config.ServerConfig{BaseURL: upstream.URL},
		Shortlinks: config.ShortlinkConfig{Enabled: true, ListenAddr: ":0"},
	}

	t.Run("zip resolution is proxied in place, not redirected", func(t *testing.T) {
		resolver := &fakeResolver{resolutions: map[string]*services.ShortlinkResolution{
			"zipcode": {Action: services.ShortlinkZip},
		}}
		srv := setupShortlinkServer(cfg, resolver, zap.NewNop())

		req := httptest.NewRequest(http.MethodGet, "/zipcode", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		req.Header.Set("Range", "bytes=0-99")
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (proxied response, not a redirect)", rec.Code, http.StatusOK)
		}
		if got := rec.Body.String(); got != "upstream-body" {
			t.Errorf("body = %q, want the upstream body relayed verbatim", got)
		}
		if got := rec.Header().Get("Content-Disposition"); got == "" {
			t.Error("Content-Disposition header from upstream was not relayed to the client")
		}
		if got := rec.Header().Get("X-Seen-Path"); got != "/api/shares/zipcode/zip" {
			t.Errorf("upstream saw path %q, want /api/shares/zipcode/zip", got)
		}
		if got := rec.Header().Get("X-Seen-Authorization"); got != "Basic dXNlcjpwYXNz" {
			t.Errorf("Authorization header was not forwarded to upstream, got %q", got)
		}
		if got := rec.Header().Get("X-Seen-Range"); got != "bytes=0-99" {
			t.Errorf("Range header was not forwarded to upstream, got %q", got)
		}
	})

	t.Run("viewer resolution redirects to the main app", func(t *testing.T) {
		resolver := &fakeResolver{resolutions: map[string]*services.ShortlinkResolution{
			"viewcode": {Action: services.ShortlinkViewer},
		}}
		srv := setupShortlinkServer(cfg, resolver, zap.NewNop())

		req := httptest.NewRequest(http.MethodGet, "/viewcode", nil)
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
		}
		if want, got := upstream.URL+"/share/viewcode", rec.Header().Get("Location"); got != want {
			t.Errorf("Location = %q, want %q", got, want)
		}
	})

	t.Run("unknown code redirects home rather than proxying", func(t *testing.T) {
		srv := setupShortlinkServer(cfg, &fakeResolver{}, zap.NewNop())

		req := httptest.NewRequest(http.MethodGet, "/nope", nil)
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
		}
		if want, got := upstream.URL+"/", rec.Header().Get("Location"); got != want {
			t.Errorf("Location = %q, want %q", got, want)
		}
	})
}
