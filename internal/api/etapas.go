package api

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// ---- the interface's use, by stages ----
//
// Each release of rendimiento itself (SELF_APP) whose changes touched the
// interface (web/) is a stage: its number, its day, its branch and the line
// of its change file (cambios/<branch>.md). The Uso page draws them over the
// days' views and compares the same number of days before and after each one,
// so a change to the interface can be told by what it did to the clicks.

// Etapa is one stage and its comparison.
type Etapa struct {
	Release int64     `json:"release"`
	At      time.Time `json:"at"`
	SHA     string    `json:"sha"`
	Rama    string    `json:"rama,omitempty"`
	Pull    int       `json:"pull,omitempty"`
	Texto   string    `json:"texto"`
	// Dias is how many days are compared on each side, and Pocos that the
	// days after are too few (under three) to conclude anything.
	Dias    int          `json:"dias"`
	Pocos   bool         `json:"pocos"`
	Antes   MedidasEtapa `json:"antes"`
	Despues MedidasEtapa `json:"despues"`
}

// MedidasEtapa is what a stage's side measures: views a day, pages seen for
// each run opened (fewer is a shorter way), the time to data, and the app
// section opened most with its share.
type MedidasEtapa struct {
	VistasDia         float64 `json:"vistasDia"`
	PaginasPorCorrida float64 `json:"paginasPorCorrida,omitempty"`
	MsDatos           float64 `json:"msDatos,omitempty"`
	Seccion           string  `json:"seccion,omitempty"`
	SeccionParte      float64 `json:"seccionParte,omitempty"`
}

// EtapasView is the Uso page's stages: the days' views and each stage.
type EtapasView struct {
	Dias   []store.DiaUso `json:"dias"`
	Etapas []Etapa        `json:"etapas"`
	// Sin says why there are none: "propia" (rendimiento does not build
	// itself here, no SELF_APP).
	Sin string `json:"sin,omitempty"`
}

// etapaInfo is what is learned of a release from its repository, kept by its
// commit: whether it touched the interface, its branch, pull request and line.
type etapaInfo struct {
	interfaz bool
	rama     string
	pull     int
	texto    string
}

var (
	etapasCache sync.Map // sha → etapaInfo
	mergeDe     = regexp.MustCompile(`^Merge pull request #(\d+) from [^/\s]+/(\S+)`)
	lineaCambio = regexp.MustCompile(`^-\s*(\*\*[^*]+\*\*\s*)?`)
)

// etapasDias is how many days each side of a stage compares, at most.
const etapasDias = 7

func (s *Server) verEtapas(w http.ResponseWriter, r *http.Request, _ string) {
	dias := 30
	if q := r.URL.Query().Get("dias"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || (n != 7 && n != 14 && n != 30 && n != 90) {
			httpError(w, http.StatusBadRequest, "the period is 7, 14, 30 or 90 days")
			return
		}
		dias = n
	}
	ctx := r.Context()
	now := time.Now().UTC()
	desde := dia(now).AddDate(0, 0, -(dias - 1))
	v := EtapasView{Etapas: []Etapa{}}
	var err error
	if v.Dias, err = s.Store.UsoPorDia(ctx, desde); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v.Dias == nil {
		v.Dias = []store.DiaUso{}
	}
	if s.Update == nil || s.Update.App == "" {
		v.Sin = "propia"
		writeJSON(w, v)
		return
	}
	app, err := s.Store.GetApp(ctx, s.Update.App)
	if err != nil {
		v.Sin = "propia"
		writeJSON(w, v)
		return
	}
	rels, err := s.Store.ListReleases(ctx, app.ID, 200)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sort.Slice(rels, func(i, j int) bool { return rels[i].Number < rels[j].Number })
	repos := s.Platform.Repos()
	for i, rel := range rels {
		if i == 0 || rel.CreatedAt.Before(desde) || len(v.Etapas) >= 20 {
			continue
		}
		info, ok := s.etapaDe(ctx, repos, app, rels[i-1].SHA, rel.SHA)
		if !ok || !info.interfaz {
			continue
		}
		e := Etapa{Release: rel.Number, At: rel.CreatedAt, SHA: rel.SHA, Rama: info.rama, Pull: info.pull, Texto: info.texto}
		if err := s.compararEtapa(ctx, &e, now); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		v.Etapas = append(v.Etapas, e)
	}
	writeJSON(w, v)
}

