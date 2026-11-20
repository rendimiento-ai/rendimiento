import { useState } from "react";
import { Link } from "react-router-dom";
import { api, session, timeAgo, type PaginaUso, type UsoView } from "../api";
import { lang, t, tn } from "../i18n";
import { ErrorBox, Switch, usePoll } from "../components/ui";
import { Esqueleto, Hueso } from "../components/esqueleto";
import { DESCONOCIDA, type Accion } from "../uso";

/** The book's page on the use, in the reader's language. */
const LIBRO = lang === "es" ? "https://aprende.rendimiento.ai/guide/uso/" : "https://learn.rendimiento.ai/guide/uso/";

/** A page's name for people, from its route pattern (the pattern itself when it has none). */
function nombre(ruta: string): string {
  const n: Record<string, string> = {
    "/": t("Apps"), "/new": t("New app"), "/mercado": t("Mercado"), "/mercado/nuevo": t("A new Mercado page"), "/mercado/:app": t("A Mercado page"),
    "/environment": t("Environment"), "/universe": t("Universe"), "/universe/requests/:id": t("A request"), "/services": t("Services"),
    "/addons": t("Add-ons"), "/addons/:name": t("An add-on"), "/problems": t("Problems"), "/ruta": t("Ruta"), "/bitacora": t("Bitácora"),
    "/senales": t("Señales de vida"), "/ia": t("AI"), "/buzon": t("Comments"), "/escribir": t("Write to the team"), "/members": t("Members"),
    "/services/:ns/:name": t("A service"), "/apps/:name": t("An app"), "/apps/:name/:tab": t("A tab of an app"),
    "/apps/:name/runs/:id": t("A run"), "/uso": t("Use of the interface"), "/numeros": t("Numbers"), [DESCONOCIDA]: t("A page that does not exist"),
  };
  return n[ruta] ?? ruta;
}

/** An action's name for people. */
function accion(a: string): string {
  const n: Record<Accion, string> = {
    build_now: t("Build now"), retry: t("Retry a run"), cancel_run: t("Cancel a run"), merge: t("Merge this branch"),
    dismiss_problem: t("Dismiss a problem"), ask_agent: t("Ask the agent why it failed"), select_step: t("Open a step"),
    switch_theme: t("Switch day and night"), switch_language: t("Switch the language"), rollback: t("Roll back"),
    customize_home: t("Open Customize home"), save_home: t("Save the home page"),
  };
  return n[a as Accion] ?? a;
}

const segundos = (ms: number) => `${(ms / 1000).toLocaleString(lang === "es" ? "es-MX" : "en-US", { maximumFractionDigits: 1 })} s`;

/** The share of a page's measured views that took 2 s or more. */
const lentas = (p: PaginaUso) => (p.medidas ? (p.cubetas[3] + p.cubetas[4]) / p.medidas : 0);

/**
 * The longest common path, estimated from the moves: from the most common first page, the most common next
 * page each time, while it is still a common move. The same people need not have walked all of it.
 */
function camino(v: UsoView): { paginas: string[]; veces: number } | undefined {
  const pasos = v.pasos.filter((p) => p.desde !== p.hacia);
  const inicio = pasos.filter((p) => p.desde === "").sort((a, b) => b.veces - a.veces)[0];
  if (!inicio) return undefined;
  const paginas = [inicio.hacia];
  let veces = inicio.veces;
  while (paginas.length < 8) {
    const aqui = paginas[paginas.length - 1];
    const salen = pasos.filter((p) => p.desde === aqui);
    const total = salen.reduce((s, p) => s + p.veces, 0);
    const sig = salen.filter((p) => !paginas.includes(p.hacia)).sort((a, b) => b.veces - a.veces)[0];
    // A next page counts when it is at least 2 moves and a fifth of those leaving this page.
    if (!sig || sig.veces < 2 || sig.veces < total / 5) break;
    paginas.push(sig.hacia);
    veces = Math.min(veces, sig.veces);
  }
  return paginas.length >= 3 ? { paginas, veces } : undefined;
}

