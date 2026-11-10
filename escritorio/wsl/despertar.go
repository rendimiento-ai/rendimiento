package wsl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

var (
	// paciencia is how long Despertar waits for rendimiento: its first
	// start pulls Postgres and a registry's image, and a start can fail
	// once and be tried again by systemd 10 seconds later.
	paciencia = 8 * time.Minute
	pausa     = 2 * time.Second
)

// Despertar starts the distribution, keeps it awake (WSL stops one with
// no wsl.exe attached) and waits until rendimiento answers.
func (d *Distro) Despertar(ctx context.Context) error {
	if err := d.velar(ctx); err != nil {
		return err
	}
	// Reading who may sign in starts the distribution, so wsl.exe says at
	// once why it cannot; and without ALLOWED_USERS rendimiento would
	// never answer, however long the wait.
	env, err := d.dentro(ctx, nil, "sh", "-c", leerEnv)
	if err != nil {
		d.Soltar()
		return err
	}
	usuario := usuarioDe(env)
	if usuario == "" {
		d.Soltar()
		_ = os.Remove(d.marca())
		return &Problema{Clave: Arranque, Detalle: "nobody may sign in yet: /etc/rendimiento/env has no ALLOWED_USERS"}
	}
	d.marcar(usuario)

	limite := time.NewTimer(paciencia)
	defer limite.Stop()
	for {
		if d.responde(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-limite.C:
			// It stays awake: it may still be pulling its images, and a
			// second Despertar finds it further along.
			return &Problema{Clave: Arranque, Detalle: d.bitacora(ctx)}
		case <-time.After(pausa):
		}
	}
}

// velar attaches a wsl.exe that keeps the distribution awake. It always
// starts a new one before ending the one before: that one may have died
// with a wsl --shutdown, and there is no asking it.
func (d *Distro) velar(ctx context.Context) error {
	// It has to outlive this call, whatever ctx does afterwards.
	detener, err := d.Ejecutor.Iniciar(context.WithoutCancel(ctx), programa, "-d", d.Nombre, "--exec", "sleep", "infinity")
	if err != nil {
		return &Problema{Clave: Otro, Detalle: err.Error()}
	}
	d.mu.Lock()
	anterior := d.detener
	d.detener = detener
	d.mu.Unlock()
	if anterior != nil {
		anterior()
	}
	return nil
}

// bitacora is what to tell about a rendimiento that did not answer: its
// last lines in the journal.
func (d *Distro) bitacora(ctx context.Context) string {
	detalle := fmt.Sprintf("no answer on %s after %s", d.Direccion, paciencia)
	if out, err := d.dentro(ctx, nil, "journalctl", "-u", "rendimiento", "-n", "5", "--no-pager", "-o", "cat"); err == nil && out != "" {
		detalle += "\n" + cola(out, 1200)
	}
	return detalle
}

// responde reports whether rendimiento answers on Direccion.
func (d *Distro) responde(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, esperaSalud)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.Direccion+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := d.cliente().Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Dormir stops the distribution: its apps, its builds and the memory it
// held. Its data stays.
func (d *Distro) Dormir(ctx context.Context) error {
	d.Soltar()
	_, err := d.pedir(ctx, esperaWSL, nil, "--terminate", d.Nombre)
	return err
}

// Soltar ends what keeps the distribution awake, without stopping it at
// once: for when the app exits.
func (d *Distro) Soltar() {
	d.mu.Lock()
	detener := d.detener
	d.detener = nil
	d.mu.Unlock()
	if detener != nil {
		detener()
	}
}

// Configurado says whether rendimiento already has its GitHub App (GET
// /api/setup/status): until then the app opens the setup page.
func (d *Distro) Configurado(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, esperaHTTP)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.Direccion+"/api/setup/status", nil)
	if err != nil {
		return false, err
	}
	resp, err := d.cliente().Do(req)
	if err != nil {
		return false, fmt.Errorf("setup status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("setup status: %s", resp.Status)
	}
	var estado struct {
		Configured bool `json:"configured"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&estado); err != nil {
		return false, fmt.Errorf("setup status: %w", err)
	}
	return estado.Configured, nil
}
