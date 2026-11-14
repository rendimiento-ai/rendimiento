// Package actualizar knows which rendimiento is published: once a day it
// asks GitHub for the latest release, and says whether it is newer than the
// one running, whether it only brings fixes (the same minor version: it may
// install itself, decision 138), and whether it is signed (only a signed
// one is ever installed).
package actualizar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Repo is where rendimiento's releases are published.
const Repo = "rendimiento-ai/rendimiento"

// Publicada is a published release, as the card shows it.
type Publicada struct {
	Version string    `json:"version"` // 0.2.1, without the v
	Notas   string    `json:"notas"`   // its notes, as written on GitHub
	URL     string    `json:"url"`
	Fecha   time.Time `json:"fecha"`
	Firmada bool      `json:"firmada"` // it carries SHA256SUMS.sig
}

// Version is a version as three numbers; ok is false for one that is not
// (a build from a branch: "dev").
func Version(v string) (n [3]int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "-") // 0.3.0-rc1: what it will be
	partes := strings.Split(v, ".")
	if len(partes) != 3 {
		return n, false
	}
	for i, p := range partes {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

// Comparar says whether nueva is newer than actual, and whether it only
// brings fixes: the same major and minor version (0.2.0 → 0.2.1). A version
// that is not three numbers is never newer.
func Comparar(actual, nueva string) (masNueva, soloArreglos bool) {
	a, okA := Version(actual)
	b, okB := Version(nueva)
	if !okA || !okB {
		return false, false
	}
	for i := range a {
		if b[i] != a[i] {
			masNueva = b[i] > a[i]
			break
		}
	}
	return masNueva, masNueva && a[0] == b[0] && a[1] == b[1]
}

// Consultor asks GitHub for the latest release, keeping the answer a day.
type Consultor struct {
	// API is GitHub's API (https://api.github.com; tests point it elsewhere).
	API  string
	HTTP *http.Client
	// Cada is how long an answer is kept (a day).
	Cada time.Duration

	mu     sync.Mutex
	ultima *Publicada
	cuando time.Time
	err    error
}

// Ultima is the latest published release; refrescar asks GitHub now.
func (c *Consultor) Ultima(ctx context.Context, refrescar bool) (*Publicada, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cada := c.Cada
	if cada == 0 {
		cada = 24 * time.Hour
	}
	if !refrescar && !c.cuando.IsZero() && time.Since(c.cuando) < cada {
		return c.ultima, c.err
	}
	c.ultima, c.err = c.consultar(ctx)
	c.cuando = time.Now()
	return c.ultima, c.err
}

func (c *Consultor) consultar(ctx context.Context) (*Publicada, error) {
	api := c.API
	if api == "" {
		api = "https://api.github.com"
	}
	cl := c.HTTP
	if cl == nil {
		cl = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(api, "/")+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asking GitHub for the latest release: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s for the latest release", res.Status)
	}
	var r struct {
		Tag       string    `json:"tag_name"`
		Body      string    `json:"body"`
		URL       string    `json:"html_url"`
		Published time.Time `json:"published_at"`
		Assets    []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("reading GitHub's latest release: %w", err)
	}
	p := &Publicada{Version: strings.TrimPrefix(r.Tag, "v"), Notas: r.Body, URL: r.URL, Fecha: r.Published}
	for _, a := range r.Assets {
		p.Firmada = p.Firmada || a.Name == "SHA256SUMS.sig"
	}
	return p, nil
}
