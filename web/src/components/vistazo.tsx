import { Link } from "react-router-dom";
import { api, duration, isLibrary, type App, type Reliability, type Run, type Step, type Visits } from "../api";
import { LoUltimo } from "./lo-ultimo";
import { statusText, usePoll } from "./ui";
import { BuildNow, puedeConstruir } from "./construir";
import { porque } from "./mesa";
import { locale, t, tn } from "../i18n";

/** How an app is, in one word: a bud before its first release, open and breathing when it runs, closing when it is slow or fails now and then, withered when it does not answer. */
export type Floracion = "brote" | "abierta" | "cerrandose" | "marchita";

/** A day of the week as a dot: how its checks went. */
export interface Dia { day: string; estado: "ok" | "lenta" | "caida" | "nada" }

/** The week's numbers from the uptime checks: share in line, typical answer time, one dot per day. */
export function semana(r: Reliability | undefined, now = new Date()): { enLinea?: number; respuestaMs?: number; dias: Dia[] } {
  const dias: Dia[] = [];
  for (let i = 6; i >= 0; i--) {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() - i);
    dias.push({ day: d.toDateString(), estado: "nada" });
  }
  if (!r) return { dias };
  let total = 0, ok = 0, p50 = 0;
  const porDia = new Map<string, { total: number; ok: number; cubetas: number; lentas: number }>();
  for (const s of r.series) {
    total += s.total; ok += s.ok; p50 += s.p50Ms * s.total;
    for (const b of s.buckets) {
      const k = new Date(b.at).toDateString();
      const v = porDia.get(k) ?? { total: 0, ok: 0, cubetas: 0, lentas: 0 };
      v.total += b.total; v.ok += b.ok;
      if (b.total > 0) { v.cubetas++; if (b.p95Ms >= 1000) v.lentas++; }
      porDia.set(k, v);
    }
  }
  for (const d of dias) {
    const v = porDia.get(d.day);
    if (!v || v.total === 0) continue;
    // A blip is not a slow day: each release restarts the app and may miss a check or two. A day is slow when more
    // than 1 in 200 checks failed, or when a tenth of its stretches answered in a second or more (p95).
    const parte = v.ok / v.total;
    d.estado = parte < 0.9 ? "caida" : parte < 0.995 || v.lentas > v.cubetas / 10 ? "lenta" : "ok";
  }
  return { enLinea: total ? ok / total : undefined, respuestaMs: total ? p50 / total : undefined, dias };
}

/** The flower's state from what the page knows of the app and its week. */
export function floracion(app: App, enLinea?: number, dias: Dia[] = []): Floracion {
  if (!app.status.release) return "brote";
  if ((app.down?.length ?? 0) > 0 || app.status.phase === "Error" || app.status.phase === "Degraded") return "marchita";
  const hoy = dias[dias.length - 1]?.estado;
  if ((enLinea !== undefined && enLinea < 0.99) || hoy === "lenta" || hoy === "caida") return "cerrandose";
  return "abierta";
}

const palabra: Record<Floracion, string> = { brote: "Not live yet", abierta: "It's alive", cerrandose: "A little slow", marchita: "Not answering" };

/**
 * The glance at an app, for whoever does not need the technical side. Its head, the width of the card: a cempasúchil that
 * blooms while the app runs (a hummingbird while an agent works on it), its state in a word, a sentence of what changed,
 * its addresses, its numbers, and the two buttons. Below, three columns that fill: what runs now and what waits for you;
 * the week; and its diary, with what it still needs for its flower to tell more. The details stay a tap further.
 */
