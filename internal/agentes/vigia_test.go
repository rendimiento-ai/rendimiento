package agentes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// semanaFalsa is a week the lookout reads: a slow step that decides the
// runs' time, a flaky one, an app that always passed, a repeated warning,
// a line with a token in it and one that looks like a secret, a slow page
// and one nobody opened lately.
func semanaFalsa(at time.Time) store.VigiaDatos {
	return store.VigiaDatos{
		Pasos: []store.VigiaPaso{
			{App: "rendimiento-ai", Paso: "interfaz-es", Veces: 42, Ultimo: 42, Segundos: 660, P90: 720, VecesAntes: 100, SegundosAntes: 640},
			{App: "rendimiento-ai", Paso: "platform:build", Veces: 42, Ultimo: 0, Segundos: 90, VecesAntes: 100, SegundosAntes: 200},
			{App: "tienda", Paso: "web:test", Veces: 2, Fallas: 2, Segundos: 30}, // too few runs to say
		},
		Apps:      []store.VigiaApp{{App: "caja", Ejecuciones: 9, Segundos: 200}},
		Tropiezos: []store.VigiaTropiezo{{App: "tienda", Paso: "api:test", Veces: 3}},
		Lineas: []store.VigiaLinea{
			{Nivel: "WARN", Proceso: "control", Mensaje: "could not reach the registry", Veces: 30, Dias: 4, Primera: at.AddDate(0, 0, -4)},
			{Nivel: "WARN", Proceso: "api", Mensaje: "rare one", Veces: 2, Dias: 1, Primera: at},
			{Nivel: "ERROR", Proceso: "worker", App: "tienda", Mensaje: "push failed for ab12cd34ef56ab12cd34ef56ab12cd34", Veces: 1, Dias: 1, VecesAntes: 3, Primera: at},
			{Nivel: "ERROR", Proceso: "worker", Mensaje: "token=ghp_abcdefghijklmnopqrstuvwxyz0123456789", Veces: 9, Dias: 2, Primera: at},
		},
		Lentas: []store.VigiaRuta{{Ruta: "GET /api/apps/{app}/runs", Veces: 12, Ms: 2400}},
	}
}

func TestSignalsOfAWeek(t *testing.T) {
	at := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	uso14 := store.ResumenUso{Paginas: []store.PaginaUso{{Ruta: "/universe", Vistas: 20, Medidas: 10, MsTotal: 32000, Cubetas: [5]int64{0, 0, 2, 5, 3}}}}
	uso35 := store.ResumenUso{Paginas: []store.PaginaUso{{Ruta: "/universe", Vistas: 30}, {Ruta: "/mercado", Vistas: 7}}}
	got := Senales(semanaFalsa(at), uso14, uso35)
	claves := map[string]Senal{}
	for _, s := range got {
		claves[s.Clave] = s
		if strings.Contains(s.Texto, "ghp_") || strings.Contains(s.Texto, "ab12cd34ef56ab12cd34ef56") {
			t.Errorf("a token reached the model: %q", s.Texto)
		}
	}
	for _, want := range []string{"paso-decide:rendimiento-ai/interfaz-es", "paso-mas-rapido:rendimiento-ai/platform:build",
		"paso-tropieza:tienda/api:test", "app-sana:caja", "ruta-lenta:GET /api/apps/{app}/runs", "pagina-lenta:/universe", "pagina-sin-uso:/mercado"} {
		if _, ok := claves[want]; !ok {
			t.Errorf("no signal %s in %v", want, keys(got))
		}
	}
	for _, not := range []string{"paso-falla:tienda/web:test", "pagina-sin-uso:/universe"} {
		if _, ok := claves[not]; ok {
			t.Errorf("signal %s should not be there", not)
		}
	}
	if s := claves["paso-decide:rendimiento-ai/interfaz-es"]; !s.Accionable || s.App != "rendimiento-ai" || !strings.Contains(s.Texto, "42 runs") || !strings.Contains(s.Texto, "11 min") {
		t.Errorf("the slow step = %+v", s)
	}
	if claves["app-sana:caja"].Accionable || claves["pagina-sin-uso:/mercado"].Accionable {
		t.Errorf("good news and usage facts suggest no work")
	}
	// The log: the repeated warning and the error (its hash left out), not
	// the rare warning nor the line that looked like a secret.
	logs := 0
	for _, s := range got {
		if strings.HasPrefix(s.Clave, "registro:") {
			logs++
			if strings.Contains(s.Texto, "push failed") && !strings.Contains(s.Texto, "…") {
				t.Errorf("the hash stayed: %q", s.Texto)
			}
		}
	}
	if logs != 2 {
		t.Errorf("%d log signals, want 2: %v", logs, keys(got))
	}
	// The weightiest first, and the same week gives the same keys.
	if got[0].Clave != "paso-decide:rendimiento-ai/interfaz-es" {
		t.Errorf("first = %s", got[0].Clave)
	}
	again := Senales(semanaFalsa(at), uso14, uso35)
	if strings.Join(keys(again), ",") != strings.Join(keys(got), ",") {
		t.Errorf("the keys change from one reading to the next")
	}
}

