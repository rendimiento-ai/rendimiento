// The Libreta tab of an app (Ruta 150): what was asked for the app and whether it is done. It only shows: every line
// is a Ruta entry about the app, an encargo or a pull request of Renovate's, and its state is computed by rendimiento
// from its branch's run, its merge and its release. «Note a request» writes a to-do in the Ruta with the app set.
import { useState } from "react";
import { Link } from "react-router-dom";
import { api, session, type App, type LibretaItem, type LibretaStatus } from "../api";
import { locale, t, tn } from "../i18n";
import { ErrorBox, usePoll } from "./ui";
import { Rama } from "./lo-ultimo";

const WEEK = 7 * 24 * 3600 * 1000;

/** Each state's words and colour; the order is the one a request goes through. */
const ESTADO: Record<LibretaStatus, { label: string; cls: string }> = {
  pedido: { label: "Asked", cls: "pedido" },
  en_curso: { label: "In progress", cls: "en-curso" },
  listo: { label: "Ready to merge", cls: "listo" },
  integrado: { label: "Integrated", cls: "integrado" },
  publicado: { label: "Shipped", cls: "publicado" },
  esperando: { label: "Waiting", cls: "esperando" },
  descartado: { label: "Discarded", cls: "descartado" },
  hecho: { label: "Finished", cls: "integrado" },
};

const abierto = (s: LibretaStatus) => s === "pedido" || s === "en_curso" || s === "listo" || s === "esperando";
const hecho = (s: LibretaStatus) => s === "integrado" || s === "publicado" || s === "hecho";
const reciente = (it: LibretaItem) => !!it.doneAt && Date.now() - new Date(it.doneAt).getTime() < WEEK;

const fmtDay = new Intl.DateTimeFormat(locale, { day: "numeric", month: "short" });
const day = (iso?: string) => (iso && !iso.startsWith("0001") ? fmtDay.format(new Date(iso)) : "");

type Filtro = "open" | "done" | "all";

