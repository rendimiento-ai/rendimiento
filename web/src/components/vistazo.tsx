import { Link } from "react-router-dom";
import { api, type App, type Reliability, type Visits } from "../api";
import { usePoll } from "./ui";
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
 * The glance at an app, for whoever does not need the technical side: a cempasúchil that blooms while the app runs, a
 * hummingbird while an agent works on it, a sentence of what changed, its visits and its week, and what it needs of you, only
 * when it needs something. The details stay a tap further.
 */
export function Vistazo({ app }: { app: App }) {
  const name = app.name;
  const { data: rel } = usePoll(() => api.reliability(name, "7d"), [name], 60000);
  const { data: vis } = usePoll(() => api.visits(name, "7d"), [name], 300000);
  const { data: resumen } = usePoll(() => api.summary().catch(() => undefined), [], 300000);
  const { data: encargos } = usePoll(() => api.encargos().catch(() => []), [], 60000);
  const sem = semana(rel);
  const flor = floracion(app, sem.enLinea, sem.dias);
  const mios = (encargos ?? []).filter((e) => e.app === name);
  const trabajando = mios.some((e) => e.status === "taken" || e.status === "waiting");
  const porRevisar = mios.filter((e) => e.status === "delivered").length;
  const frase = resumen?.days.flatMap((d) => d.lines).find((l) => l.app === name)?.text;
  const cambios = rel?.releases.length ?? 0;
  const malDia = [...sem.dias].reverse().find((d) => d.estado === "caida" || d.estado === "lenta");
  const fallo = app.lastRun?.deploy && app.lastRun.status === "failed";
  return (
    <section className="vistazo">
      <div className="vistazo-flor">
        <FlorGrande estado={flor} colibri={trabajando} />
        <div>
          <div className={`vistazo-palabra ${flor}`}>{t(palabra[flor])}</div>
          {frase && <p className="vistazo-frase">{frase}</p>}
          {trabajando && <p className="vistazo-agente">{t("An agent is working on it. You don't need to do anything.")}</p>}
        </div>
      </div>

      <div className="vistazo-datos">
        {sem.enLinea !== undefined && <Anillo parte={sem.enLinea} />}
        <div className="vistazo-semana">
          <div className="small muted">{t("This week, day by day")}</div>
          <div className="vistazo-dias" role="img" aria-label={sem.dias.map((d) => `${diaCorto(d.day)}: ${t(estadoDia[d.estado])}`).join(", ")}>
            {sem.dias.map((d) => <span key={d.day} className={`vistazo-dia ${d.estado}`} title={`${diaCorto(d.day)}: ${t(estadoDia[d.estado])}`}><i />{diaCorto(d.day).slice(0, 2)}</span>)}
          </div>
          {malDia && <div className="small muted">{t(malDia.estado === "caida" ? "On {day} it did not answer for a while." : "On {day} it was slow for a while.", { day: diaLargo(malDia.day) })}</div>}
        </div>
      </div>

      <Visitas vis={vis} />

      <div className="vistazo-chips">
        {sem.respuestaMs !== undefined && <span className="chip"><b>{respuesta(sem.respuestaMs)}</b> {t("to answer")}</span>}
        <span className="chip"><b>{cambios}</b> {tn(cambios, "change this week", "changes this week")}</span>
      </div>

      {(porRevisar > 0 || fallo) && (
        <div className="vistazo-usted">
          <strong>{t("For you")}</strong>
          {fallo && <Link to={`/apps/${name}/runs/${app.lastRun!.id}`}>{t("The last publication failed: see why")}</Link>}
          {porRevisar > 0 && <Link to="/ruta?tab=encargos">{tn(porRevisar, "An agent finished something for you to review", "{n} things agents finished wait for your review")}</Link>}
        </div>
      )}

      <div className="vistazo-pie">
        <Link to={`/apps/${name}/mesa`} className="vistazo-mas">{t("See the details →")}</Link>
        <label className="small muted"><input type="checkbox" checked={tecnicoPrimero()} onChange={(e) => ponerTecnico(e.target.checked)} /> {t("Show me the technical side first")}</label>
      </div>
    </section>
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

/** The share of the week in line, as a ring that fills. */
function Anillo({ parte }: { parte: number }) {
  const c = 2 * Math.PI * 50, pct = Math.round(parte * 1000) / 10;
  const color = parte >= 0.999 ? "var(--nopal)" : parte >= 0.99 ? "var(--maiz)" : "var(--grana)";
  return (
    <svg viewBox="0 0 120 120" width="104" role="img" aria-label={t("{pct}% in line this week", { pct })} className="vistazo-anillo">
      <circle cx="60" cy="60" r="50" fill="none" stroke="var(--border)" strokeWidth="12" />
      <circle className="llena" cx="60" cy="60" r="50" fill="none" stroke={color} strokeWidth="12" strokeLinecap="round"
        strokeDasharray={c} strokeDashoffset={c * (1 - parte)} transform="rotate(-90 60 60)" />
      <text x="60" y="58" textAnchor="middle" fontSize="20" fontWeight="800" fill="var(--text)">{pct}%</text>
      <text x="60" y="77" textAnchor="middle" fontSize="11" fill="var(--muted)">{t("in line")}</text>
    </svg>
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
      <div className="vistazo-chips">
        <span className="chip"><b>{cur}</b> {tn(cur, "visitor this week", "visitors this week")}</span>
        {cambio !== undefined && <span className="chip"><b>{cambio > 0 ? "+" : ""}{cambio}%</b> {t("than the week before")}</span>}
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
