package uaclass

import "testing"

func TestLooksLikeBrowser(t *testing.T) {
	cases := []struct {
		name    string
		ua      string
		browser bool
	}{
		// known direct/scripted clients -> not browser
		{"curl", "curl/8.4.0", false},
		{"wget", "Wget/1.21.3 (linux-gnu)", false},
		{"python-requests", "python-requests/2.31.0", false},
		{"python-urllib", "Python-urllib/3.11", false},
		{"go-http-client", "Go-http-client/1.1", false},
		{"libcurl", "SomeLib libcurl/8.0.1", false},
		{"httpie", "HTTPie/3.2.2", false},
		{"powershell", "Mozilla/5.0 (Windows NT; PowerShell/7.4.0)", false},
		{"okhttp", "okhttp/4.12.0", false},
		{"aria2", "aria2/1.36.0", false},
		{"axios", "axios/1.6.0", false},
		{"node-fetch", "node-fetch/1.0 (+https://github.com/bitinn/node-fetch)", false},
		{"java", "Java/17.0.2", false},
		{"apache-httpclient", "Apache-HttpClient/4.5.13 (Java/11.0.1)", false},
		{"libwww-perl", "libwww-perl/6.67", false},

		// real browsers -> browser
		{"chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", true},
		{"firefox", "Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0", true},
		{"safari", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Safari/605.1.15", true},
		{"mobile safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Mobile/15E148 Safari/604.1", true},
		{"edge", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0", true},

		// ambiguous/unknown -> default to browser (safer failure mode)
		{"empty", "", true},
		{"unknown", "SomeWeirdClient/1.0", true},
		{"slackbot", "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)", true},
		{"discordbot", "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)", true},
		{"twitterbot", "Twitterbot/1.0", true},
		{"whatsapp", "WhatsApp/2.23.20.0", true},
		{"googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LooksLikeBrowser(tc.ua); got != tc.browser {
				t.Errorf("LooksLikeBrowser(%q) = %v, want %v", tc.ua, got, tc.browser)
			}
		})
	}
}