/** The Libreta tab. */
export function LibretaTab({ app }: { app: App }) {
  const { data, error, reload } = usePoll(() => api.libreta(app.name), [app.name], 30000);
  const [filtro, setFiltro] = useState<Filtro>("open");
  const [renovate, setRenovate] = useState(true);
  const [anotando, setAnotando] = useState(false);
  const puede = session.role !== "guest";

  const items = (data?.items ?? []).filter((it) => renovate || it.source !== "renovate");
  const open = items.filter((it) => abierto(it.status));
  const done = items.filter((it) => hecho(it.status));
  const gone = items.filter((it) => it.status === "descartado");
  const doneWeek = done.filter(reciente), goneWeek = gone.filter(reciente);
  const shownDone = filtro === "open" ? doneWeek : done;
  const shownGone = filtro === "open" ? goneWeek : filtro === "all" ? gone : [];

  return (
    <div className="libreta">
      <div className="libreta-cabeza">
        <p className="small muted">{t("What was asked for {app} and whether it is done. The states are computed from each branch's run, merge and release; nobody sets them by hand.", { app: app.name })}</p>
        {puede && (
          <button className="primary libreta-anotar" aria-expanded={anotando} onClick={() => setAnotando(!anotando)}>
            <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
            <span>{t("Note a request")}</span>
          </button>
        )}
      </div>
      {anotando && <Anotar app={app} onDone={() => { setAnotando(false); reload(); }} onCancel={() => setAnotando(false)} />}
      <ErrorBox error={error} />
      {!data && !error && <p className="muted">{t("Loading…")}</p>}
      {data && (
        <div className="libreta-cuerpo">
          <div className="libreta-lista">
            <div className="libreta-filtros" role="group" aria-label={t("Which requests")}>
              <button type="button" className="chip-btn" aria-pressed={filtro === "open"} onClick={() => setFiltro("open")}>{t("Open")} <span>· {open.length}</span></button>
              <button type="button" className="chip-btn" aria-pressed={filtro === "done"} onClick={() => setFiltro("done")}>{t("Done (requests)")} <span>· {done.length}</span></button>
              <button type="button" className="chip-btn" aria-pressed={filtro === "all"} onClick={() => setFiltro("all")}>{t("All")} <span>· {items.length}</span></button>
              <span className="libreta-hueco" />
              <label className="small muted libreta-renovate">
                <input type="checkbox" checked={renovate} onChange={(e) => setRenovate(e.target.checked)} /> {t("Include Renovate's")}
              </label>
            </div>
            {!data.github && <p className="small muted">{t("GitHub did not answer: merges come from the runs only, and Renovate's pull requests are missing.")}</p>}
            {items.length === 0 && (
              <div className="empty">{t("Nothing asked for {app} yet. Note a request here, or ask an agent from the work board.", { app: app.name })}</div>
            )}

            {filtro !== "done" && open.length > 0 && (
              <section aria-labelledby="libreta-abiertos" className="libreta-seccion">
                <h2 id="libreta-abiertos" className="libreta-titulo">{t("Open")}</h2>
                {open.map((it) => <Abierto key={it.source + it.id} app={app} it={it} />)}
              </section>
            )}
            {filtro !== "done" && open.length === 0 && items.length > 0 && <p className="muted">{t("Nothing open: everything asked is done or set aside.")}</p>}

            {shownDone.length > 0 && (
              <section aria-labelledby="libreta-hechos" className="libreta-seccion">
                <h2 id="libreta-hechos" className="libreta-titulo">{filtro === "open" ? t("Done this week") : t("Done (requests)")}</h2>
                <div className="libreta-hechos">
                  {shownDone.map((it) => <Hecho key={it.source + it.id} app={app} it={it} />)}
                </div>
              </section>
            )}

            {shownGone.length > 0 && (
              <section aria-labelledby="libreta-desc" className="libreta-seccion">
                <h2 id="libreta-desc" className="libreta-titulo">{t("Discarded (requests)")}</h2>
                <div className="libreta-descartados">
                  {shownGone.map((it) => (
                    <div key={it.source + it.id} className="libreta-descartado">
                      <span className="libreta-tachado">{it.title}</span>
                      <span className="small muted">{it.reason || (it.why === "closed" ? t("its pull request was closed without merging") : t("no reason was written"))}</span>
                    </div>
                  ))}
                </div>
              </section>
            )}
          </div>

          <aside className="libreta-lado" aria-label={t("How the libreta works")}>
            <div className="card libreta-semana">
              <h2>{t("This week")}</h2>
              <div className="libreta-cuentas">
                <div><strong className="ok">{doneWeek.length}</strong><span>{tn(doneWeek.length, "request done", "requests done")}</span></div>
                <div><strong className="warn">{open.length}</strong><span>{tn(open.length, "request open", "requests open")}</span></div>
                <div><strong>{goneWeek.length}</strong><span>{tn(goneWeek.length, "request discarded", "requests discarded")}</span></div>
              </div>
            </div>
            <div className="libreta-manda">
              <h2>{t("La Ruta is in charge")}</h2>
              <p>{t("The libreta only shows what is about this app. Each request is a Ruta entry, an encargo or a pull request of Renovate's; here it is seen together with its branch, its run and its release.")}</p>
              <p>{t("«Note a request» writes in the Ruta, with this app already set.")}</p>
              <Link to="/ruta">{t("See everything in the Ruta")}</Link>
            </div>
          </aside>
        </div>
      )}
    </div>
  );
}

