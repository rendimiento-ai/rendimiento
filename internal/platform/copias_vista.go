package platform

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
)

// ---- the copies on the app's Caminos tab (Ruta 180) ----
//
// The tab shows an app's live copies (one per pull request, and the short
// ones its runs make en seco), what each runs and how it is, the memory
// they hold and the cluster's share of all apps' copies, those deleted
// lately and why, and the Secrets a copy lacks. A person who may merge can
// make a copy again with its branch's last commit that passed, keep it 3
// more days (up to previews' 30) or delete it now.

// Bounds of what a person does with a copy.
const (
	// CopyExtendDays is how much «keep it longer» adds.
	CopyExtendDays = 3
	// PreviewClusterCap is the share of the cluster's memory (percent) all
	// apps' copies may hold before it is too much (decision 136).
	PreviewClusterCap = 10
	// copyGoneDays is how far back the deleted copies are listed.
	copyGoneDays = 7
)

// Where a copy's memory was read: what its pods use (the metrics server),
// or what they ask for (their requests, or the copy's spec when its pods
// cannot be read).
const (
	MemUsage    = "usage"
	MemRequests = "requests"
	MemMixed    = "mixed"
)

// CopiasView is what the Caminos tab shows of an app's copies.
type CopiasView struct {
	// Previews is the app's previews: settings, resolved (nil: it asks for
	// none, and the rest is empty).
	Previews *CopiasSettings `json:"previews,omitempty"`
	// Where is "cluster" or "computer".
	Where string        `json:"where"`
	Live  []CopiaLive   `json:"live"`
	Gone  []store.Copia `json:"gone"`
	// MemoryMiB is what the live copies hold together, and MemoryFrom
	// where it was read (MemUsage, MemRequests or MemMixed).
	MemoryMiB  int64  `json:"memoryMiB"`
	MemoryFrom string `json:"memoryFrom,omitempty"`
	// Cluster is the cluster's share all apps' copies hold (nil on a
	// computer, or when the nodes cannot be read).
	Cluster *CopiasCluster `json:"cluster,omitempty"`
	// MissingSecrets are the Secrets the app's services read that a live
	// copy does not have (names only, never values).
	MissingSecrets []string `json:"missingSecrets,omitempty"`
}

// CopiasSettings is an app's previews: block, with its defaults.
type CopiasSettings struct {
	Keep    int    `json:"keep"`
	Max     int    `json:"max"`
	Data    string `json:"data"`
	MaxKeep int    `json:"maxKeep"`
	Extend  int    `json:"extend"`
}

// CopiasCluster is the memory all apps' copies hold against the cluster's.
type CopiasCluster struct {
	CopiesMiB  int64   `json:"copiesMiB"`
	ClusterMiB int64   `json:"clusterMiB"`
	Percent    float64 `json:"percent"`
	Cap        int     `json:"cap"`
	From       string  `json:"from,omitempty"`
}

// CopiaLive is one live copy.
type CopiaLive struct {
	Name  string `json:"name"`
	Pull  int    `json:"pull,omitempty"`
	Short bool   `json:"short,omitempty"` // a short copy en seco (Run is its run)
	// Title and Author are its pull request's, when the forja said.
	Title  string `json:"title,omitempty"`
	Author string `json:"author,omitempty"`
	Bot    bool   `json:"bot,omitempty"`
	URL    string `json:"url,omitempty"`
	Branch string `json:"branch,omitempty"`
	SHA    string `json:"sha,omitempty"`
	Run    int64  `json:"run,omitempty"` // the run whose images it runs
	// Health is the copy's phase (Healthy, Progressing, Degraded...), with
	// the controller's words.
	Health  string `json:"health"`
	Message string `json:"message,omitempty"`
	// Tests is its own tests' result against it (nil: none ran yet).
	Tests *CopiaTests `json:"tests,omitempty"`
	// Dry is what its run checked en seco against it (nil: nothing).
	Dry        *CopiaDry  `json:"dry,omitempty"`
	MemoryMiB  int64      `json:"memoryMiB"`
	MemoryFrom string     `json:"memoryFrom,omitempty"`
	MadeAt     time.Time  `json:"madeAt"`
	PushedAt   time.Time  `json:"pushedAt"`
	DeletesOn  *time.Time `json:"deletesOn,omitempty"`
	// Extended: a person moved its deletion; CanExtend: it may be kept
	// longer still (up to previews' 30 days after its last push).
	Extended       bool     `json:"extended,omitempty"`
	CanExtend      bool     `json:"canExtend,omitempty"`
	MissingSecrets []string `json:"missingSecrets,omitempty"`
}