/** One list with a bar for each row's share, like the visits' lists. */
function Barras({ title, rows, empty, slow }: { title: string; rows: { key: string; label: string; n: number; value: string; title?: string }[]; empty: string; slow?: boolean }) {
  const max = Math.max(...rows.map((r) => r.n), 1);
  return (
    <section className={`card vis-top${slow ? " uso-lentas" : ""}`}>
      <h3>{title}</h3>
      {rows.length === 0 ? <p className="small muted">{empty}</p> : (
        <table>
          <tbody>
            {rows.map((r, i) => (
              <tr key={r.key} title={r.title}>
                <td>
                  <span className="vis-bar" style={{ width: `${(r.n / max) * 100}%`, animationDelay: `${Math.min(i, 12) * 40}ms` }} aria-hidden="true" />
                  <span className="vis-label">{r.label}</span>
                </td>
                <td className="num uso-valor">{r.value}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

/**
 * How rendimiento's own interface is used, for the platform's administrators: what is noted and the switch, and
 * over 7, 14 or 30 days the pages most opened, the slowest to show their data, the moves, the actions and the
 * longest common path.
 */
export function UsoPage() {
  const [dias, setDias] = useState(14);
  const { data, error, setData } = usePoll(() => api.uso(dias), [dias], 60000);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const cambiar = async (on: boolean) => {
    setBusy(true);
    setErr(undefined);
    try {
      const v = await api.putUso(on, dias);
      session.uso = v.encendido;
      setData(v);
    } catch (e) { setErr(e); } finally { setBusy(false); }
  };
  const fmt = (n: number) => n.toLocaleString(lang === "es" ? "es-MX" : "en-US");
  const vacio = t("Nothing yet: the numbers appear as people use the interface.");
  const medidas = (data?.paginas ?? []).filter((p) => p.medidas > 0).sort((a, b) => b.msTotal / b.medidas - a.msTotal / a.medidas).slice(0, 10);
  const c = data ? camino(data) : undefined;
  return (
    <>
      <h1>{t("Use of the interface")}</h1>
      <p className="sub">{t("How people move through rendimiento's own pages, so clicks can be taken away where they cost the most. Only totals per day, kept here for 90 days: never who.")}{" "}
        <a href={LIBRO} target="_blank" rel="noreferrer">{t("More in the book")}</a></p>
      <ErrorBox error={err ?? error} />
      {!data && !error && <Esqueleto><Hueso h={232} r={14} /></Esqueleto>}
      {data && (
        <section className="card stack uso-que" data-listo="">
          <div className="row between" style={{ flexWrap: "wrap", gap: 8 }}>
            <span>
              <span className={`badge ${data.encendido ? "ok" : ""}`}>{data.encendido ? t("on") : t("off")}</span>{" "}
              {data.encendido ? t("The interface notes how it is used.") : t("Turned off: the interface sends nothing.")}
              {data.updatedBy && <span className="small muted"> {t("Changed by {who}, {when}.", { who: data.updatedBy, when: timeAgo(data.updatedAt ?? "") })}</span>}
            </span>
            <Switch checked={data.encendido} disabled={busy} onChange={cambiar} label={data.encendido ? t("Turn off") : t("Turn on")} />
          </div>
          <div className="uso-columnas">
            <div>
              <strong>{t("What is noted")}</strong>
              <ul className="small">
                <li>{t("The page opened, by its kind (“a run”), never its address: no app's name, no run's number.")}</li>
                <li>{t("The page it came from, the same way.")}</li>
                <li>{t("How long the page took to show its data, on the pages that say when their data is in.")}</li>
                <li>{t("The main buttons used, by a fixed name (“Build now”, “Retry a run”).")}</li>
              </ul>
            </div>
            <div>
              <strong>{t("What is never noted")}</strong>
              <ul className="small">
                <li>{t("Who: no account, no session, no address of the computer.")}</li>
                <li>{t("Anything typed, and what the pages show.")}</li>
                <li>{t("Anything from a read-only demo.")}</li>
              </ul>
            </div>
          </div>
        </section>
      )}
      <div className="rel-filters" role="group" aria-label={t("Time range")} style={{ marginTop: 20 }}>
        {[7, 14, 30].map((d) => (
          <button key={d} aria-pressed={dias === d} onClick={() => setDias(d)}>{tn(d, "Last {n} days", "Last {n} days")}</button>
        ))}
      </div>
      {!data && !error && (
        <Esqueleto className="stack">
          <Hueso h={104} r={14} />
          <div className="uso-columnas">{Array.from({ length: 4 }, (_, i) => <Hueso key={i} h={300} r={14} />)}</div>
        </Esqueleto>
      )}
      {data && (
        <div className="stack">
          {c && (
            <section className="card">
              <h3 style={{ margin: "0 0 8px", fontSize: 14 }}>{t("The longest common path")} <span className="small muted">· {t("an estimate")}</span></h3>
              <div className="uso-camino">
                {c.paginas.map((p, i) => <span key={p}>{i > 0 && <em aria-hidden="true">›</em>}<b>{nombre(p)}</b></span>)}
              </div>
              <p className="small muted" style={{ margin: "8px 0 0" }}>
                {tn(c.paginas.length - 1, "{n} click from the first page to the last", "{n} clicks from the first page to the last")} ·{" "}
                {tn(c.veces, "its least common move was made {n} time", "its least common move was made {n} times")}.{" "}
                {t("Made by following the most common next page from each one: the same people need not have walked all of it.")}
              </p>
            </section>
          )}
          <div className="vis-tops">
            <Barras title={t("Most opened pages")} empty={vacio}
              rows={data.paginas.slice(0, 12).map((p) => ({ key: p.ruta, label: nombre(p.ruta), n: p.vistas, value: fmt(p.vistas), title: p.ruta }))} />
            <Barras title={t("Slowest to show their data")} slow empty={t("No times yet: they appear as people open the pages that are measured.")}
              rows={medidas.map((p) => ({
                key: p.ruta, label: nombre(p.ruta), n: p.msTotal / p.medidas, value: segundos(p.msTotal / p.medidas),
                title: t("{avg} on average · {slow}% took 2 s or more · {n} measured", { avg: segundos(p.msTotal / p.medidas), slow: Math.round(lentas(p) * 100), n: fmt(p.medidas) }),
              }))} />
            <Barras title={t("Most common moves")} empty={vacio}
              rows={data.pasos.filter((p) => p.desde !== "").slice(0, 12).map((p) => ({ key: `${p.desde} ${p.hacia}`, label: `${nombre(p.desde)} › ${nombre(p.hacia)}`, n: p.veces, value: fmt(p.veces) }))} />
            <Barras title={t("Actions used most")} empty={t("No actions yet.")}
              rows={data.acciones.slice(0, 12).map((a) => ({ key: `${a.ruta} ${a.accion}`, label: `${accion(a.accion)} · ${nombre(a.ruta)}`, n: a.veces, value: fmt(a.veces) }))} />
          </div>
          {medidas.length > 0 && (
            <section className="card">
              <h3 style={{ margin: "0 0 8px", fontSize: 14 }}>{t("Time to data, page by page")}</h3>
              <div style={{ overflowX: "auto" }}>
                <table className="bitacora small">
                  <thead><tr><th>{t("Page")}</th><th>{t("Average")}</th><th>{t("2 s or more")}</th><th>{t("Measured")}</th></tr></thead>
                  <tbody>
                    {medidas.map((p) => (
                      <tr key={p.ruta}><td>{nombre(p.ruta)}</td><td>{segundos(p.msTotal / p.medidas)}</td><td>{Math.round(lentas(p) * 100)}%</td><td>{fmt(p.medidas)}</td></tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="small muted" style={{ margin: "8px 0 0" }}>{t("From the moment the address changes until the page's data is on screen. Measured on Apps, an app, a run, the Universe, a request, Environment and this page; the others count their visits only.")}</p>
            </section>
          )}
          <p className="small muted">{t("Also in")} <Link to="/members">{t("Members")}</Link>{" · "}<Link to="/senales">{t("Señales de vida")}</Link></p>
        </div>
      )}
    </>
  );
}