export function Vistazo({ app, onBuilt }: { app: App; onBuilt?: () => void }) {
  const name = app.name;
  const { data: rel } = usePoll(() => api.reliability(name, "7d"), [name], 60000);
  const { data: vis } = usePoll(() => api.visits(name, "7d"), [name], 300000);
  const { data: resumen } = usePoll(() => api.summary().catch(() => undefined), [], 300000);
  const { data: encargos } = usePoll(() => api.encargos().catch(() => []), [], 60000);
  const sem = semana(rel);
  const flor = floracion(app, sem.enLinea, sem.dias);
  const trabajando = (encargos ?? []).some((e) => e.app === name && (e.status === "taken" || e.status === "waiting"));
  const frase = resumen?.days.flatMap((d) => d.lines).find((l) => l.app === name)?.text;
  const cambios = rel?.releases.length ?? 0;
  const malDia = [...sem.dias].reverse().find((d) => d.estado === "caida" || d.estado === "lenta");
  return (
    <section className="vistazo">
      <div className="vistazo-cabeza">
        <FlorGrande estado={flor} colibri={trabajando} />
        <div className="vistazo-texto">
          <div className={`vistazo-palabra ${flor}`}>{t(palabra[flor])}</div>
          {frase && <p className="vistazo-frase">{frase}</p>}
          {trabajando && <p className="vistazo-agente">{t("An agent is working on it. You don't need to do anything.")}</p>}
          <Enlaces app={app} />
          <div className="vistazo-chips">
            {sem.respuestaMs !== undefined && <span className="chip"><b>{respuesta(sem.respuestaMs)}</b> {t("to answer")}</span>}
            <span className="chip"><b>{cambios}</b> {tn(cambios, "change this week", "changes this week")}</span>
            {sem.enLinea !== undefined && <span className="chip"><b>{Math.round(sem.enLinea * 1000) / 10}%</b> {t("online")}</span>}
          </div>
        </div>
        <div className="vistazo-acciones">
          {puedeConstruir(app) && <BuildNow name={name} main={app.defaultBranch} onDone={() => onBuilt?.()} />}
          <Link className="btn" to={`/apps/${name}/runs`}>{t("See runs")}</Link>
        </div>
      </div>

      <div className="vistazo-columnas">
        <section className="vistazo-col vistazo-ahora" aria-labelledby="vistazo-ahora">
          <h2 id="vistazo-ahora">{t("Now")}</h2>
          <Ahora app={app} />
          <h3>{t("Waiting for you")}</h3>
          <LeEspera app={app} />
        </section>

        <section className="vistazo-col vistazo-semana" aria-labelledby="vistazo-semana">
          <h2 id="vistazo-semana">{t("This week")}</h2>
          <div className="vistazo-dias" role="img" aria-label={sem.dias.map((d) => `${diaCorto(d.day)}: ${t(estadoDia[d.estado])}`).join(", ")}>
            {sem.dias.map((d) => <span key={d.day} className={`vistazo-dia ${d.estado}`} title={`${diaCorto(d.day)}: ${t(estadoDia[d.estado])}`}><i />{diaCorto(d.day).slice(0, 2)}</span>)}
          </div>
          <div className="small muted">
            {malDia ? t(malDia.estado === "caida" ? "On {day} it did not answer for a while." : "On {day} it was slow for a while.", { day: diaLargo(malDia.day) })
              : sem.enLinea !== undefined ? t("Every day went well.") : t("No checks yet this week.")}
          </div>
          <Visitas vis={vis} />
        </section>

        <section className="vistazo-col vistazo-diario" aria-labelledby="vistazo-diario">
          <h2 id="vistazo-diario">{t("The latest")}</h2>
          <LoUltimo app={app} n={5} plain />
          <Pasos app={app} conRevisiones={sem.enLinea !== undefined} vis={vis} />
        </section>
      </div>

      <div className="vistazo-pie">
        <Link to={`/apps/${name}/mesa`} className="vistazo-mas">{t("See the details →")}</Link>
        <label className="small muted"><input type="checkbox" checked={tecnicoPrimero()} onChange={(e) => ponerTecnico(e.target.checked)} /> {t("Show me the technical side first")}</label>
      </div>
    </section>
  );
}

/** A step of a run as a short bar: green when done, amber while it runs, red when it failed, grey before. */
const barra = (s: Step["status"]) => (s === "succeeded" || s === "reused" ? "ok" : s === "running" ? "va" : s === "failed" ? "mal" : s === "skipped" ? "salto" : "");