// CopiaTests is a copy's own tests' result.
type CopiaTests struct {
	Passed int      `json:"passed"`
	Total  int      `json:"total"`
	Failed []string `json:"failed,omitempty"`
}

// CopiaDry is what a run checked en seco against the copy.
type CopiaDry struct {
	Running bool     `json:"running,omitempty"`
	Failed  bool     `json:"failed,omitempty"`
	Passed  int      `json:"passed"`
	Total   int      `json:"total"`
	Names   []string `json:"names,omitempty"` // the checks that did not pass
}

// CopiaNo is an action on a copy that cannot be done, and why.
type CopiaNo struct{ Why string }

func (e *CopiaNo) Error() string { return e.Why }

// CopiasOf is what the Caminos tab shows of app's copies.
func (p *Platform) CopiasOf(ctx context.Context, app *store.App) (*CopiasView, error) {
	out := &CopiasView{Where: "cluster", Live: []CopiaLive{}, Gone: []store.Copia{}}
	copias := p.copias()
	_, inCluster := copias.(kubeCopias)
	if !inCluster {
		out.Where = "computer"
	}
	pv := app.Spec.Previews
	if pv != nil {
		data := pv.Data
		if data == "" {
			data = spec.PreviewEmpty
		}
		out.Previews = &CopiasSettings{Keep: pv.KeepDays(), Max: pv.Copies(), Data: data, MaxKeep: spec.PreviewMaxKeep, Extend: CopyExtendDays}
	}
	now := time.Now()
	rows, gone, err := p.Store.Copias(ctx, app.ID, now.AddDate(0, 0, -copyGoneDays))
	if err != nil {
		return nil, err
	}
	for _, g := range gone {
		if g.GoneWhy != store.CopiaSeco { // a short copy goes after every such run
			out.Gone = append(out.Gone, g)
		}
	}
	if copias == nil {
		return out, nil
	}
	live, err := copias.List(ctx, app.Name)
	if err != nil {
		return nil, err
	}
	pulls := map[int]forja.Change{}
	if got, ok := p.recentPulls(ctx, app); ok {
		for _, pr := range got.prs {
			pulls[pr.Number] = pr
		}
	}
	rowOf := map[[2]int64]store.Copia{}
	for _, r := range rows {
		rowOf[[2]int64{int64(r.Pull), r.SecoRun}] = r
	}
	missing := map[string]bool{}
	froms := map[string]bool{}
	for _, c := range live {
		v := p.copiaLive(ctx, app, c, rowOf[[2]int64{int64(c.Number), c.Run}], pulls[c.Number], inCluster, now)
		out.MemoryMiB += v.MemoryMiB
		froms[v.MemoryFrom] = true
		for _, s := range v.MissingSecrets {
			missing[s] = true
		}
		out.Live = append(out.Live, v)
	}
	out.MemoryFrom = memFrom(froms)
	for s := range missing {
		out.MissingSecrets = append(out.MissingSecrets, s)
	}
	sort.Strings(out.MissingSecrets)
	// Pull requests' copies by number, then the short ones by run.
	sort.Slice(out.Live, func(i, j int) bool {
		a, b := out.Live[i], out.Live[j]
		if a.Short != b.Short {
			return !a.Short
		}
		if a.Pull != b.Pull {
			return a.Pull < b.Pull
		}
		return a.Run < b.Run
	})
	if inCluster {
		out.Cluster = p.copiesShare(ctx)
	}
	return out, nil
}

// memFrom is where a sum of memories was read.
func memFrom(froms map[string]bool) string {
	delete(froms, "")
	switch {
	case len(froms) == 1:
		for f := range froms {
			return f
		}
	case len(froms) > 1:
		return MemMixed
	}
	return ""
}

// specOfCopy is the spec a copy runs, from its app's (the default branch's:
// the branch's may differ in what the tab shows, not in its name or size).
func specOfCopy(app *store.App, c copia) spec.Spec {
	if c.Run != 0 {
		return spec.ShortCopyOf(app.Spec, c.Run)
	}
	return spec.PreviewOf(app.Spec, c.Number)
}