/** Writing a request: a to-do in the Ruta, about this app. */
function Anotar({ app, onDone, onCancel }: { app: App; onDone: () => void; onCancel: () => void }) {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const save = () => {
    setBusy(true); setErr(undefined);
    api.anotarPedido(app.name, { title: title.trim(), body }).then(() => { setBusy(false); onDone(); }, (e) => { setBusy(false); setErr(e); });
  };
  return (
    <div className="card stack libreta-form">
      <label className="field"><span>{t("What is asked for {app}", { app: app.name })}</span>
        <input value={title} maxLength={200} autoFocus onChange={(e) => setTitle(e.target.value)} placeholder={t("e.g. That the fix can be started from the button, in any order")} />
      </label>
      <label className="field"><span>{t("Details (optional)")}</span>
        <textarea rows={3} value={body} maxLength={20000} onChange={(e) => setBody(e.target.value)} />
      </label>
      <p className="small muted" style={{ margin: 0 }}>{t("It is kept in the Ruta as a to-do about {app}; agents see it in app_estado.", { app: app.name })}</p>
      <ErrorBox error={err} />
      <div className="row">
        <button className="primary" disabled={busy || !title.trim()} onClick={save}>{t("Note it")}</button>
        <button onClick={onCancel}>{t("Cancel")}</button>
      </div>
    </div>
  );
}

/** Where a line comes from, as a link: its Ruta entry, its encargo or its pull request. */
function Origen({ app, it }: { app: App; it: LibretaItem }) {
  switch (it.source) {
    case "ruta": return <Link to={`/ruta?entrada=${it.id}`}>{t("Ruta entry {n}", { n: it.id })}</Link>;
    case "encargo": return <Link to={`/ruta?tab=encargos#encargo-${it.id}`}>{t("encargo {n}", { n: it.id })}</Link>;
    case "renovate": return <a href={`https://github.com/${app.repo}/pull/${it.id}`} target="_blank" rel="noreferrer">{t("pull request #{n}", { n: it.id })}</a>;
    default: return null;
  }
}

/** What a line's state means, in words. */
function porque(it: LibretaItem, app: App): string {
  const branch = it.branch ?? "";
  switch (it.why) {
    case "branch": return t("Branch {branch}, with no run yet", { branch });
    case "running": return t("Branch {branch}, testing", { branch });
    case "failed": return it.failed?.length ? t("Branch {branch}: its run failed in {steps}", { branch, steps: it.failed.join(", ") }) : t("Branch {branch}: its run failed", { branch });
    case "cancelled": return t("Branch {branch}: its run was cancelled", { branch });
    case "conflict": return t("Branch {branch} conflicts with {main}", { branch, main: app.defaultBranch });
    case "blocked": return t("It passed, but GitHub will not merge it yet (a review or a required check)");
    case "passed": return t("Branch {branch} passed: it can be merged", { branch });
    case "closed": return t("Its pull request was closed without merging: the request stands");
    case "taken": return it.agent ? t("{agent} has it", { agent: it.agent }) : t("an agent has it");
    case "review": return t("an agent delivered it: it waits for your review");
    case "merged": return t("Merged into {main}; no release carries it yet", { main: app.defaultBranch });
    case "verifying": return t("Release #{n} is being verified", { n: it.release ?? 0 });
    case "verify-failed": return t("Release #{n} did not pass its verification; the next one will carry it", { n: it.release ?? 0 });
    case "tests": return t("They only report until they pass {of} releases in a row · {done} of {of} so far", { of: it.progress?.of ?? 0, done: it.progress?.done ?? 0 });
    default: return "";
  }
}

/** Where a line is on the way: 0 asked, 1 on its branch, 2 merged, 3 shipped. */
const paso = (s: LibretaStatus) => (s === "publicado" ? 3 : s === "integrado" ? 2 : s === "pedido" ? 0 : 1);

