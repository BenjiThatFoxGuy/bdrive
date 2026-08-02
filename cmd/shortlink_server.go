package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/pkg/services"
	"go.uber.org/zap"
)

// requestContextKey stashes per-request data the shared reverseProxy's
// Rewrite and ModifyResponse funcs need to read: the target path is
// per-request (which file, which share), and cacheable records whether the
// resolved share is password-protected, decided once at resolution time so
// neither hook has to re-derive it.
type requestContextKey struct{}

type proxyRequestInfo struct {
	path      string
	cacheable bool
}

// cacheHeaders sets long-TTL, publicly-cacheable response headers for a
// direct-download or zip response, aimed at a CDN sitting in front of the
// alt domain (and the visitor's own browser) so a popular share's repeat
// requests don't all hit the origin.
//
// Cache-Control is the standard directive browsers and any generic shared
// cache honor. CDN-Cache-Control (a multi-CDN tiered-caching convention) and
// Cloudflare-CDN-Cache-Control (Cloudflare's own override of it, taking
// precedence when both are present) additionally let Cloudflare's edge cache
// on a different TTL than the browser does. None of the three headers turns
// caching on by itself - Cloudflare still needs a Cache Rule (or similar)
// marking this URL pattern as eligible before it caches anything; these only
// control the TTL once it's eligible. Setting Cloudflare-CDN-Cache-Control at
// all also switches Cloudflare to strict RFC 7234 authorization for this
// response, which per Cloudflare's own docs accepts only s-maxage,
// must-revalidate or public as directives - anything else in that specific
// header causes a bypass - so only that safe subset is used here, not a bare
// max-age.
//
// Deliberately not "immutable": a file's bytes can be edited in place after
// its share was created (FilesUpdate can rewrite content without changing
// the share or its shortlink), so a cache is expected to revalidate once
// max-age is up rather than treat this URL as permanent.
func cacheHeaders(h http.Header) {
	const maxAge = "31536000" // 1 year
	h.Set("Cache-Control", "public, max-age="+maxAge+", s-maxage="+maxAge)
	h.Set("CDN-Cache-Control", "public, s-maxage="+maxAge)
	h.Set("Cloudflare-CDN-Cache-Control", "public, s-maxage="+maxAge)
	// Not a Cloudflare header - an older Fastly/Varnish-originated convention
	// some other CDN setups still check. Harmless to include if ignored.
	h.Set("Surrogate-Control", "max-age="+maxAge)
}

// setupShortlinkServer builds the standalone alt-domain listener (entry
// point (b) of the shortlinks feature), fronted by the operator's own
// reverse proxy on a short branded subdomain, giving out links like
// https://go.example.com/{code} with no /share/ prefix.
//
// A resolution that needs the web viewer redirects there (main-domain SPA,
// nothing to proxy). A resolution that needs raw bytes - the direct-download
// or zip case - is reverse-proxied instead: the response is served in place
// at https://go.example.com/{code}, not handed off to the main domain. That
// keeps the URL a visitor already has working for non-browser clients (curl,
// download managers, video players doing Range requests) instead of routing
// them through a second origin.
//
// The proxy targets the main app's own listener on localhost, not its public
// base URL: both run in the same process, so routing a request back out
// through DNS, TLS and (usually) Cloudflare just to hairpin straight back in
// is pure overhead - and unreliable overhead, since exactly that round trip
// is what produced "ReverseProxy read error during body copy: unexpected
// EOF" on larger streamed responses. Authorization and Range headers forward
// automatically, so a password-protected share's Basic Auth challenge and
// range-seeking both keep working exactly as they do calling the main app
// directly.
//
// Bare "/" and any path that isn't a real shortlink code redirect to the
// main app's configured base URL - visiting the wrong thing on this domain
// always sends you home rather than showing a 404. The viewer case also
// redirects there: it needs the public URL specifically, since it's a real
// browser navigation to the main-domain SPA, not a server-to-server hop.
func setupShortlinkServer(cfg *config.ServerCmdConfig, resolver services.ShortlinkResolver, lg *zap.Logger) *http.Server {
	base := strings.TrimSuffix(cfg.Server.BaseURL, "/")
	local := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", cfg.Server.Port)}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(local)
			if info, ok := pr.In.Context().Value(requestContextKey{}).(proxyRequestInfo); ok {
				pr.Out.URL.Path = info.path
				pr.Out.URL.RawPath = ""
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if info, ok := resp.Request.Context().Value(requestContextKey{}).(proxyRequestInfo); ok && info.cacheable {
				cacheHeaders(resp.Header)
			}
			return nil
		},
		ErrorLog: zap.NewStdLog(lg),
	}

	resolve := func(w http.ResponseWriter, r *http.Request, code string) {
		if code != "" {
			if res, err := resolver.ResolveShortlink(r.Context(), code, r.UserAgent()); err == nil {
				path := services.ShortlinkRedirectPath(code, res)
				switch res.Action {
				case services.ShortlinkDirect, services.ShortlinkZip:
					// res.Share is nil only for ShortlinkNotFound/ShortlinkViewer in the
					// one real implementation, but ShortlinkResolver is an interface -
					// nothing stops another implementation (or a test double) from
					// returning a Direct/Zip action with no Share. Fail closed: an
					// unknown protection state doesn't get cached.
					cacheable := res.Share != nil && res.Share.Password == nil
					info := proxyRequestInfo{path: path, cacheable: cacheable}
					ctx := context.WithValue(r.Context(), requestContextKey{}, info)
					proxy.ServeHTTP(w, r.WithContext(ctx))
					return
				case services.ShortlinkViewer:
					http.Redirect(w, r, base+path, http.StatusFound)
					return
				}
			}
		}
		http.Redirect(w, r, base+"/", http.StatusFound)
	}

	mux := chi.NewRouter()
	mux.Use(chimiddleware.Recoverer)
	mux.Use(chimiddleware.RealIP)

	mux.Get("/", func(w http.ResponseWriter, r *http.Request) { resolve(w, r, "") })
	mux.Get("/{code}", func(w http.ResponseWriter, r *http.Request) { resolve(w, r, chi.URLParam(r, "code")) })
	mux.NotFound(func(w http.ResponseWriter, r *http.Request) { resolve(w, r, "") })
	mux.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) { resolve(w, r, "") })

	return &http.Server{
		Addr:              cfg.Shortlinks.ListenAddr,
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