func (p *Platform) copiaLive(ctx context.Context, app *store.App, c copia, row store.Copia, pr forja.Change, inCluster bool, now time.Time) CopiaLive {
	sp := specOfCopy(app, c)
	v := CopiaLive{Name: c.name(), Pull: c.Number, Short: c.Run != 0, URL: p.copias().URL(sp, c), PushedAt: c.PushedAt,
		Run: c.Release, Branch: row.Branch, Health: "Waiting"}
	if v.Run == 0 {
		v.Run = row.RunID
	}
	if v.Short && v.Run == 0 {
		v.Run = c.Run
	}
	switch {
	case row.ID != 0:
		v.MadeAt = row.MadeAt
	case !c.MadeAt.IsZero():
		v.MadeAt = c.MadeAt
	default:
		v.MadeAt = c.PushedAt
	}
	if pr.Number != 0 {
		v.Title, v.Author, v.Bot = pr.Title, pr.User.Login, pr.User.Type == "Bot"
		if v.Branch == "" {
			v.Branch = pr.Head.Ref
		}
	}
	if run, err := p.Store.GetRun(ctx, v.Run); err == nil {
		v.SHA = run.SHA
		if v.Branch == "" {
			v.Branch = run.Branch
		}
		v.Dry = copiaDry(run, c)
	}
	if st, err := p.Target().Situacion(ctx, c.name()); err == nil && st.Status.Phase != "" {
		v.Health, v.Message = string(st.Status.Phase), st.Status.Message
	}
	if row.TestsTotal != nil && row.TestsPassed != nil {
		v.Tests = &CopiaTests{Passed: *row.TestsPassed, Total: *row.TestsTotal, Failed: row.TestsFailed}
	}
	if v.Short {
		on := c.PushedAt.Add(shortCopyLife)
		v.DeletesOn = &on
	} else if app.Spec.Previews != nil {
		keep := app.Spec.Previews.KeepDays()
		on := p.copyDeadline(ctx, app.ID, c.Number, c.PushedAt, keep)
		v.DeletesOn = &on
		v.Extended = on.After(c.PushedAt.AddDate(0, 0, keep))
		v.CanExtend = on.Before(c.PushedAt.AddDate(0, 0, spec.PreviewMaxKeep))
	}
	v.MemoryMiB, v.MemoryFrom = p.copyMemory(ctx, c.name(), sp, inCluster)
	v.MissingSecrets = p.copySecretsMissing(ctx, app, c, sp, inCluster)
	return v
}

// copiaDry is what run checked en seco against copy c (nil: nothing ran
// there).
func copiaDry(run *store.Run, c copia) *CopiaDry {
	dc := dryCopyOf(run)
	switch {
	case dc == nil:
		return nil
	case c.Run != 0 && dc.Kind == store.DryCopyShort:
	case c.Run == 0 && dc.Kind == store.DryCopyPull && dc.Pull == c.Number:
	default:
		return nil
	}
	d := &CopiaDry{Failed: dc.Failed, Total: len(dc.Tasks)}
	for _, t := range dc.Tasks {
		switch t.Status {
		case store.TaskSucceeded:
			d.Passed++
		case store.TaskRunning, "":
			d.Running = true
		case store.TaskSkipped:
		default:
			d.Names = append(d.Names, t.Name)
		}
	}
	if len(dc.Tasks) == 0 && !dc.Failed && dc.Note == "" {
		d.Running = true // the copy is still coming up for them
	}
	return d
}

var podMetricsGVK = schema.GroupVersionKind{Group: "metrics.k8s.io", Version: "v1beta1", Kind: "PodMetricsList"}

// copyMemory is the memory a copy holds (MiB) and where it was read: in
// a cluster what its pods use (the metrics server), else what they ask for;
// with no pods to read (a computer), what its spec asks for.
func (p *Platform) copyMemory(ctx context.Context, name string, sp spec.Spec, inCluster bool) (int64, string) {
	if inCluster && p.Kube != nil {
		if mib, from, ok := p.podsMemory(ctx, name); ok {
			return mib, from
		}
	}
	var bytes int64
	for _, svc := range sp.Services {
		if q, err := resource.ParseQuantity(spec.ResourcesFor(svc.Size, svc.Resources).MemRequest); err == nil {
			bytes += q.Value()
		}
	}
	return bytes >> 20, MemRequests
}

// podsMemory is what the pods in namespace ns use, or ask for when the
// metrics server does not say (ok false: no pods to read).
func (p *Platform) podsMemory(ctx context.Context, ns string) (int64, string, bool) {
	var pods corev1.PodList
	if err := p.Kube.List(ctx, &pods, client.InNamespace(ns)); err != nil || len(pods.Items) == 0 {
		return 0, "", false
	}
	pm := &unstructured.UnstructuredList{}
	pm.SetGroupVersionKind(podMetricsGVK)
	if err := p.Kube.List(ctx, pm, client.InNamespace(ns)); err == nil && len(pm.Items) > 0 {
		var bytes int64
		for _, it := range pm.Items {
			cs, _, _ := unstructured.NestedSlice(it.Object, "containers")
			for _, c := range cs {
				m, _ := c.(map[string]any)
				mem, _, _ := unstructured.NestedString(m, "usage", "memory")
				if q, err := resource.ParseQuantity(mem); err == nil {
					bytes += q.Value()
				}
			}
		}
		return bytes >> 20, MemUsage, true
	}
	var bytes int64
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, c := range pod.Spec.Containers {
			bytes += c.Resources.Requests.Memory().Value()
		}
	}
	return bytes >> 20, MemRequests, true
}

