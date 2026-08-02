package cmd

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/pkg/services"
	"go.uber.org/zap"
)

// setupShortlinkServer builds the standalone alt-domain listener (entry
// point (b) of the shortlinks feature): a pure resolver, never the app
// itself. The operator fronts it with their own reverse proxy on a short
// branded subdomain, giving out links like https://go.example.com/{code}
// with no /share/ prefix. Bare "/" and any path that isn't a real
// shortlink code both redirect to the main app's configured base URL —
// visiting the wrong thing on this domain always sends you home rather than
// showing a 404.
func setupShortlinkServer(cfg *config.ServerCmdConfig, resolver services.ShortlinkResolver, lg *zap.Logger) *http.Server {
	base := strings.TrimSuffix(cfg.Server.BaseURL, "/")

	resolve := func(w http.ResponseWriter, r *http.Request, code string) {
		if code != "" {
			if res, err := resolver.ResolveShortlink(r.Context(), code, r.UserAgent()); err == nil && res.Action != services.ShortlinkNotFound {
				http.Redirect(w, r, base+services.ShortlinkRedirectPath(code, res), http.StatusFound)
				return
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
