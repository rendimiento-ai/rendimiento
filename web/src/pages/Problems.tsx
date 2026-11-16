// The Problems page: rendimiento's own warnings and errors from the last 30
// days, repeats grouped: the list on the left (open ones first), the one
// chosen on the right, with what happened and what to do. Problems about an
// app also show as a banner on its page (AppProblems).
import { Link, NavLink, useNavigate, useSearchParams } from "react-router-dom";
import { useState } from "react";
import { api, timeAgo, type Problem } from "../api";
import { ErrorBox, usePoll } from "../components/ui";
import { locale, t, tn } from "../i18n";

/** A problem still counts while it is not dismissed or fixed and happened recently. */
function isOpen(p: Problem, openForHours: number) {
  return !p.dismissedAt && !p.resolvedAt && Date.now() - new Date(p.lastAt).getTime() < openForHours * 3600_000;
}

/** "rendimiento.yaml rejected; the push was not built: services[0]: …" */
function headline(p: Problem) {
  return p.detail ? `${t(p.message)}: ${p.detail}` : t(p.message);
}

/** The server's note on what fixed a problem, e.g. "fixed by c6eae54". */
function resolutionText(r: string) {
  const m = /^fixed by (\w+)$/.exec(r);
  return m ? t("fixed by {sha}", { sha: m[1] }) : t(r);
}

/** The last 30 days (UTC, oldest first), as the server keys them. */
function last30(): string[] {
  const today = Date.UTC(new Date().getUTCFullYear(), new Date().getUTCMonth(), new Date().getUTCDate());
  return Array.from({ length: 30 }, (_, i) => new Date(today - (29 - i) * 86400_000).toISOString().slice(0, 10));
}

/** A strip of the last 30 days, one cell a day, lit on the days the problem happened. */
function DayStrip({ p, color }: { p: Problem; color: string }) {
  const days = last30();
  // Before the platform counted days, only the last occurrence is known.
  const counts = p.days ?? { [p.lastAt.slice(0, 10)]: 1 };
  const n = days.filter((d) => counts[d]).length;
  return (
    <div>
      <div className="dias" role="img" aria-label={tn(n, "Happened on {n} of the last 30 days", "Happened on {n} of the last 30 days")}>
        {days.map((d, i) => (
          <span key={d} className={counts[d] ? "si" : undefined} title={counts[d] ? `${d}: ${tn(counts[d], "{n} time", "{n} times")}` : d}
            style={{ ["--c" as string]: color, ["--d" as string]: `${(i % 7) * 120}ms` }} />
        ))}
      </div>
      <div className="dias-pie small muted"><span>{t("30 days ago")}</span><span>{tn(p.count, "{n} time", "{n} times")}</span><span>{t("today")}</span></div>
    </div>
  );
}

/**
 * What a problem means and what to do about it, for the messages whose
 * meaning is clear from the code that logs them, keyed by the exact message.
 * `es` is the server's own Spanish for a message it translates (the API
 * answers a Spanish browser with it, internal/i18n/es.go), so the entry is
 * found either way. A message with no entry shows no "What to do".
 */
type Hint = { es?: string; what?: () => string; todo: () => string };