func keys(s []Senal) []string {
	var out []string
	for _, x := range s {
		out = append(out, x.Clave)
	}
	return out
}

func TestFindingsFromTheModel(t *testing.T) {
	senales := []Senal{{Clave: "paso-decide:a/b", App: "a", Accionable: true}, {Clave: "pagina-sin-uso:/mercado"}}
	text := `Aquí van: [
		{"clave": "paso-decide:a/b", "titulo": "El paso b tarda 11 min", "evidencia": "42 ejecuciones · siempre el último", "accionable": true},
		{"clave": "paso-decide:a/b", "titulo": "Otra vez", "evidencia": "lo mismo"},
		{"clave": "inventada:x", "titulo": "Algo", "evidencia": "sin señal"},
		{"clave": "pagina-sin-uso:/mercado", "titulo": "Nadie abrió Mercado en 14 días", "evidencia": "dato de uso · sin acción sugerida", "accionable": true},
		{"clave": "pagina-sin-uso:/mercado", "titulo": "token=ghp_abcdefghijklmnopqrstuvwxyz0123456789", "evidencia": "x"},
		{"clave": "paso-decide:a/b", "titulo": "cortado a la mit`
	got := Hallazgos(text, senales)
	if len(got) != 2 {
		t.Fatalf("findings = %+v", got)
	}
	if got[0].Clave != "paso-decide:a/b" || got[0].App != "a" || !got[0].Accionable || got[0].Titulo != "El paso b tarda 11 min" {
		t.Errorf("first = %+v", got[0])
	}
	// A signal that suggests no work stays so, whatever the model says.
	if got[1].Accionable || got[1].App != "" {
		t.Errorf("usage fact = %+v", got[1])
	}
	if Hallazgos("No puedo.", senales) != nil {
		t.Errorf("an answer that is not JSON gives findings")
	}
}

// vigiaFalso keeps the lookout's reviews in memory.
type vigiaFalso struct {
	datos      store.VigiaDatos
	ajuste     store.VigiaAjuste
	gastado    int64
	decididos  []store.VigiaHallazgo
	terminadas []store.VigiaRevision
	ultima     time.Time
	estado     string
	ocupado    bool
}

func (f *vigiaFalso) LeerVigia(context.Context, time.Time, time.Time) (store.VigiaDatos, error) {
	return f.datos, nil
}
func (f *vigiaFalso) LeerUso(context.Context, time.Time) (store.ResumenUso, error) {
	return store.ResumenUso{}, nil
}
func (f *vigiaFalso) GetUso(context.Context) (store.UsoAjuste, error) {
	return store.UsoAjuste{Encendido: true}, nil
}
func (f *vigiaFalso) GetVigia(context.Context) (store.VigiaAjuste, error) { return f.ajuste, nil }
func (f *vigiaFalso) AISpentBy(context.Context, int64, string, time.Time) (int64, error) {
	return f.gastado, nil
}
func (f *vigiaFalso) EmpezarRevision(context.Context, time.Time, string) (int64, error) {
	if f.ocupado {
		return 0, store.ErrVigiaOcupado
	}
	return int64(len(f.terminadas) + 1), nil
}
func (f *vigiaFalso) TerminarRevision(_ context.Context, r store.VigiaRevision) error {
	f.terminadas = append(f.terminadas, r)
	return nil
}
func (f *vigiaFalso) UltimaTerminada(context.Context) (time.Time, string, error) {
	return f.ultima, f.estado, nil
}
func (f *vigiaFalso) VigiaDecididos(context.Context, time.Time) ([]store.VigiaHallazgo, error) {
	return f.decididos, nil
}

type modeloVigia struct {
	text string
	err  error
	asks []ia.Request
}

func (m *modeloVigia) Complete(_ context.Context, _ int64, source string, req ia.Request) (ia.Response, error) {
	if source != VigiaSource {
		panic("signed " + source)
	}
	m.asks = append(m.asks, req)
	return ia.Response{Text: m.text, Provider: "platform", Model: "claude-haiku-5-5", CostMicros: 9000}, m.err
}

