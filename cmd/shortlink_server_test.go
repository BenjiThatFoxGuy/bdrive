package cmd

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// portOf extracts the numeric port httptest bound an upstream to, so a test
// server can stand in for "the main app's own listener on localhost" that
// setupShortlinkServer now always proxies to (cfg.Server.Port), instead of
// the configurable public base URL it used before.
func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Atoi(%q): %v", portStr, err)
	}
	return port
}

// Only ShortlinkZip and ShortlinkViewer/NotFound are exercised here:
// ShortlinkResolution.Share's type is unexported by package services, and
// that cuts two ways in this file. It can't be constructed from a cmd
// package test, so (a) ShortlinkDirect itself isn't separately covered -
// Zip and Direct share the exact proxy branch this test pins, so this is
// still full coverage of what changed - and (b) the "real Share, no
// password" case of the caching logic can't be exercised either: this test
// can only ever produce a nil Share, which is the fail-closed/not-cached
// path. That path is worth covering in its own right (it's the defensive
// branch guarding a nil deref), but the positive "aggressive cache headers
// are actually set" case has no coverage here and would need a test living
// in package services, where fileShare values can be constructed directly.
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
		Server:     config.ServerConfig{BaseURL: "https://drive.example.com", Port: portOf(t, upstream.Listener.Addr().String())},
		Shortlinks: config.ShortlinkConfig{Enabled: true, ListenAddr: ":0"},
	}

	t.Run("zip resolution is proxied to localhost, not redirected", func(t *testing.T) {
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

	t.Run("nil Share fails closed to no caching, doesn't panic", func(t *testing.T) {
		// The real ShortlinkResolver always populates Share for a Direct/Zip
		// action, but the interface doesn't require it. This is the only case
		// this test package can construct (ShortlinkResolution.Share's type is
		// unexported), and it doubles as the nil-Share regression case.
		resolver := &fakeResolver{resolutions: map[string]*services.ShortlinkResolution{
			"zipcode": {Action: services.ShortlinkZip},
		}}
		srv := setupShortlinkServer(cfg, resolver, zap.NewNop())

		req := httptest.NewRequest(http.MethodGet, "/zipcode", nil)
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		for _, h := range []string{"Cache-Control", "CDN-Cache-Control", "Cloudflare-CDN-Cache-Control", "Surrogate-Control"} {
			if got := rec.Header().Get(h); got != "" {
				t.Errorf("%s = %q, want unset when the share's protection state is unknown (nil Share)", h, got)
			}
		}
	})

	t.Run("viewer resolution redirects to the main app's public URL", func(t *testing.T) {
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
		if want, got := "https://drive.example.com/share/viewcode", rec.Header().Get("Location"); got != want {
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
		if want, got := "https://drive.example.com/", rec.Header().Get("Location"); got != want {
			t.Errorf("Location = %q, want %q", got, want)
		}
	})
}
