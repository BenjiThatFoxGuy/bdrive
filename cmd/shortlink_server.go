package cmd

import (
	"context"
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

// targetPathKey stashes the resolved upstream path on the request context so
// the shared reverseProxy's Rewrite func can read it - the target path is
// per-request (which file, which share), but the proxy itself is built once
// so it reuses connections to the upstream instead of dialing fresh per hit.
type targetPathKey struct{}

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
// them through a second origin. Authorization and Range headers forward
// automatically, so a password-protected share's Basic Auth challenge and
// range-seeking both keep working exactly as they do calling the main app
// directly.
//
// Bare "/" and any path that isn't a real shortlink code redirect to the
// main app's configured base URL - visiting the wrong thing on this domain
// always sends you home rather than showing a 404.
func setupShortlinkServer(cfg *config.ServerCmdConfig, resolver services.ShortlinkResolver, lg *zap.Logger) *http.Server {
	base := strings.TrimSuffix(cfg.Server.BaseURL, "/")

	upstream, err := url.Parse(base)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		// cmd.go's startup validation already requires a non-empty base-url
		// when shortlinks are enabled; this only catches a malformed value
		// (missing scheme, unparsable). Direct/zip resolutions fail closed to
		// the home redirect below rather than proxying to a broken target.
		lg.Error("shortlink_server.invalid_base_url", zap.String("base_url", base), zap.Error(err))
		upstream = nil
	}

	var proxy *httputil.ReverseProxy
	if upstream != nil {
		target := *upstream
		proxy = &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(&target)
				if path, ok := pr.In.Context().Value(targetPathKey{}).(string); ok {
					pr.Out.URL.Path = path
					pr.Out.URL.RawPath = ""
				}
			},
			ErrorLog: zap.NewStdLog(lg),
		}
	}

	resolve := func(w http.ResponseWriter, r *http.Request, code string) {
		if code != "" {
			if res, err := resolver.ResolveShortlink(r.Context(), code, r.UserAgent()); err == nil {
				path := services.ShortlinkRedirectPath(code, res)
				switch res.Action {
				case services.ShortlinkDirect, services.ShortlinkZip:
					if proxy != nil {
						ctx := context.WithValue(r.Context(), targetPathKey{}, path)
						proxy.ServeHTTP(w, r.WithContext(ctx))
						return
					}
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
