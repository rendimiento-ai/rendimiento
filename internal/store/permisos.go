package store

// What each agent may do (Ruta 184): a permission per subject (one of the
// platform's agents, or a person's MCP key), in one of three modes, maybe
// only for some apps; what an agent asks a person for, and what a person
// gave it: once, while an encargo (or the key's cargo) lasts, or always for
// one app.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The permissions.
const (
	// PermCodeRead: the code, its branches and history, and the Ruta. Always
	// on: without it there is no agent.
	PermCodeRead = "code_read"
	// PermPlatformRead: runs, step logs, slow requests, the app's status.
	PermPlatformRead = "platform_read"
	// PermInfraRead: the app's pods, events, logs, rollout and verification
	// (its containers on a computer). Read-only, never Secrets.
	PermInfraRead = "infra_read"
	// PermSandbox: running commands in a sandbox (the coding agent's run).
	PermSandbox = "sandbox"
	// PermCopiesRead: the app's preview copies, their health and tests.
	PermCopiesRead = "copies_read"
	// PermEffects: restarting a service of its app, running a branch again,
	// remaking a preview copy. Never always: each use is a person's yes.
	PermEffects = "effects"
)

// Permisos are every permission, in the order the page shows them.
var Permisos = []string{PermCodeRead, PermPlatformRead, PermInfraRead, PermSandbox, PermCopiesRead, PermEffects}

// The modes of a permission.
const (
	ModoOff    = "off"    // the tool is not offered (or answers it is off)
	ModoAsk    = "ask"    // it asks a person, who gives it or not
	ModoAlways = "always" // it uses it without asking
)

// The subjects: the platform's own agents, by a stable id (a page about
// them links to IA → Permisos by it), and each MCP key as "mcp:<id>".
const (
	SujetoCoder        = "coder"
	SujetoDiagnostico  = "diagnostician"
	SujetoEstimador    = "estimator"
	SujetoVigia        = "vigia"
	SujetoClasificador = "clasificador"
	SujetoPlatica      = "platica"
	SujetoResumen      = "resumen"
	SujetoTraductor    = "traductor"
	SujetoAprendiz     = "aprendiz"
)

// SujetosPlataforma are the platform's agents, in the order the page lists them.
var SujetosPlataforma = []string{SujetoCoder, SujetoDiagnostico, SujetoEstimador, SujetoVigia, SujetoClasificador, SujetoPlatica, SujetoResumen, SujetoTraductor, SujetoAprendiz}

// SujetoLlave is an MCP key's subject.
func SujetoLlave(keyID int64) string { return "mcp:" + strconv.FormatInt(keyID, 10) }

// LlaveDeSujeto is the key of an MCP key's subject (0 when it is not one).
func LlaveDeSujeto(sujeto string) int64 {
	n, err := strconv.ParseInt(strings.TrimPrefix(sujeto, "mcp:"), 10, 64)
	if !strings.HasPrefix(sujeto, "mcp:") || err != nil || n <= 0 {
		return 0
	}
	return n
}

// SujetoValido says whether a subject is one of the platform's agents or a key's.
func SujetoValido(sujeto string) bool {
	return slices.Contains(SujetosPlataforma, sujeto) || LlaveDeSujeto(sujeto) > 0
}

// PermisoDefault is a permission's mode when nobody set it: the coding
// agent and MCP keys see the platform and their copies, ask for the
// infrastructure, run their sandbox and do nothing with effect; the
// diagnostician reads the platform and asks for the infrastructure (for
// it, always means the failed run's build pods only: see AlcanceDe); the vigía reads the platform; the
// others only what they use today (the Ruta and their own inputs).
func PermisoDefault(sujeto, permiso string) string {
	if permiso == PermCodeRead {
		return ModoAlways
	}
	if sujeto == SujetoCoder || LlaveDeSujeto(sujeto) > 0 {
		switch permiso {
		case PermPlatformRead, PermSandbox, PermCopiesRead:
			return ModoAlways
		case PermInfraRead:
			return ModoAsk
		}
		return ModoOff
	}
	switch {
	case sujeto == SujetoDiagnostico && permiso == PermPlatformRead:
		return ModoAlways
	case sujeto == SujetoDiagnostico && permiso == PermInfraRead:
		return ModoAlways // only its steps' pods (AlcanceDe); the app's own stays ask
	case sujeto == SujetoVigia && permiso == PermPlatformRead:
		return ModoAlways
	}
	return ModoOff
}

