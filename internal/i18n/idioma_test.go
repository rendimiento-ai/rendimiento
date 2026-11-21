package i18n

import (
	"net/http/httptest"
	"testing"
)

func TestParseOrder(t *testing.T) {
	for in, want := range map[string]Lang{
		"es;q=0.2, en;q=0.9":                  English,
		"en;q=0.1, es-AR;q=0.8":               Spanish,
		"pt-BR, fr;q=0.9, es;q=0.5, en;q=0.4": Spanish,
		"fr, en-GB;q=0.5, es;q=0.5":           English,
		"es;q=0, en":                          English,
		"*":                                   English,
		"pt-BR,pt;q=0.9":                      English,
		" ES-mx ":                             Spanish,
		"estonian":                            English,
	} {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
}

// The browser's languages decide; the country only when they are neither
// Spanish nor English; a request without Accept-Language (a crawler) stays
// English wherever it comes from.
func TestFromRequest(t *testing.T) {
	for _, c := range []struct {
		accept, country string
		want            Lang
	}{
		{"es-MX,es;q=0.9", "US", Spanish},
		{"en-US", "MX", English},
		{"pt-BR,pt;q=0.9", "MX", Spanish},
		{"pt-BR", "BR", English},
		{"de", "es", Spanish},
		{"pt-BR", "", English},
		{"", "MX", English},
		{"fr, en;q=0.3", "AR", English},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept-Language", c.accept)
		}
		if c.country != "" {
			r.Header.Set("CF-IPCountry", c.country)
		}
		if got := FromRequest(r); got != c.want {
			t.Errorf("FromRequest(%q, %q) = %q, want %q", c.accept, c.country, got, c.want)
		}
		if _, ok := CountryDefault(r); ok != (c.country != "" && c.accept != "") {
			t.Errorf("CountryDefault(%q, %q) = %v", c.accept, c.country, ok)
		}
	}
}
