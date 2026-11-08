package senales

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

type memoria struct{ a store.SenalesAjustes }

func (m *memoria) GetSenales(context.Context) (store.SenalesAjustes, error) { return m.a, nil }
func (m *memoria) SaveSenales(_ context.Context, a store.SenalesAjustes, by string) error {
	a.Identidad, a.UpdatedBy = m.a.Identidad, by
	m.a = a
	return nil
}
func (m *memoria) Conteos(context.Context) (store.ConteosSenales, error) {
	return store.ConteosSenales{Apps: 3, Ejecuciones: 41, Versiones: 9, AgentesConLlave: 2, LlamadasDeAgentes: 318, Organizaciones: 1, Personas: 7}, nil
}

// A señal carries exactly the counts, the identity and where it runs, and
// nothing that names anything; sending it records when; off, nothing goes.
func TestSenal(t *testing.T) {
	var recibido []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recibido, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	m := &memoria{a: store.SenalesAjustes{Identidad: "8f3c2a9e1d474b6a9e0c5a7d2f1b3c84"}}
	e := &Enviador{Store: m, URL: srv.URL, Version: "0.1.4", Modo: "local", Motor: "docker", Equipo: true}
	if err := e.Enviar(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(recibido, &got); err != nil {
		t.Fatal(err)
	}
	semana, _ := got["semana"].(map[string]any)
	if got["instalacion"] != "8f3c2a9e1d474b6a9e0c5a7d2f1b3c84" || got["version"] != "0.1.4" || got["modo"] != "local" || got["motor"] != "docker" ||
		got["personas"] != "6-20" || got["organizaciones"] != float64(1) || got["equipo"] != true || semana["apps"] != float64(3) || semana["llamadas_de_agentes"] != float64(318) {
		t.Fatalf("sent: %s", recibido)
	}
	// Only these fields, so nothing else slips in later unnoticed.
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	for _, k := range keys {
		if !strings.Contains(" instalacion version modo motor so arquitectura semana organizaciones personas equipo ", " "+k+" ") {
			t.Errorf("an unexpected field: %s", k)
		}
	}
	if len(semana) != 5 {
		t.Errorf("the week's counts: %v", semana)
	}
	if m.a.UltimoEnvio == nil {
		t.Fatal("the send was not recorded")
	}

	m.a.Apagadas = true
	if on, why, _ := e.Encendidas(context.Background()); on || why == "" {
		t.Fatalf("turned off: %v %q", on, why)
	}
	m.a.Apagadas, e.Off = false, true
	if on, why, _ := e.Encendidas(context.Background()); on || why != "SENALES=off" {
		t.Fatalf("SENALES=off: %v %q", on, why)
	}
}

func TestRango(t *testing.T) {
	for n, want := range map[int]string{1: "1-5", 5: "1-5", 6: "6-20", 20: "6-20", 21: "21-100", 101: "100+"} {
		if got := Rango(n); got != want {
			t.Errorf("%d: %s, want %s", n, got, want)
		}
	}
}