// AccionPasos is the target of the diagnostician's infra_read on a failed
// run's own step and build pods in the build namespace, their events and
// the nodes' pressure (never an app's data or logs, never Secrets).
const AccionPasos = "steps"

// AlcanceDe is how narrow a permission's «always» is for a subject: for
// the diagnostician's infra_read, "steps" (Ruta 185): always only for its
// failed run's step pods (AccionPasos), and ask for the app's namespace.
func AlcanceDe(sujeto, permiso string) string {
	if sujeto == SujetoDiagnostico && permiso == PermInfraRead {
		return AccionPasos
	}
	return ""
}

// ModoValido says whether a permission may be in a mode: code_read only
// always, effects never always.
func ModoValido(permiso, modo string) bool {
	if !slices.Contains(Permisos, permiso) {
		return false
	}
	switch {
	case permiso == PermCodeRead:
		return modo == ModoAlways
	case permiso == PermEffects:
		return modo == ModoOff || modo == ModoAsk
	}
	return modo == ModoOff || modo == ModoAsk || modo == ModoAlways
}

// Permiso is one permission of a subject: its mode, and the apps it is
// limited to (none: all).
type Permiso struct {
	Permiso string   `json:"permiso"`
	Modo    string   `json:"modo"`
	Apps    []string `json:"apps"`
	Set     bool     `json:"set"` // a person set it (else it is the default)
	// Alcance narrows «always» (AlcanceDe): "steps" for the diagnostician's
	// infra_read, which asks for anything but its run's step pods.
	Alcance   string     `json:"alcance,omitempty"`
	UpdatedBy string     `json:"updatedBy,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// PermisosOf are a subject's permissions, every one, with the defaults
// for those nobody set. The coding agent's sandbox without a row follows
// the run tool's old switch (ai_settings.code_run).
func (s *Store) PermisosOf(ctx context.Context, orgID int64, sujeto string) ([]Permiso, error) {
	orgID = orgOr(orgID)
	rows, err := s.pool.Query(ctx, `SELECT permiso, modo, apps, updated_by, updated_at FROM agente_permisos WHERE org_id = $1 AND sujeto = $2`, orgID, sujeto)
	if err != nil {
		return nil, err
	}
	set := map[string]Permiso{}
	for rows.Next() {
		var p Permiso
		var at time.Time
		if err := rows.Scan(&p.Permiso, &p.Modo, &p.Apps, &p.UpdatedBy, &at); err != nil {
			rows.Close()
			return nil, err
		}
		p.Set, p.UpdatedAt = true, &at
		set[p.Permiso] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Permiso, 0, len(Permisos))
	for _, name := range Permisos {
		p, ok := set[name]
		if !ok || !ModoValido(name, p.Modo) {
			p = Permiso{Permiso: name, Modo: PermisoDefault(sujeto, name)}
			if name == PermSandbox && sujeto == SujetoCoder {
				if a, err := s.AISettingsOf(ctx, orgID); err == nil && !a.CodeRun {
					p.Modo = ModoOff
				}
			}
		}
		if p.Apps == nil {
			p.Apps = []string{}
		}
		p.Alcance = AlcanceDe(sujeto, name)
		out = append(out, p)
	}
	return out, nil
}

// PermisoOf is one of a subject's permissions.
func (s *Store) PermisoOf(ctx context.Context, orgID int64, sujeto, permiso string) (Permiso, error) {
	all, err := s.PermisosOf(ctx, orgID, sujeto)
	if err != nil {
		return Permiso{}, err
	}
	for _, p := range all {
		if p.Permiso == permiso {
			return p, nil
		}
	}
	return Permiso{}, fmt.Errorf("no permission %q", permiso)
}

// ErrPermiso is a mode, subject or permission that cannot be.
var ErrPermiso = errors.New("not a permission an agent may have")

// SetPermiso sets one of a subject's permissions. The coding agent's
// sandbox also sets the old switch, for the previous version during an
// update.
func (s *Store) SetPermiso(ctx context.Context, orgID int64, sujeto string, p Permiso, by string) error {
	if !SujetoValido(sujeto) || !ModoValido(p.Permiso, p.Modo) {
		return ErrPermiso
	}
	apps := []string{}
	for _, a := range p.Apps {
		if a = strings.TrimSpace(a); a != "" && !slices.Contains(apps, a) {
			apps = append(apps, a)
		}
	}
	if p.Permiso == PermCodeRead {
		apps = []string{}
	}
	orgID = orgOr(orgID)
	if _, err := s.pool.Exec(ctx, `INSERT INTO agente_permisos (org_id, sujeto, permiso, modo, apps, updated_by, updated_at) VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (org_id, sujeto, permiso) DO UPDATE SET modo = $4, apps = $5, updated_by = $6, updated_at = now()`,
		orgID, sujeto, p.Permiso, p.Modo, apps, by); err != nil {
		return err
	}
	if sujeto == SujetoCoder && p.Permiso == PermSandbox {
		_, err := s.pool.Exec(ctx, `INSERT INTO ai_settings (org_id, code_run, updated_by, updated_at) VALUES ($1, $2, $3, now())
			ON CONFLICT (org_id) DO UPDATE SET code_run = $2, updated_by = $3, updated_at = now()`, orgID, p.Modo != ModoOff, by)
		return err
	}
	return nil
}

// The reaches of a grant.
const (
	AlcanceOnce    = "once"    // one use («solo esta vez»)
	AlcanceEncargo = "encargo" // while its encargo lasts
	AlcanceCargo   = "cargo"   // while the key's cargo is open (an MCP key's «mientras dure»)
	AlcanceAlways  = "always"  // always, for its app
)

// PermisoDado is a grant: a person's yes to a subject for one permission
// on one app, with how far it reaches.
type PermisoDado struct {
	ID        int64      `json:"id"`
	Sujeto    string     `json:"sujeto"`
	Permiso   string     `json:"permiso"`
	App       string     `json:"app"`
	Alcance   string     `json:"alcance"`
	Accion    string     `json:"accion,omitempty"`
	EncargoID *int64     `json:"encargoId,omitempty"`
	CargoID   *int64     `json:"cargoId,omitempty"`
	PedidoID  *int64     `json:"pedidoId,omitempty"`
	GrantedBy string     `json:"grantedBy"`
	GrantedAt time.Time  `json:"grantedAt"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
}