/** An open line: its title and state, who asked, its way so far, and its links. */
function Abierto({ app, it }: { app: App; it: LibretaItem }) {
  const e = ESTADO[it.status];
  if (it.source === "pruebas") {
    const p = it.progress ?? { done: 0, of: 5 };
    return (
      <article className="card libreta-pedido">
        <div className="libreta-pedido-cabeza">
          <h3>{t("That its own tests can roll a release back")}</h3>
          <span className={`libreta-estado ${e.cls}`}>{t(e.label)}</span>
        </div>
        <p className="small muted">{porque(it, app)}{it.tests?.length ? ` · ${it.tests.join(", ")}` : ""}</p>
        <div className="libreta-barra" role="img" aria-label={t("{done} of {of} clean releases", { done: p.done, of: p.of })}
          style={{ gridTemplateColumns: `repeat(${p.of}, minmax(0, 1fr))` }}>
          {Array.from({ length: p.of }, (_, i) => <span key={i} className={i < p.done ? "on" : ""} />)}
        </div>
      </article>
    );
  }
  const n = paso(it.status);
  const medio = it.branch ? t("Branch {branch}", { branch: it.branch }) : t(e.label);
  const pasos = [t("Asked"), medio, t("Integrated"), t("Shipped")];
  const quien = it.source === "renovate" ? t("Renovate's pull request") : it.by ? t("Asked by {who} on {day}", { who: it.by, day: day(it.at) }) : "";
  const detalle = porque(it, app);
  return (
    <article className="card libreta-pedido">
      <div className="libreta-pedido-cabeza">
        <h3>{it.title}</h3>
        <span className={`libreta-estado ${e.cls}`}>{t(e.label)}</span>
      </div>
      <p className="small muted">{[quien, detalle].filter(Boolean).join(" · ")}</p>
      <ol className="libreta-pasos" aria-label={t("Progress")}>
        {pasos.map((p, i) => (
          <li key={i} className={i < n ? "hecho" : i === n ? `actual ${e.cls}` : ""} aria-current={i === n ? "step" : undefined}>
            {i < n ? "✓ " : i === n ? "● " : ""}{p}
          </li>
        ))}
      </ol>
      <div className="libreta-barra libreta-barra-pasos" role="img" aria-label={t("Step {n} of {of}: {step}", { n: n + 1, of: 4, step: pasos[n] })}>
        {pasos.map((_, i) => <span key={i} className={i < n ? "on" : i === n ? `actual ${e.cls}` : ""} />)}
      </div>
      <p className="small libreta-enlaces">
        <Origen app={app} it={it} />
        {it.cargo ? <> · {t("cargo #{n}", { n: it.cargo })}</> : null}
        {it.branch && <> · <Rama repo={app.repo} branch={it.branch} className="" /></>}
        {it.pull && it.source !== "renovate" ? <> · <a href={`https://github.com/${app.repo}/pull/${it.pull}`} target="_blank" rel="noreferrer">#{it.pull}</a></> : null}
        {it.run ? <> · <Link to={`/apps/${app.name}/runs/${it.run}`}>{t("run {n}", { n: it.run })}</Link></> : null}
      </p>
    </article>
  );
}

/** A done line: its title, and the merge and the release it went out in. */
function Hecho({ app, it }: { app: App; it: LibretaItem }) {
  const pull = it.pull ? `#${it.pull}` : "";
  const fin = it.status === "publicado" ? t("shipped in #{n}", { n: it.release ?? 0 })
    : it.status === "integrado" ? (it.release ? t("integrated · release #{n} pending", { n: it.release }) : t("integrated"))
      : it.why === "decided" ? t("a decision") : it.why === "accepted" ? t("accepted") : t("ticked in the Ruta");
  return (
    <div className="libreta-hecho">
      <span className="libreta-palomita" aria-hidden="true">✓</span>
      <span className="libreta-hecho-titulo">{it.source === "ruta" ? <Link to={`/ruta?entrada=${it.id}`}>{it.title}</Link> : it.title}</span>
      {it.source === "renovate" && <span className="chip">Renovate</span>}
      <span className="small muted">
        {pull && it.pull ? <a href={`https://github.com/${app.repo}/pull/${it.pull}`} target="_blank" rel="noreferrer">{pull}</a> : null}
        {pull ? " · " : ""}{fin}
      </span>
    </div>
  );
}