// copiesShare is the cluster's memory all apps' copies hold (nil when the
// nodes cannot be read).
func (p *Platform) copiesShare(ctx context.Context) *CopiasCluster {
	var nodes corev1.NodeList
	if err := p.Kube.List(ctx, &nodes); err != nil {
		return nil
	}
	var total int64
	for _, n := range nodes.Items {
		total += n.Status.Allocatable.Memory().Value()
	}
	if total <= 0 {
		return nil
	}
	all, err := p.copias().List(ctx, "")
	if err != nil {
		return nil
	}
	out := &CopiasCluster{ClusterMiB: total >> 20, Cap: PreviewClusterCap}
	froms := map[string]bool{}
	apps := map[string]*store.App{}
	for _, c := range all {
		a, ok := apps[c.App]
		if !ok {
			a, _ = p.Store.GetApp(ctx, c.App)
			apps[c.App] = a
		}
		var sp spec.Spec
		if a != nil {
			sp = specOfCopy(a, c)
		}
		mib, from := p.copyMemory(ctx, c.name(), sp, true)
		out.CopiesMiB += mib
		froms[from] = true
	}
	out.From = memFrom(froms)
	out.Percent = float64(out.CopiesMiB) * 100 / float64(out.ClusterMiB)
	return out
}

// copySecretsMissing are the Secrets the app's services read that copy c
// has none of. In a cluster a copy reads them in its own namespace, where
// nothing copies them (only its database's credentials are made); on a
// computer a copy reads its app's own.
func (p *Platform) copySecretsMissing(ctx context.Context, app *store.App, c copia, sp spec.Spec, inCluster bool) []string {
	where := app.Name
	if inCluster {
		where = c.name()
	}
	var out []string
	seen := map[string]bool{}
	for _, svc := range sp.Services {
		for _, name := range svc.SecretNames() {
			if seen[name] {
				continue
			}
			seen[name] = true
			keys, err := p.Target().LlavesSecreto(ctx, where, name)
			if err == nil && len(keys) == 0 {
				out = append(out, name)
			}
		}
	}
	return out
}

// liveCopy is app's pull request number's copy, for a person's action on
// it (store.ErrNotFound: none lives; a short copy is not acted on).
func (p *Platform) liveCopy(ctx context.Context, app *store.App, number int) (copia, error) {
	copias := p.copias()
	if copias == nil {
		return copia{}, store.ErrNotFound
	}
	// The list leaves out a copy being deleted (its namespace takes a while).
	all, err := copias.List(ctx, app.Name)
	if err != nil {
		return copia{}, err
	}
	for _, c := range all {
		if c.Number == number && c.Run == 0 {
			return c, nil
		}
	}
	return copia{}, store.ErrNotFound
}

// copyBranch is the branch pull request number's copy shows: its pull
// request's, or the one its run or its row knows.
func (p *Platform) copyBranch(ctx context.Context, app *store.App, c copia) string {
	if got, ok := p.recentPulls(ctx, app); ok {
		for _, pr := range got.prs {
			if pr.Number == c.Number && pr.Head.Ref != "" {
				return pr.Head.Ref
			}
		}
	}
	if row, err := p.Store.LiveCopia(ctx, app.ID, c.Number, 0); err == nil && row.Branch != "" {
		return row.Branch
	}
	if run, err := p.Store.GetRun(ctx, c.Release); err == nil {
		return run.Branch
	}
	return ""
}

// CopyRemade is what «make it again» did: the run whose commit the copy
// runs now, and whether it was the one it already ran (its services were
// restarted).
type CopyRemade struct {
	Run       int64 `json:"run"`
	Restarted bool  `json:"restarted,omitempty"`
}