const dadoCols = `d.id, d.sujeto, d.permiso, d.app, d.alcance, d.accion, d.encargo_id, d.cargo_id, d.pedido_id, d.granted_by, d.granted_at, d.used_at`

func scanDado(r pgx.Row) (PermisoDado, error) {
	var d PermisoDado
	err := r.Scan(&d.ID, &d.Sujeto, &d.Permiso, &d.App, &d.Alcance, &d.Accion, &d.EncargoID, &d.CargoID, &d.PedidoID, &d.GrantedBy, &d.GrantedAt, &d.UsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// dadoVivo is the condition of a grant that still holds: not taken back,
// a single use not used yet, an encargo still open, a cargo not handed over.
const dadoVivo = `d.revoked_at IS NULL AND (
	(d.alcance = 'once' AND d.used_at IS NULL)
	OR d.alcance = 'always'
	OR (d.alcance = 'encargo' AND EXISTS (SELECT 1 FROM encargos e WHERE e.id = d.encargo_id AND e.status IN ('pending', 'taken', 'waiting', 'asking')))
	OR (d.alcance = 'cargo' AND EXISTS (SELECT 1 FROM cargos c WHERE c.id = d.cargo_id AND c.ended_at IS NULL)))`

// Decision is whether a subject may use a permission on an app now: the
// mode that counts, and the grant that lets it when it asks (nil: none).
type Decision struct {
	Modo  string       `json:"modo"`
	Dado  *PermisoDado `json:"dado,omitempty"`
	Apps  []string     `json:"apps,omitempty"`
	Fuera bool         `json:"fuera,omitempty"` // the app is not among the ones it is limited to
}

// Puede says whether the decision lets it use the permission now.
func (d Decision) Puede() bool {
	return d.Modo == ModoAlways || (d.Modo == ModoAsk && d.Dado != nil)
}

// PermisoDecide is what a subject may do with a permission on an app:
// off (also for an app outside its list), always, or ask, with the grant
// that holds for it (on the encargo it works on, or the key's open cargo;
// for effects, a single use for exactly that action).
func (s *Store) PermisoDecide(ctx context.Context, orgID int64, sujeto, permiso, app, accion string, encargoID int64) (Decision, error) {
	p, err := s.PermisoOf(ctx, orgID, sujeto, permiso)
	if err != nil {
		return Decision{}, err
	}
	d := Decision{Modo: p.Modo, Apps: p.Apps}
	if len(p.Apps) > 0 && !slices.ContainsFunc(p.Apps, func(a string) bool { return strings.EqualFold(a, app) }) {
		return Decision{Modo: ModoOff, Apps: p.Apps, Fuera: true}, nil
	}
	if d.Modo == ModoAlways && p.Alcance != "" && accion != p.Alcance {
		d.Modo = ModoAsk // narrow «always»: only its own target goes without asking
	}
	if d.Modo != ModoAsk {
		return d, nil
	}
	cond := `d.org_id = $1 AND d.sujeto = $2 AND d.permiso = $3 AND lower(d.app) = lower($4) AND ` + dadoVivo + ` AND (`
	if permiso == PermEffects {
		// Effects: only a single use, given for this very action.
		cond += `d.alcance = 'once' AND d.accion = $5 AND (d.encargo_id IS NOT DISTINCT FROM $6))`
	} else {
		cond += `d.alcance IN ('always', 'cargo') OR (d.alcance IN ('once', 'encargo') AND d.encargo_id IS NOT DISTINCT FROM $6)) AND $5::text = $5::text`
	}
	var enc *int64
	if encargoID > 0 {
		enc = &encargoID
	}
	g, err := scanDado(s.pool.QueryRow(ctx, `SELECT `+dadoCols+` FROM agente_permiso_dados d WHERE `+cond+`
		ORDER BY CASE d.alcance WHEN 'always' THEN 0 WHEN 'encargo' THEN 1 WHEN 'cargo' THEN 2 ELSE 3 END, d.id LIMIT 1`,
		orgOr(orgID), sujeto, permiso, app, accion, enc))
	switch {
	case err == nil:
		d.Dado = &g
	case !errors.Is(err, ErrNotFound):
		return d, err
	}
	return d, nil
}

// UsarPermisoDado spends a single-use grant: false when another use spent
// it first. Grants that reach further are not spent.
func (s *Store) UsarPermisoDado(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE agente_permiso_dados SET used_at = now() WHERE id = $1 AND alcance = 'once' AND used_at IS NULL AND revoked_at IS NULL`, id)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	var alcance string
	err = s.pool.QueryRow(ctx, `SELECT alcance FROM agente_permiso_dados WHERE id = $1 AND revoked_at IS NULL`, id).Scan(&alcance)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil && alcance != AlcanceOnce, err
}

// PermisosDados are the grants of an organization that still hold (of one
// subject, or all for ""), newest first.
func (s *Store) PermisosDados(ctx context.Context, orgID int64, sujeto string) ([]PermisoDado, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+dadoCols+` FROM agente_permiso_dados d WHERE d.org_id = $1 AND ($2 = '' OR d.sujeto = $2) AND `+dadoVivo+`
		ORDER BY d.id DESC LIMIT 200`, orgOr(orgID), sujeto)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PermisoDado, error) { return scanDado(r) })
	if out == nil {
		out = []PermisoDado{}
	}
	return out, err
}

// QuitarPermisoDado takes a grant back.
func (s *Store) QuitarPermisoDado(ctx context.Context, id, orgID int64, by string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE agente_permiso_dados SET revoked_at = now(), revoked_by = $3 WHERE id = $1 AND org_id = $2 AND revoked_at IS NULL`, id, orgOr(orgID), by)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PermisoPedido is what an agent asked a person for: a permission on its
// app, what it wants to see or do, why, and in which window of time; then
// the person's answer (once, encargo, cargo, always or no).
type PermisoPedido struct {
	ID         int64      `json:"id"`
	OrgID      int64      `json:"orgId,omitempty"`
	Sujeto     string     `json:"sujeto"`
	Permiso    string     `json:"permiso"`
	App        string     `json:"app"`
	Tool       string     `json:"tool,omitempty"`
	Accion     string     `json:"accion,omitempty"`
	Que        string     `json:"que"`
	PorQue     string     `json:"porQue"`
	Ventana    string     `json:"ventana,omitempty"`
	EncargoID  *int64     `json:"encargoId,omitempty"`
	CargoID    *int64     `json:"cargoId,omitempty"`
	AskedBy    string     `json:"askedBy"`
	AskedAt    time.Time  `json:"askedAt"`
	Answer     string     `json:"answer,omitempty"`
	AnsweredBy string     `json:"answeredBy,omitempty"`
	AnsweredAt *time.Time `json:"answeredAt,omitempty"`
	// For «Para usted»: the encargo's title and the key's name.
	EncargoTitle string `json:"encargoTitle,omitempty"`
	KeyName      string `json:"keyName,omitempty"`
}

const pedidoCols = `p.id, p.org_id, p.sujeto, p.permiso, p.app, p.tool, p.accion, p.que, p.por_que, p.ventana, p.encargo_id, p.cargo_id,
	p.asked_by, p.asked_at, p.answer, p.answered_by, p.answered_at,
	COALESCE((SELECT e.title FROM encargos e WHERE e.id = p.encargo_id), ''),
	COALESCE((SELECT k.name FROM agent_keys k WHERE p.sujeto = 'mcp:' || k.id::text), '')`

func scanPedido(r pgx.Row) (PermisoPedido, error) {
	var p PermisoPedido
	err := r.Scan(&p.ID, &p.OrgID, &p.Sujeto, &p.Permiso, &p.App, &p.Tool, &p.Accion, &p.Que, &p.PorQue, &p.Ventana, &p.EncargoID, &p.CargoID,
		&p.AskedBy, &p.AskedAt, &p.Answer, &p.AnsweredBy, &p.AnsweredAt, &p.EncargoTitle, &p.KeyName)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// PedirPermiso keeps what an agent asks a person for. An open request of
// the same subject for the same thing is kept once: a reason given later
// joins it.
func (s *Store) PedirPermiso(ctx context.Context, p PermisoPedido) (PermisoPedido, error) {
	if !SujetoValido(p.Sujeto) || !slices.Contains(Permisos, p.Permiso) || p.Permiso == PermCodeRead || strings.TrimSpace(p.App) == "" {
		return PermisoPedido{}, ErrPermiso
	}
	org := orgOr(p.OrgID)
	var id int64
	err := s.pool.QueryRow(ctx, `UPDATE agente_permiso_pedidos SET por_que = CASE WHEN $7 <> '' THEN $7 ELSE por_que END,
			que = CASE WHEN $8 <> '' THEN $8 ELSE que END, ventana = CASE WHEN $9 <> '' THEN $9 ELSE ventana END
		WHERE id = (SELECT id FROM agente_permiso_pedidos WHERE org_id = $1 AND sujeto = $2 AND permiso = $3 AND lower(app) = lower($4) AND accion = $5
			AND encargo_id IS NOT DISTINCT FROM $6 AND answered_at IS NULL ORDER BY id DESC LIMIT 1) RETURNING id`,
		org, p.Sujeto, p.Permiso, p.App, p.Accion, p.EncargoID, p.PorQue, p.Que, p.Ventana).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `INSERT INTO agente_permiso_pedidos (org_id, sujeto, permiso, app, tool, accion, que, por_que, ventana, encargo_id, cargo_id, asked_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
			org, p.Sujeto, p.Permiso, p.App, p.Tool, p.Accion, p.Que, p.PorQue, p.Ventana, p.EncargoID, p.CargoID, p.AskedBy).Scan(&id)
	}
	if err != nil {
		return PermisoPedido{}, err
	}
	return s.PermisoPedidoDe(ctx, id)
}

// PermisoPedidoDe is one request.
func (s *Store) PermisoPedidoDe(ctx context.Context, id int64) (PermisoPedido, error) {
	return scanPedido(s.pool.QueryRow(ctx, `SELECT `+pedidoCols+` FROM agente_permiso_pedidos p WHERE p.id = $1`, id))
}

// PermisoPedidoWait is how long an MCP key's request waits for an answer.
const PermisoPedidoWait = 7 * 24 * time.Hour

// PermisosPedidos are an organization's requests nobody answered yet, oldest
// first: the coding agent's while its encargo still asks with them, an MCP
// key's while the key works and PermisoPedidoWait has not gone by.
func (s *Store) PermisosPedidos(ctx context.Context, orgID int64) ([]PermisoPedido, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+pedidoCols+` FROM agente_permiso_pedidos p
		WHERE p.org_id = $1 AND p.answered_at IS NULL AND (
			(p.encargo_id IS NOT NULL AND EXISTS (SELECT 1 FROM encargo_preguntas q JOIN encargos e ON e.id = q.encargo_id
				WHERE q.pedido_id = p.id AND e.status = 'asking' AND q.answered_at IS NULL AND q.passed_at IS NULL))
			OR (p.encargo_id IS NULL AND p.asked_at > now() - $2::interval AND EXISTS (SELECT 1 FROM agent_keys k
				WHERE p.sujeto = 'mcp:' || k.id::text AND k.revoked_at IS NULL AND k.expires_at > now())))
		ORDER BY p.id LIMIT 100`, orgOr(orgID), fmt.Sprintf("%d seconds", int(PermisoPedidoWait.Seconds())))
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PermisoPedido, error) { return scanPedido(r) })
	if out == nil {
		out = []PermisoPedido{}
	}
	return out, err
}

// ErrPedidoRespondido is a request somebody answered already.
var ErrPedidoRespondido = errors.New("this request was answered already")

// The answers to a request: the reaches of a grant, or no.
const PedidoNo = "no"

// ResponderPermiso is a person's answer to a request of their organization:
// no, or a grant that reaches once, while the encargo lasts (an MCP key's
// cargo, for a key), or always for the app. Effects take only once.
func (s *Store) ResponderPermiso(ctx context.Context, id, orgID int64, answer, by string) (PermisoPedido, *PermisoDado, error) {
	p, err := s.PermisoPedidoDe(ctx, id)
	if err != nil || p.OrgID != orgOr(orgID) {
		return PermisoPedido{}, nil, ErrNotFound
	}
	if p.AnsweredAt != nil {
		return p, nil, ErrPedidoRespondido
	}
	alcance := answer
	switch answer {
	case PedidoNo, AlcanceOnce, AlcanceAlways:
	case AlcanceEncargo:
		if p.EncargoID == nil {
			alcance = AlcanceCargo
		}
	default:
		return p, nil, ErrPermiso
	}
	if p.Permiso == PermEffects && answer != PedidoNo && answer != AlcanceOnce {
		return p, nil, ErrPermiso
	}
	if alcance == AlcanceCargo && p.CargoID == nil {
		return p, nil, ErrPermiso
	}
	var dado *PermisoDado
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE agente_permiso_pedidos SET answer = $2, answered_by = $3, answered_at = now() WHERE id = $1 AND answered_at IS NULL`, id, answer, by)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrPedidoRespondido
		}
		if answer == PedidoNo {
			return nil
		}
		var enc, cargo *int64
		switch alcance {
		case AlcanceOnce, AlcanceEncargo:
			enc = p.EncargoID
		case AlcanceCargo:
			cargo = p.CargoID
		}
		accion := ""
		if p.Permiso == PermEffects {
			accion = p.Accion
		}
		d, err := scanDado(tx.QueryRow(ctx, `INSERT INTO agente_permiso_dados AS d (org_id, sujeto, permiso, app, alcance, accion, encargo_id, cargo_id, pedido_id, granted_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING `+dadoCols,
			p.OrgID, p.Sujeto, p.Permiso, p.App, alcance, accion, enc, cargo, p.ID, by))
		if err != nil {
			return err
		}
		dado = &d
		return nil
	})
	if err != nil {
		return p, nil, err
	}
	p, err = s.PermisoPedidoDe(ctx, id)
	return p, dado, err
}

// The kinds of an encargo's history about permissions (Ruta 184): a use,
// a request, a grant and a no. The previous version shows them as they are.
const (
	LogPermisoUso    = "permiso_uso"
	LogPermisoPedido = "permiso_pedido"
	LogPermisoDado   = "permiso_dado"
	LogPermisoNegado = "permiso_negado"
)

// NoteEncargoPermiso adds a use, request, grant or no of a permission to
// an encargo's history: note says what (a summary, never values); cause
// keeps the permission.
func (s *Store) NoteEncargoPermiso(ctx context.Context, id int64, who, what, permiso, note string) error {
	switch what {
	case LogPermisoUso, LogPermisoPedido, LogPermisoDado, LogPermisoNegado:
	default:
		return ErrPermiso
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO encargo_log (encargo_id, who, what, note, cause) VALUES ($1, $2, $3, $4, NULLIF($5, ''))`, id, who, what, note, permiso)
	return err
}