const hints: Record<string, Hint> = {
  "recovered runs interrupted by the last restart": {
    what: () => t("rendimiento restarted while runs were going. When it came back it put them in the queue again, to run from the start; a run interrupted twice is marked failed instead. The technical detail says how many of each."),
    todo: () => t("Nothing: rendimiento resumed them by itself. If any were marked failed, run them again from their page."),
  },
  "rendimiento.yaml rejected; the push was not built": {
    es: "rendimiento.yaml rechazado; el envío no se construyó",
    what: () => t("A push had a rendimiento.yaml that could not be used, so nothing was built or released; the app keeps running its current release."),
    todo: () => t("Fix the file as the detail says and push again. Once a push on the same branch is accepted, the problem is marked resolved."),
  },
  "notification email failed": {
    es: "falló el correo de notificación",
    what: () => t("An alert email could not be sent."),
    todo: () => t("Check the mail server rendimiento sends through, as the detail says. Once an email is sent, the problem is marked resolved."),
  },
  "notification dropped: hourly email limit reached": {
    what: () => t("More alert emails were due in one hour than the limit allows, so this one was not sent."),
    todo: () => t("Usually nothing: the limit keeps a burst of alerts from filling your inbox. If it happens often, look at which alerts keep repeating."),
  },
  "CLOUDFLARE_API_TOKEN not set: DNS records must be created by hand": {
    todo: () => t("Set CLOUDFLARE_API_TOKEN for the platform so it creates DNS records by itself, or keep creating each app's records by hand."),
  },
  "image registry nearly full after its nightly cleanup": {
    es: "el registro de imágenes sigue casi lleno después de su limpieza nocturna",
    what: () => t("Even after deleting old images, 80% or more of the image registry's disk is in use."),
    todo: () => t("Give the registry a bigger disk, or keep fewer images. Once it is full, new images cannot be stored."),
  },
  "release failed verification; rolled back": {
    what: () => t("A new release did not pass its checks after it was deployed, so rendimiento returned the app to its last good release."),
    todo: () => t("See why on the release, in the app's page; meanwhile the app runs the earlier release. Fix it and push again."),
  },
  "release failed verification; kept": {
    what: () => t("A new release did not pass its checks after it was deployed, and it was kept: automatic rollback is off, there was no earlier good release, or the rollback failed. The technical detail says which."),
    todo: () => t("The release is still running. See why it failed on the app's page, roll back from there if you need to, or fix it and push again."),
  },
  "automatic rollback failed": {
    what: () => t("A release failed its checks and rendimiento could not return the app to its last good release."),
    todo: () => t("The failing release is still running: roll back by hand from the app's page, and see in the detail why the automatic rollback failed."),
  },
  "post-deploy tasks not run: the release did not become healthy": {
    what: () => t("A release's post-deploy tasks wait for it to be healthy; it never was, so they did not run."),
    todo: () => t("See on the app's page why the release is not healthy, fix it and push again."),
  },
  "change detection failed; building everything": {
    what: () => t("rendimiento could not ask GitHub which files changed since the last release, so it built every part of the app instead of only the changed ones."),
    todo: () => t("Nothing: the build only took longer."),
  },
  "login refused": {
    what: () => t("Someone signed in with a GitHub account that is not a member here. Their login is in the technical detail."),
    todo: () => t("If you know them, add them on the Members page; if not, there is nothing to do."),
  },
  "señal de vida not sent; trying again in an hour": {
    todo: () => t("Nothing: rendimiento tries again in an hour."),
  },
  "a saved setting does not check out; using the environment's": {
    what: () => t("A setting saved in rendimiento is no longer valid (a namespace deleted since, for example), so the platform uses the values it was started with."),
    todo: () => t("Fix or clear the setting the detail names, on the Environment page."),
  },
};

/**
 * Work rendimiento tries again by itself (internal/problems.Retried): such a
 * problem is resolved once it has not happened for 30 minutes.
 */
const retried = [
  "sync failed", "Reconciler error", "list network policies",
  "Error retrieving lease lock", "error retrieving resource lock",
  "merge: list pull requests", "branch kinds: pull requests",
  "addons: sync failed", "claim run", "coder: looking for encargos",
  "live events: listening stopped; retrying", "live events: pruning stored events",
  "could not read the platform's settings; keeping the last ones",
];
const retriedHint: Hint = {
  what: () => t("This is work rendimiento tries again by itself."),
  todo: () => t("Usually nothing: once it has not failed for 30 minutes, it is marked resolved. If it keeps coming back, ask an agent to look into it."),
};

/** The hint for a problem's message, in English or as the server translated it. */
function hintFor(message: string): Hint | undefined {
  const h = hints[message] ?? Object.values(hints).find((x) => x.es === message);
  if (h) return h;
  if (retried.includes(message) || message === "complementos: falló la sincronización") return retriedHint;
  return undefined;
}