// etapaRepos is what a stage reads of the repository.
type etapaRepos interface {
	ChangedFiles(ctx context.Context, installation int64, repo, base, head string) ([]string, bool, error)
	CommitSubject(ctx context.Context, installation int64, repo, sha string) (string, error)
	FileAt(ctx context.Context, installation int64, repo, path, ref string) ([]byte, error)
}

// etapaDe learns a release from its repository, once per commit: false when
// the repository did not answer (it is asked again next time).
func (s *Server) etapaDe(ctx context.Context, repos etapaRepos, app *store.App, antes, sha string) (etapaInfo, bool) {
	if v, ok := etapasCache.Load(sha); ok {
		return v.(etapaInfo), true
	}
	files, _, err := repos.ChangedFiles(ctx, app.InstallationID, app.Repo, antes, sha)
	if err != nil {
		return etapaInfo{}, false
	}
	var info etapaInfo
	for _, f := range files {
		info.interfaz = info.interfaz || strings.HasPrefix(f, "web/")
	}
	if info.interfaz {
		subject, err := repos.CommitSubject(ctx, app.InstallationID, app.Repo, sha)
		if err != nil {
			return etapaInfo{}, false
		}
		info.texto = recortarEtapa(subject)
		if m := mergeDe.FindStringSubmatch(subject); m != nil {
			info.pull, _ = strconv.Atoi(m[1])
			info.rama, info.texto = m[2], m[2]
			if b, err := repos.FileAt(ctx, app.InstallationID, app.Repo, "cambios/"+m[2]+".md", sha); err == nil {
				if l := primeraLinea(b); l != "" {
					info.texto = l
				}
			}
		}
	}
	etapasCache.Store(sha, info)
	return info, true
}

// primeraLinea is a change file's first line, without its «- **What:**».
func primeraLinea(b []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); strings.HasPrefix(l, "-") {
			return recortarEtapa(strings.TrimSpace(lineaCambio.ReplaceAllString(l, "")))
		}
	}
	return ""
}

func recortarEtapa(s string) string {
	if r := []rune(s); len(r) > 220 {
		return string(r[:220]) + "…"
	}
	return s
}

func dia(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }

// compararEtapa measures the same number of days before a stage's day and
// after it (its own day left out: it holds both), up to etapasDias.
func (s *Server) compararEtapa(ctx context.Context, e *Etapa, now time.Time) error {
	d := dia(e.At.UTC())
	despues := int(dia(now).Sub(d).Hours() / 24) // whole days after its day, today included
	n := min(etapasDias, max(despues, 1))
	e.Dias, e.Pocos = n, despues < 3
	antes, err := s.Store.LeerUsoEntre(ctx, d.AddDate(0, 0, -n), d)
	if err != nil {
		return err
	}
	tras, err := s.Store.LeerUsoEntre(ctx, d.AddDate(0, 0, 1), d.AddDate(0, 0, 1+n))
	if err != nil {
		return err
	}
	e.Antes, e.Despues = medirEtapa(antes, n), medirEtapa(tras, n)
	return nil
}

// medirEtapa is one side's measures over n days.
func medirEtapa(u store.ResumenUso, n int) MedidasEtapa {
	var m MedidasEtapa
	var vistas, corridas, medidas, ms int64
	for _, p := range u.Paginas {
		vistas += p.Vistas
		medidas += p.Medidas
		ms += p.MsTotal
		if p.Ruta == "/apps/:name/runs/:id" {
			corridas += p.Vistas
		}
	}
	m.VistasDia = float64(vistas) / float64(max(n, 1))
	if corridas > 0 {
		m.PaginasPorCorrida = float64(vistas) / float64(corridas)
	}
	if medidas > 0 {
		m.MsDatos = float64(ms) / float64(medidas)
	}
	var abiertas int64
	var mejor store.AccionUsada
	for _, a := range u.Acciones {
		if !strings.HasPrefix(a.Accion, "open_") {
			continue
		}
		abiertas += a.Veces
		if a.Veces > mejor.Veces {
			mejor = a
		}
	}
	if abiertas > 0 {
		m.Seccion, m.SeccionParte = mejor.Accion, float64(mejor.Veces)/float64(abiertas)
	}
	return m
}
