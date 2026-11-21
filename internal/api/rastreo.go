package api

import (
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
)

var websiteID = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

// TrackerTag is Umami's script for rendimiento's own pages, so the
// platform's visits are counted like any app's: umamiURL is Umami's public
// address (UMAMI_PUBLIC_URL) and id the website Umami gave rendimiento
// (UMAMI_WEBSITE_ID). Empty when either is missing or not what it should be.
func TrackerTag(umamiURL, id string) string {
	u, err := url.Parse(strings.TrimRight(umamiURL, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || !websiteID.MatchString(id) {
		return ""
	}
	return `<script defer src="` + html.EscapeString(u.String()+"/script.js") + `" data-website-id="` + html.EscapeString(id) + `"></script>`
}

// withTracker is the page with the tracker's tag at the end of its head.
func withTracker(page []byte, tag string) []byte {
	return []byte(strings.Replace(string(page), "</head>", tag+"</head>", 1))
}

// withCountryLang is the page with the language of the visitor's country
// (i18n.CountryDefault) at the end of its head, for the UI to start in when
// none of the browser's languages is Spanish or English (web/src/i18n.ts).
func withCountryLang(page []byte, l i18n.Lang) []byte {
	return []byte(strings.Replace(string(page), "</head>", `<meta name="rendimiento-lang-pais" content="`+string(l)+`" /></head>`, 1))
}