/** A detail worth reading as a sentence: short, one line, and not code, a URL or a quote of something. */
function readable(d: string) {
  return d.length > 0 && d.length <= 200 && !/[\n{}<>[\]=`"]|:\/\//.test(d);
}

const toneOf = (p: Problem) => (p.resolvedAt ? "ok" : p.level === "ERROR" ? "bad" : "warn");
const colorOf = (tone: string) => (tone === "ok" ? "var(--ok)" : tone === "bad" ? "var(--bad)" : "var(--warn)");

/** Where a problem happened: its app, or the platform itself. */
function Where({ p, link }: { p: Problem; link?: boolean }) {
  if (!p.app) return <>{t("The platform")}</>;
  return link ? <Link to={`/apps/${p.app}`}>{p.app}</Link> : <>{p.app}</>;
}

/**
 * Hands a problem to agents as an encargo, with what was logged as its
 * detail, then goes to the encargos (as a failed run's «Ask an agent» does).
 */
function AskAgent({ p, appKnown }: { p: Problem; appKnown: boolean }) {
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const body = [
    t("rendimiento recorded this problem on its Problems page ({level}, {n} times since {first}, last {last}):", { level: p.level, n: p.count, first: p.firstAt, last: p.lastAt }),
    p.message,
    p.detail ? `${t("Detail")}: ${p.detail}` : "",
    p.component ? `${t("Component")}: ${p.component}` : "",
    p.attrs ? `${t("Attributes")}: ${p.attrs}` : "",
    t("Find out from the code that logs it why it happens; if it needs a fix, make it on a branch, test it there and hand it over with the link."),
  ].filter(Boolean).join("\n");
  const title = t("Look into a problem: {message}", { message: t(p.message) });
  return (
    <button className="primary" disabled={busy} title={t("Creates an encargo for an agent to take")} onClick={() => {
      setBusy(true);
      api.createEncargo({ title: title.length > 200 ? title.slice(0, 199) + "…" : title, body, app: appKnown ? p.app : undefined })
        .then((e) => navigate(`/ruta?tab=encargos#encargo-${e.id}`), (e) => { setBusy(false); alert(e.message); });
    }}>{t("Ask an agent")}</button>
  );
}

/** One problem, in full: what happened, what to do, its 30 days, the buttons and the technical detail. */
function ProblemDetail({ p, open, appKnown, onDismiss }: { p: Problem; open: boolean; appKnown: boolean; onDismiss: () => void }) {
  const [busy, setBusy] = useState(false);
  const tone = toneOf(p);
  const hint = hintFor(p.message);
  return (
    <main className="ld-detalle problema-detalle" id="problema-elegido">
      <div>
        <div className="row" style={{ alignItems: "baseline", flexWrap: "wrap", gap: 8 }}>
          <span className={`badge ${tone}`}>{p.resolvedAt ? t("Resolved") : p.level === "ERROR" ? t("Error") : t("Warning")}</span>
          <span className="small muted">
            <Where p={p} link />
            {p.resolvedAt ? <> · <span className="problema-resuelto">{t("Resolved {when}", { when: timeAgo(p.resolvedAt) })}{p.resolution ? `: ${resolutionText(p.resolution)}` : ""}</span></>
              : p.dismissedAt ? <> · {t("dismissed {when}", { when: timeAgo(p.dismissedAt) })}</> : null}
          </span>
        </div>
        <h2 className="ld-titulo problema-titulo">{t(p.message)}</h2>
      </div>
      <section className="ld-sec">
        <h3>{t("What happened")}</h3>
        {hint?.what && <p>{hint.what()}</p>}
        {readable(p.detail) && <p>{p.detail}</p>}
        <p className="muted">{p.count > 1
          ? t("It happened {n} times: the first {first}, the last {last}.", { n: p.count, first: timeAgo(p.firstAt), last: timeAgo(p.lastAt) })
          : t("It happened {when}.", { when: timeAgo(p.lastAt) })}</p>
      </section>
      {hint && (
        <section className="ld-sec">
          <h3>{t("What to do")}</h3>
          <p>{hint.todo()}</p>
        </section>
      )}
      <DayStrip p={p} color={colorOf(tone)} />
      {open && (
        <div className="row" style={{ flexWrap: "wrap", gap: 8 }}>
          <AskAgent p={p} appKnown={appKnown} />
          <button disabled={busy} title={t("Hide it until it happens again")}
            onClick={() => { setBusy(true); api.dismissProblem(p.id).then(onDismiss, (e) => { setBusy(false); alert(e.message); }); }}>
            {t("Dismiss")}
          </button>
        </div>
      )}
      <details className="problema-tecnico">
        <summary>{t("Technical detail")}</summary>
        {p.detail && <pre className="problema-crudo mono">{p.detail}</pre>}
        <dl className="small">
          <dt>{t("Message")}</dt><dd className="mono">{p.message}</dd>
          {p.component && <><dt>{t("Component")}</dt><dd className="mono">{p.component}</dd></>}
          {p.attrs && <><dt>{t("Attributes")}</dt><dd className="mono">{p.attrs}</dd></>}
          <dt>{t("First")}</dt><dd>{new Date(p.firstAt).toLocaleString(locale)}</dd>
          <dt>{t("Last")}</dt><dd>{new Date(p.lastAt).toLocaleString(locale)}</dd>
        </dl>
      </details>
    </main>
  );
}