/**
 * What runs for the app now: each run (three at most) with its branch, its steps as bars, the step it is on and for how
 * long, and the way to it. When nothing runs, one quiet line says so.
 */
function Ahora({ app }: { app: App }) {
  const name = app.name;
  // The page hears each run start and end (it reloads the app): the list follows at once, not at the next poll.
  const { data: runs } = usePoll(() => api.runs(name), [name, app.lastRun?.id, app.lastRun?.status], 10000);
  const vivas = (runs ?? []).filter((r) => r.status === "running" || r.status === "queued").slice(0, 3);
  const ids = vivas.map((r) => r.id).join(",");
  const { data: detalle } = usePoll(() => (vivas.length ? Promise.all(vivas.map((r) => api.run(r.id).catch(() => r))) : Promise.resolve([] as Run[])), [ids], 5000);
  if (!runs) return null;
  if (vivas.length === 0) return <p className="vistazo-tranquilo small muted">{t("Nothing is running for it now.")}</p>;
  return (
    <div className="vistazo-corren">
      {vivas.map((r0) => {
        const r = detalle?.find((d) => d.id === r0.id) ?? r0;
        const pasos = r.steps ?? [];
        const ahora = pasos.find((s) => s.status === "running");
        const cola = r.status === "queued";
        return (
          <div key={r.id} className={`vistazo-corre${cola ? " cola" : ""}`}>
            <div className="vistazo-corre-que">
              <Link to={`/apps/${name}/runs/${r.id}`}><strong>{t("Run #{n}", { n: r.id })}</strong></Link>
              <span className="mono small">{r.branch}</span>
            </div>
            {pasos.length > 0 && (
              <div className="vistazo-barras" role="img" aria-label={pasos.map((s) => `${s.id}: ${statusText(s.status)}`).join(", ")}>
                {pasos.map((s) => <i key={s.id} className={barra(s.status)} />)}
              </div>
            )}
            <div className="small muted">
              {cola ? t("Waiting its turn") : ahora ? <><b className="mono">{ahora.id}</b> · {t("for {time}", { time: duration(ahora.startedAt) })}</> : t("Running for {time}", { time: duration(r.startedAt ?? r.createdAt) })}
            </div>
          </div>
        );
      })}
    </div>
  );
}

/**
 * What waits for the person on this app: the last publication when it failed, and the work board's "your turn" lines
 * (a pull request that passed or failed, a conflict, an encargo to review), each a door to the board. Quiet when none.
 */
function LeEspera({ app }: { app: App }) {
  const name = app.name;
  const { data } = usePoll(() => api.mesa(name).catch(() => undefined), [name, app.lastRun?.id, app.lastRun?.status], 60000);
  const fallo = app.lastRun?.deploy && app.lastRun.status === "failed";
  const suyas = (data?.lines ?? []).filter((l) => l.group === "you");
  if (!data && !fallo) return null;
  if (suyas.length === 0 && !fallo) return <p className="vistazo-tranquilo small muted">{t("Nothing waits for you.")}</p>;
  const MAX = 4;
  return (
    <ul className="vistazo-espera">
      {fallo && <li><Link to={`/apps/${name}/runs/${app.lastRun!.id}`}>{t("The last publication failed: see why")}</Link></li>}
      {suyas.slice(0, MAX).map((l) => (
        <li key={(l.branch ?? "") + (l.encargos?.[0]?.id ?? "")}>
          <Link to={`/apps/${name}/mesa`}>{l.pullTitle || l.branch || l.encargos?.[0]?.title}</Link>
          <div className="small muted">{porque(l, app)}</div>
        </li>
      ))}
      {suyas.length > MAX && <li><Link to={`/apps/${name}/mesa`} className="small">{t("{n} more on the work board →", { n: suyas.length - MAX })}</Link></li>}
    </ul>
  );
}