// RemakeCopy writes pull request number's copy again from its branch's
// last run that passed (a person's «make it again with the last commit»):
// when that run is the one it runs, its services are restarted.
func (p *Platform) RemakeCopy(ctx context.Context, app *store.App, number int, who string) (*CopyRemade, error) {
	c, err := p.liveCopy(ctx, app, number)
	if err != nil {
		return nil, err
	}
	branch := p.copyBranch(ctx, app, c)
	if branch == "" {
		return nil, &CopiaNo{M("the branch of pull request #%d is not known: push to it to make its copy again", number)}
	}
	run, err := p.Store.LastPassedRunOf(ctx, app.ID, branch)
	if errors.Is(err, store.ErrNotFound) {
		return nil, &CopiaNo{M("no run of branch %s passed: there is no commit to make the copy with", branch)}
	}
	if err != nil {
		return nil, err
	}
	if run.ID == c.Release {
		if err := p.Target().Reiniciar(ctx, c.name()); err != nil {
			return nil, err
		}
		p.Log.Info("preview restarted by a person", "app", app.Name, "pull", number, "run", run.ID, "by", who)
		return &CopyRemade{Run: run.ID, Restarted: true}, nil
	}
	if !p.previewFromRun(ctx, app, number, run.ID) {
		return nil, &CopiaNo{M("the commit of run %d asks for no previews", run.ID)}
	}
	p.Log.Info("preview made again by a person", "app", app.Name, "pull", number, "run", run.ID, "by", who)
	return &CopyRemade{Run: run.ID}, nil
}

// ExtendCopy keeps pull request number's copy CopyExtendDays more (a
// person's «keep it 3 more days»), up to previews' most, 30 days after its
// last push: the new day it goes, said in its pull request's comment.
func (p *Platform) ExtendCopy(ctx context.Context, app *store.App, number int, who string) (time.Time, error) {
	c, err := p.liveCopy(ctx, app, number)
	if err != nil {
		return time.Time{}, err
	}
	if app.Spec.Previews == nil {
		return time.Time{}, &CopiaNo{M("the app no longer asks for previews")}
	}
	cur := p.copyDeadline(ctx, app.ID, number, c.PushedAt, app.Spec.Previews.KeepDays())
	limit := c.PushedAt.AddDate(0, 0, spec.PreviewMaxKeep)
	from := cur
	if now := time.Now(); now.After(from) {
		from = now
	}
	next := from.AddDate(0, 0, CopyExtendDays)
	if next.After(limit) {
		next = limit
	}
	if !next.After(cur) {
		return time.Time{}, &CopiaNo{M("the copy is already kept %d days after its last push, the most there is", spec.PreviewMaxKeep)}
	}
	if err := p.Store.KeepCopiaUntil(ctx, app.ID, number, c.Release, p.copyBranch(ctx, app, c), next); err != nil {
		return time.Time{}, err
	}
	p.Log.Info("preview kept longer by a person", "app", app.Name, "pull", number, "until", next, "by", who)
	if c.CommentID != 0 && p.Forja != nil {
		if run, err := p.Store.GetRun(ctx, c.Release); err == nil {
			body := previewComment(number, p.copias().URL(specOfCopy(app, c), c), run, p.Config.BaseURL, app.Name, next) +
				"\n\n" + M("**Kept longer:** %s asked rendimiento to keep this copy until %s.", who, next.Format(time.DateOnly))
			if row, err := p.Store.LiveCopia(ctx, app.ID, number, 0); err == nil {
				body += copyTestsSection(row)
			}
			if _, err := p.repos(app).Comment(ctx, app.InstallationID, app.Repo, number, c.CommentID, inForjaWords(app, body)); err != nil {
				p.Log.Warn("preview: its comment", "app", app.Name, "pull", number, "err", err)
			}
		}
	}
	return next, nil
}

// copyTestsSection is the comment's section of the copy's tests from what
// the store kept ("" when none ran).
func copyTestsSection(row store.Copia) string {
	if row.TestsTotal == nil || row.TestsPassed == nil {
		return ""
	}
	lines := []string{M("**Tests:** %d of %d passed against this copy.", *row.TestsPassed, *row.TestsTotal)}
	for _, n := range row.TestsFailed {
		lines = append(lines, M("- failed: %s", n))
	}
	return "\n\n" + strings.Join(lines, "\n")
}

// DeleteCopy deletes pull request number's copy now (a person's «delete
// it now»): its comment says who, and that the next push makes it again;
// until then the sweep does not.
func (p *Platform) DeleteCopy(ctx context.Context, app *store.App, number int, who string) error {
	c, err := p.liveCopy(ctx, app, number)
	if err != nil {
		return err
	}
	if err := p.deletePreview(ctx, c, store.CopiaPerson, M("by %s, from rendimiento; the next push to its branch makes it again", who), who); err != nil {
		return err
	}
	p.Log.Info("preview deleted by a person", "app", app.Name, "pull", number, "by", who)
	return nil
}