/** A row of the list: a dot of its color, the message, and where and how often. */
function ProblemRow({ p, on, pick }: { p: Problem; on: boolean; pick: () => void }) {
  const c = colorOf(toneOf(p));
  return (
    <button className={`ld-item problema-fila${on ? " on" : ""}`} style={{ ["--c" as string]: c }} aria-current={on} onClick={pick}>
      <span className="ld-punto" style={{ background: c, margin: "6px 0 0" }} aria-hidden="true" />
      <span className="problema-fila-texto">
        {t(p.message)}
        <span className="ld-sub"><Where p={p} /> · {tn(p.count, "{n} time", "{n} times")}</span>
      </span>
    </button>
  );
}

type Ver = "abiertos" | "errores" | "avisos" | "resueltos";

/**
 * The Problems page: counters that filter; on the left the problems in groups
 * (the open ones, the resolved, the rest), on the right the one chosen (?p=).
 */
export function Problems() {
  const [params, setParams] = useSearchParams();
  const app = params.get("app") ?? "";
  const ver = (params.get("ver") ?? "") as Ver | "";
  const [dismissed, setDismissed] = useState(false);
  const { data, error, reload } = usePoll(() => api.problems({ app, dismissed }), [app, dismissed], 15000);
  const { data: apps } = usePoll(api.apps, [], 0);
  const all = data?.problems ?? [];
  const isOpenP = (p: Problem) => !!data && isOpen(p, data.openForHours);
  const filtros: { id: Ver; label: string; color: string; test: (p: Problem) => boolean }[] = [
    { id: "abiertos", label: "Open", color: "var(--bad)", test: isOpenP },
    { id: "errores", label: "Errors", color: "var(--bad)", test: (p) => p.level === "ERROR" },
    { id: "avisos", label: "Warnings", color: "var(--warn)", test: (p) => p.level === "WARN" },
    { id: "resueltos", label: "Resolved", color: "var(--ok)", test: (p) => !!p.resolvedAt },
  ];
  const filtro = filtros.find((f) => f.id === ver);
  const shown = filtro ? all.filter(filtro.test) : all;
  const open = shown.filter(isOpenP);
  const resolved = shown.filter((p) => !isOpenP(p) && p.resolvedAt);
  const earlier = shown.filter((p) => !isOpenP(p) && !p.resolvedAt);
  const groups: { label: string; items: Problem[] }[] = [
    { label: t("Need your attention"), items: open },
    { label: t("Already resolved"), items: resolved },
    { label: t("Earlier"), items: earlier },
  ];
  const poner = (k: string, v: string) => setParams((p) => { if (v) p.set(k, v); else p.delete(k); return p; }, { replace: true });
  // The one chosen stays in the address, so a reload or the next poll keeps it;
  // when it is gone (dismissed, filtered out) the first open one is shown.
  const chosenID = Number(params.get("p"));
  const chosen = shown.find((p) => p.id === chosenID) ?? open[0] ?? shown[0];
  // On a phone the one chosen is below the list: bring it into view.
  const elegir = (id: number) => {
    poner("p", String(id));
    if (window.matchMedia("(max-width: 760px)").matches) requestAnimationFrame(() => document.getElementById("problema-elegido")?.scrollIntoView({ block: "start" }));
  };

  return (
    <>
      <h1>{t("Problems")}</h1>
      <p className="sub">
        {t("Warnings and errors from rendimiento itself in the last 30 days: configs it rejected, emails it could not send, DNS or GitHub calls that failed. Repeats are grouped. A problem stays open for {n} hours after it last happened, until rendimiento sees it fixed (a later push accepted, a later email sent, or, for work it retries by itself, 30 minutes without failing again), or until dismissed; it reopens if it happens again. Outages of your apps are on each app's Reliability tab.", { n: data?.openForHours ?? 24 })}
      </p>
      {data && (
        <div className="problema-cuentas" role="group" aria-label={t("Filter problems")}>
          {filtros.map((f) => (
            <button key={f.id} className="problema-cuenta" aria-pressed={ver === f.id} onClick={() => poner("ver", ver === f.id ? "" : f.id)}>
              <span>{t(f.label)}</span>
              <b style={{ color: f.color }}>{all.filter(f.test).length}</b>
            </button>
          ))}
        </div>
      )}
      <div className="rel-filters">
        <label className="small">{t("App")}{" "}
          <select value={app} onChange={(e) => poner("app", e.target.value)}>
            <option value="">{t("All apps and the platform")}</option>
            {apps?.map((a) => <option key={a.id} value={a.name}>{a.name}</option>)}
          </select>
        </label>
        <label className="small row" style={{ gap: 6 }}>
          <input type="checkbox" checked={dismissed} onChange={(e) => setDismissed(e.target.checked)} /> {t("Show dismissed")}
        </label>
      </div>
      <ErrorBox error={error} />
      {data && all.length === 0 && (
        <div className="card empty"><p>{app ? t("No problems in the last 30 days for {app}.", { app }) : t("No problems in the last 30 days.")}</p></div>
      )}
      {data && all.length > 0 && shown.length === 0 && <div className="card empty"><p>{t("Nothing here.")}</p></div>}
      {chosen && (
        <div className="lista-detalle problemas">
          <aside className="ld-lista" aria-label={t("Problems")}>
            {groups.map((g) => g.items.length > 0 && (
              <div key={g.label}>
                <div className="ld-grupo">{g.label} <span style={{ fontWeight: 400 }}>{g.items.length}</span></div>
                {g.items.map((p) => <ProblemRow key={p.id} p={p} on={p.id === chosen.id} pick={() => elegir(p.id)} />)}
              </div>
            ))}
          </aside>
          <ProblemDetail key={chosen.id} p={chosen} open={isOpenP(chosen)} appKnown={!!apps?.some((a) => a.name === chosen.app)} onDismiss={reload} />
        </div>
      )}
    </>
  );
}