/** Where the app answers: each service's address (its domain, or on the network at home), without repeats. */
function Enlaces({ app }: { app: App }) {
  const vistos = new Set<string>();
  const enlaces = (app.status.services ?? []).flatMap((s) => [s.url, s.lanURL].filter((u): u is string => !!u).map((u) => ({ u, s: s.name })))
    .filter(({ u }) => !vistos.has(u) && !!vistos.add(u));
  if (enlaces.length === 0) return null;
  const varios = new Set(enlaces.map((e) => e.s)).size > 1;
  return (
    <ul className="vistazo-enlaces" aria-label={t("Where it answers")}>
      {enlaces.map(({ u, s }) => (
        <li key={u}><a href={u} target="_blank" rel="noreferrer">{u.replace(/^https?:\/\//, "").replace(/\/$/, "")} ↗</a>{varios && <span className="small muted"> · {s}</span>}</li>
      ))}
    </ul>
  );
}

/**
 * What the app is missing for its glance to tell more, each with the way to get it. A step leaves when it is done, and
 * the whole card leaves with the last one, so an app that has its numbers never sees it.
 */
function Pasos({ app, conRevisiones, vis }: { app: App; conRevisiones: boolean; vis?: Visits }) {
  const base = `/apps/${app.name}`;
  if (isLibrary(app.spec) || !app.status.release) return null;
  const pasos: { id: string; marca: string; titulo: string; texto: string; ir?: { to: string; label: string } }[] = [];
  if (!conRevisiones) pasos.push({ id: "revisiones", marca: "Next", titulo: "Knowing whether it answers", texto: "A check every minute fills in the days of the week. They start by themselves once it is published.", ir: { to: `${base}/reliability`, label: "See its availability" } });
  if (vis && (!vis.configured || vis.reports.length === 0)) pasos.push({ id: "visitas", marca: "Optional", titulo: "Counting visits", texto: "Without cookies. The week's bars appear here.", ir: { to: `${base}/visits`, label: "How to turn them on" } });
  if (!app.spec.previews) pasos.push({ id: "copias", marca: "Try it", titulo: "Seeing a change before merging it", texto: "Add previews: {} to rendimiento.yaml, and each pull request gets its own live copy." });
  if (pasos.length === 0) return null;
  return (
    <div className="vistazo-pasos">
      <div className="small muted">{t("For its flower to tell you more")}</div>
      <div className="vistazo-pasos-lista">
        <div className="vistazo-paso hecho"><span className="marca">✓ {t("Done")}</span><strong>{t("Published")}</strong><span className="small muted">{t("Release #{n} is live.", { n: app.status.release })}</span></div>
        {pasos.map((p) => (
          <div key={p.id} className="vistazo-paso">
            <span className="marca">{t(p.marca)}</span>
            <strong>{t(p.titulo)}</strong>
            <span className="small muted">{t(p.texto)}</span>
            {p.ir && <Link to={p.ir.to} className="small">{t(p.ir.label)} →</Link>}
          </div>
        ))}
      </div>
    </div>
  );
}

const estadoDia: Record<Dia["estado"], string> = { ok: "all well", lenta: "slow for a while", caida: "did not answer for a while", nada: "no checks" };
const diaCorto = (d: string) => new Date(d).toLocaleDateString(locale, { weekday: "short" });
const diaLargo = (d: string) => new Date(d).toLocaleDateString(locale, { weekday: "long" });
const respuesta = (ms: number) => (ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${Math.round(ms)} ms`);

const TECNICO = "rendimiento.vistazo.tecnico";
/** Whether this person asked to see the technical side first (on this device). */
export function tecnicoPrimero(): boolean {
  try { return localStorage.getItem(TECNICO) === "1"; } catch { return false; }
}
function ponerTecnico(on: boolean) {
  try { if (on) localStorage.setItem(TECNICO, "1"); else localStorage.removeItem(TECNICO); } catch { /* this visit only */ }
  const app = location.pathname.split("/").slice(0, 3).join("/");
  location.assign(on ? app + "/mesa" : app);
}

/** On the work board, for whoever chose the technical side first: the way back to the glance. */
export function VolverAlVistazo() {
  if (!tecnicoPrimero()) return null;
  return (
    <label className="small muted" style={{ display: "block", marginBottom: 8 }}>
      <input type="checkbox" checked onChange={() => ponerTecnico(false)} /> {t("Show me the technical side first")}
    </label>
  );
}

/** The week's visits as bars, with how they compare with the week before. */
function Visitas({ vis }: { vis?: Visits }) {
  if (!vis?.configured || !vis.reports.length) return null;
  const puntos = vis.reports[0].pageviews.slice(-7);
  const cur = vis.reports.reduce((s, r) => s + r.current.visitors, 0), prev = vis.reports.reduce((s, r) => s + r.previous.visitors, 0);
  const max = Math.max(1, ...puntos.map((p) => p.n));
  const cambio = prev ? Math.round(((cur - prev) / prev) * 100) : undefined;
  return (
    <div className="vistazo-visitas">
      <svg viewBox="0 0 240 60" role="img" aria-label={t("Visits of the last seven days")}>
        {puntos.map((p, i) => {
          const h = Math.max(4, (p.n / max) * 54);
          return <rect key={p.at} className="barra" x={i * 34 + 4} y={58 - h} width="26" height={h} rx="6" fill="var(--rosa)" style={{ animationDelay: `${i * 0.08}s` }}><title>{p.n}</title></rect>;
        })}
      </svg>
      <div className="small muted">
        {tn(cur, "{n} visitor", "{n} visitors")}
        {cambio !== undefined && cambio !== 0 && <> · {t(cambio > 0 ? "{pct}% more than the week before" : "{pct}% fewer than the week before", { pct: Math.abs(cambio) })}</>}
        {cambio === 0 && <> · {t("as many as the week before")}</>}
      </div>
    </div>
  );
}

/**
 * A cempasúchil, big: open and breathing while the app runs, its petals drawn in while it is slow, grey and drooping when it
 * does not answer, a green bud before its first release. A hummingbird flies by while an agent works on it.
 */
export function FlorGrande({ estado, colibri }: { estado: Floracion; colibri?: boolean }) {
  const petalos = [[100, 62], [138, 100], [100, 138], [62, 100], [127, 73], [127, 127], [73, 127], [73, 73]];
  const color = estado === "marchita" ? "var(--muted)" : estado === "cerrandose" ? "var(--maiz)" : "var(--cempasuchil)";
  return (
    <svg viewBox="0 0 220 200" className={`flor-grande ${estado}`} role="img" aria-label={t(palabra[estado])}>
      <path d="M100 196 C100 170 98 150 100 112" stroke="var(--nopal)" strokeWidth="7" fill="none" strokeLinecap="round" className="tallo" />
      <path d="M99 172 C80 162 70 148 72 138 C88 141 98 152 99 172" fill="var(--nopal)" />
      {estado === "brote" ? (
        <g className="brote"><ellipse cx="100" cy="98" rx="18" ry="26" fill="var(--nopal)" /><path d="M100 72 C108 86 108 104 100 120 C92 104 92 86 100 72" fill="var(--cempasuchil)" opacity=".7" /></g>
      ) : (
        <g className="corola">
          {petalos.map(([x, y], i) => <circle key={i} cx={x} cy={y} r={i < 4 ? 24 : 22} fill={color} />)}
          <circle cx="100" cy="100" r="26" fill={estado === "marchita" ? "var(--border)" : "var(--maiz)"} />
          <circle cx="100" cy="100" r="13" fill={estado === "marchita" ? "var(--muted)" : "var(--teja)"} />
        </g>
      )}
      {colibri && (
        <g className="colibri">
          <ellipse cx="10" cy="8" rx="10" ry="5" fill="var(--anil)" />
          <path d="M19 7 L30 5 L19 9 Z" fill="var(--text)" />
          <path className="ala" d="M6 4 Q2 -8 12 -4 Z" fill="var(--rosa)" />
        </g>
      )}
    </svg>
  );
}
