// Package senales sends this installation's señales de vida: once a week, a
// few anonymous counts to rendimiento.ai, so the project knows how many
// installations are alive, and nothing else. What goes is exactly Senal:
// a random identity, the version, the mode, the engine, the system, and
// seven days of counts; never a name, a repository, a person or a secret.
// The operators see the next one before it goes, and turn it off with a
// click or SENALES=off. See the book's page on them.
package senales

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"net/http"
	"runtime"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// URL is where señales go by default.
const URL = "https://rendimiento.ai/api/senales"

// Senal is one señal, as it is sent.
type Senal struct {
	Instalacion    string               `json:"instalacion"`
	Version        string               `json:"version"`
	Modo           string               `json:"modo"`  // local | kubernetes
	Motor          string               `json:"motor"` // containerd | docker | kubernetes
	SO             string               `json:"so"`
	Arquitectura   string               `json:"arquitectura"`
	Semana         store.ConteosSenales `json:"semana"`
	Organizaciones int                  `json:"organizaciones"`
	Personas       string               `json:"personas"` // a range: 1-5, 6-20, 21-100, 100+
	Equipo         bool                 `json:"equipo,omitempty"`
}

// Store is what the sender reads and keeps (*store.Store).
type Store interface {
	GetSenales(ctx context.Context) (store.SenalesAjustes, error)
	SaveSenales(ctx context.Context, a store.SenalesAjustes, by string) error
	Conteos(ctx context.Context) (store.ConteosSenales, error)
}

// Enviador sends the señales of this installation.
type Enviador struct {
	Store   Store
	URL     string // URL when empty
	Version string
	Modo    string
	Motor   string
	// Equipo marks the team's own installations (SENALES_EQUIPO): kept
	// apart, never in the numbers that count.
	Equipo bool
	// Off is SENALES=off: nothing is ever sent, whatever the setting says.
	Off  bool
	HTTP *http.Client
	Log  *slog.Logger
	// Cada is the time between señales (a week), Primera the wait before
	// the first one of an installation (ten minutes, so a person sees the
	// notice before it goes).
	Cada, Primera time.Duration
}

// Rango is how many people use an installation, as a range.
func Rango(n int) string {
	switch {
	case n <= 5:
		return "1-5"
	case n <= 20:
		return "6-20"
	case n <= 100:
		return "21-100"
	}
	return "100+"
}

// Proxima is the señal that would go now: what the operators see.
func (e *Enviador) Proxima(ctx context.Context) (Senal, error) {
	a, err := e.Store.GetSenales(ctx)
	if err != nil {
		return Senal{}, err
	}
	c, err := e.Store.Conteos(ctx)
	if err != nil {
		return Senal{}, err
	}
	v := e.Version
	if v == "" {
		v = "dev"
	}
	return Senal{Instalacion: a.Identidad, Version: v, Modo: e.Modo, Motor: e.Motor, SO: runtime.GOOS, Arquitectura: runtime.GOARCH,
		Semana: c, Organizaciones: c.Organizaciones, Personas: Rango(c.Personas), Equipo: e.Equipo}, nil
}

// Encendidas says whether señales go, and if not, why.
func (e *Enviador) Encendidas(ctx context.Context) (bool, string, error) {
	if e.Off {
		return false, "SENALES=off", nil
	}
	a, err := e.Store.GetSenales(ctx)
	if err != nil {
		return false, "", err
	}
	if a.Apagadas {
		return false, "turned off by " + a.UpdatedBy, nil
	}
	return true, "", nil
}

// Enviar sends the señal now and records when.
func (e *Enviador) Enviar(ctx context.Context) error {
	s, err := e.Proxima(ctx)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(s)
	url := e.URL
	if url == "" {
		url = URL
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "rendimiento/"+s.Version)
	client := e.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s answered %d", url, res.StatusCode)
	}
	a, err := e.Store.GetSenales(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	a.UltimoEnvio = &now
	return e.Store.SaveSenales(context.WithoutCancel(ctx), a, a.UpdatedBy)
}

// Run sends a señal when one is due, until ctx ends: the first a few
// minutes after the installation starts, then once every Cada, a few hours
// either way so they don't all come at once. Turned off, it only waits.
// A failure is logged and tried again in an hour; it never touches
// anything else.
func (e *Enviador) Run(ctx context.Context) {
	cada, primera := e.Cada, e.Primera
	if cada <= 0 {
		cada = 7 * 24 * time.Hour
	}
	if primera <= 0 {
		primera = 10 * time.Minute
	}
	for {
		espera := time.Hour
		if on, _, err := e.Encendidas(ctx); err == nil && on {
			if a, err := e.Store.GetSenales(ctx); err == nil {
				if a.UltimoEnvio == nil {
					espera = primera
				} else {
					espera = time.Until(a.UltimoEnvio.Add(cada)) + time.Duration(mathrand.Int64N(int64(6*time.Hour)))
				}
			}
		}
		if espera < 0 {
			espera = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(espera):
		}
		on, _, err := e.Encendidas(ctx)
		if err != nil || !on {
			continue
		}
		if a, err := e.Store.GetSenales(ctx); err != nil || (a.UltimoEnvio != nil && time.Since(*a.UltimoEnvio) < cada-time.Hour) {
			continue // another process sent it meanwhile
		}
		if err := e.Enviar(ctx); err != nil && e.Log != nil {
			e.Log.Warn("señal de vida not sent; trying again in an hour", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour):
			}
		}
	}
}