/** Open problems about one app, as a banner on its page. */
export function AppProblems({ name }: { name: string }) {
  const { data } = usePoll(() => api.problems({ app: name }), [name], 30000);
  const open = data?.problems.filter((p) => isOpen(p, data.openForHours)) ?? [];
  if (open.length === 0) return null;
  return (
    <div className="alert" style={{ marginTop: 12 }} role="status">
      <strong>{tn(open.length, "rendimiento had a problem with this app", "rendimiento had {n} problems with this app")}</strong>
      <ul className="problem-list">
        {open.slice(0, 3).map((p) => (
          <li key={p.id}>{headline(p)}{p.count > 1 ? ` (${t("{n} times", { n: p.count })})` : ""} · {timeAgo(p.lastAt)}</li>
        ))}
      </ul>
      <Link to={`/problems?app=${encodeURIComponent(name)}`}>{open.length > 3 ? t("See all {n} →", { n: open.length }) : t("See details →")}</Link>
    </div>
  );
}

/** The navigation link, with the number of open problems. */
export function ProblemsNavLink() {
  const { data } = usePoll(api.problemCount, [], 30000);
  const n = data?.open ?? 0;
  return (
    <NavLink to="/problems" className="nav-link" aria-label={n ? t("Problems, {n} open", { n }) : t("Problems")}>
      {t("Problems")}{n > 0 && <span className="nav-count">{n}</span>}
    </NavLink>
  );
}
