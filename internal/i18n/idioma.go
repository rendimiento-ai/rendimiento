package i18n

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Parse reads a language setting ("es", "en") or an Accept-Language header:
// the first language in the header's order of preference (q) that is
// Spanish or English; English when there is none.
func Parse(s string) Lang {
	if l, ok := preferred(s); ok {
		return l
	}
	return English
}

// preferred is the first Spanish or English language of an Accept-Language
// list, by its q values (equal ones keep their order); false when the list
// names neither.
func preferred(header string) (Lang, bool) {
	type choice struct {
		lang Lang
		q    float64
	}
	var found []choice
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(p), "q="); ok {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					q = f
				}
			}
		}
		primary, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		if q <= 0 || (primary != "es" && primary != "en") {
			continue
		}
		found = append(found, choice{Lang(primary), q})
	}
	if len(found) == 0 {
		return English, false
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].q > found[j].q })
	return found[0].lang, true
}

// spanishCountries are the countries where Spanish is spoken first (ISO 3166
// codes, as Cloudflare's CF-IPCountry header gives them).
var spanishCountries = map[string]bool{
	"MX": true, "ES": true, "AR": true, "CO": true, "CL": true, "PE": true, "VE": true,
	"EC": true, "GT": true, "CU": true, "BO": true, "DO": true, "HN": true, "PY": true,
	"SV": true, "NI": true, "CR": true, "PA": true, "UY": true, "PR": true, "GQ": true,
}

// ForCountry is the language for a visitor from a country: Spanish for the
// Spanish-speaking ones, else English.
func ForCountry(country string) Lang {
	if spanishCountries[strings.ToUpper(strings.TrimSpace(country))] {
		return Spanish
	}
	return English
}

// CountryDefault is the language of the country Cloudflare says the request
// comes from (CF-IPCountry), for a browser whose languages are neither
// Spanish nor English. False without the header (not behind Cloudflare) or
// without Accept-Language at all (a crawler or a script, which keep
// English). The header only picks this default; it is never kept or logged.
func CountryDefault(r *http.Request) (Lang, bool) {
	country := strings.TrimSpace(r.Header.Get("CF-IPCountry"))
	if country == "" || strings.TrimSpace(r.Header.Get("Accept-Language")) == "" {
		return English, false
	}
	return ForCountry(country), true
}

// FromRequest is the language to answer a request in: the first Spanish or
// English language the browser asks for (Accept-Language, which the web UI
// sets to the language chosen with its switch); else, for a browser that
// asks for other languages only, its country's (CountryDefault); else
// English.
func FromRequest(r *http.Request) Lang {
	if l, ok := preferred(r.Header.Get("Accept-Language")); ok {
		return l
	}
	if l, ok := CountryDefault(r); ok {
		return l
	}
	return English
}