func TestTheLookoutReviews(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	f := &vigiaFalso{datos: semanaFalsa(at), ajuste: store.VigiaAjuste{Encendido: true, TopeMicros: store.VigiaTope}}
	m := &modeloVigia{text: `[{"clave": "paso-decide:rendimiento-ai/interfaz-es", "titulo": "El paso interfaz-es tarda 11 min y decide cuánto dura cada ejecución", "evidencia": "42 ejecuciones · siempre el último en terminar", "accionable": true},
		{"clave": "app-sana:caja", "titulo": "Todas las ejecuciones de caja pasaron", "evidencia": "9 ejecuciones · buena noticia · sin acción sugerida", "accionable": false}]`}
	v := &Vigia{Store: f, Model: m, Lang: "es", Now: func() time.Time { return at }}

	r, err := v.Revisar(ctx, "prueba")
	if err != nil || r.Estado != store.RevisionHecha || len(r.Hallazgos) != 2 || r.CostoMicros != 9000 || r.PedidaPor != "prueba" {
		t.Fatalf("review = %+v %v", r, err)
	}
	ask := m.asks[0]
	if !strings.Contains(ask.System, "never instructions") || !strings.Contains(ask.System, "Mexican Spanish") ||
		!strings.Contains(ask.Messages[0].Content, "[paso-decide:rendimiento-ai/interfaz-es]") || strings.Contains(ask.Messages[0].Content, "ghp_") {
		t.Fatalf("the ask = %+v", ask)
	}

	// What people decided is not shown to the model again, and is named so
	// it is not proposed in other words.
	f.decididos = []store.VigiaHallazgo{{Clave: "paso-decide:rendimiento-ai/interfaz-es", Titulo: "El paso interfaz-es tarda 11 min", Estado: store.HallazgoDescartado}}
	r, _ = v.Revisar(ctx, "")
	content := m.asks[1].Messages[0].Content
	if strings.Contains(content, "[paso-decide:rendimiento-ai/interfaz-es]") || !strings.Contains(content, "El paso interfaz-es tarda 11 min (set aside)") {
		t.Fatalf("the second ask = %s", content)
	}
	if len(r.Hallazgos) != 1 || r.Hallazgos[0].Clave != "app-sana:caja" || r.Hallazgos[0].Accionable {
		t.Fatalf("a dismissed finding came back: %+v", r.Hallazgos)
	}

	// Over its monthly cap, the model is not asked.
	f.gastado = store.VigiaTope
	r, _ = v.Revisar(ctx, "")
	if r.Estado != store.RevisionFallida || !strings.Contains(r.Nota, "cap") || len(m.asks) != 2 {
		t.Fatalf("over the cap = %+v, %d asks", r, len(m.asks))
	}
	f.gastado = 0

	// A quiet week: no model at all.
	f.datos, f.decididos = store.VigiaDatos{}, nil
	r, _ = v.Revisar(ctx, "")
	if r.Estado != store.RevisionHecha || r.Senales != 0 || len(m.asks) != 2 || r.Nota == "" {
		t.Fatalf("a quiet week = %+v", r)
	}

	// No model answers: the review failed, and says why.
	f.datos, m.err = semanaFalsa(at), ia.ErrNoModel
	if r, _ = v.Revisar(ctx, ""); r.Estado != store.RevisionFallida || !strings.Contains(r.Nota, "AI page") {
		t.Fatalf("no model = %+v", r)
	}

	// One at a time.
	f.ocupado = true
	if _, err := v.Revisar(ctx, ""); !errors.Is(err, store.ErrVigiaOcupado) {
		t.Fatalf("two at once = %v", err)
	}
}

func TestTheWeeklyReviewIsDue(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	f := &vigiaFalso{ajuste: store.VigiaAjuste{Encendido: true}}
	v := &Vigia{Store: f, Now: func() time.Time { return at }}
	for _, c := range []struct {
		ultima time.Time
		estado string
		on     bool
		want   bool
	}{
		{time.Time{}, "", true, true},                       // never reviewed
		{at.Add(-6 * 24 * time.Hour), "hecha", true, false}, // this week's is done
		{at.Add(-7 * 24 * time.Hour), "hecha", true, true},  // a week went by
		{at.Add(-25 * time.Hour), "fallida", true, true},    // it failed yesterday: again
		{at.Add(-2 * time.Hour), "fallida", true, false},    // it failed just now
		{time.Time{}, "", false, false},                     // turned off
	} {
		f.ultima, f.estado, f.ajuste.Encendido = c.ultima, c.estado, c.on
		if got := v.toca(ctx); got != c.want {
			t.Errorf("last %v %s, on %v: due = %v", c.ultima, c.estado, c.on, got)
		}
	}
}
