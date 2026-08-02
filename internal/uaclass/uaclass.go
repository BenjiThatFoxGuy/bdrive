// Package uaclass classifies an HTTP User-Agent as browser-like or as a
// known scripted/direct-download client, for shortlink resolution.
package uaclass

import "strings"

// knownDirectClients matches User-Agent substrings (case-insensitively) for
// tools that fetch a URL and expect raw bytes back, never an HTML page.
// Anything NOT matched here is treated as browser-like — see
// LooksLikeBrowser.
var knownDirectClients = []string{
	"curl/",
	"wget/",
	"python-requests/",
	"python-urllib/",
	"go-http-client/",
	"libcurl",
	"httpie/",
	"powershell/",
	"okhttp/",
	"aria2/",
	"axios/",
	"node-fetch",
	"java/",
	"apache-httpclient/",
	"libwww-perl/",
}

// LooksLikeBrowser reports whether ua should be treated as a browser for
// shortlink resolution purposes. It defaults to true (browser-like) for any
// UA that doesn't match a known scripted/direct-download client — including
// empty UAs, unfamiliar clients, and link-unfurl bots (Slackbot, Discordbot,
// TwitterBot, WhatsApp, ...), all of which need the HTML viewer, not raw
// bytes. This is a deliberate allowlist-of-exceptions design, not an
// attempt to enumerate every browser: the cost of a false negative (an
// unusual scripted client getting the viewer instead of raw bytes) is one
// extra redirect hop, never a leak.
func LooksLikeBrowser(ua string) bool {
	lower := strings.ToLower(ua)
	for _, needle := range knownDirectClients {
		if strings.Contains(lower, needle) {
			return false
		}
	}
	return true
}
