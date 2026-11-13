import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, session, type DayBoard, type DayCard } from "../api";
import { usePoll } from "./ui";
import { still } from "./maqueta";
import { lang, t, tn } from "../i18n";

const HIDE_KEY = "rendimiento.resumen.oculto";

/** The day it was hidden on, so it shows again the next day. */
function hiddenOn(): string {
  try { return localStorage.getItem(HIDE_KEY) ?? ""; } catch { return ""; }
}

/** A day as the reader says it: «Ayer, 12 de noviembre». */
function dayTitle(day: string) {
  const d = new Date(day + "T12:00:00");
  const yesterday = new Date(Date.now() - 864e5);
  const locale = lang === "es" ? "es-MX" : "en";
  const name = d.toLocaleDateString(locale, { day: "numeric", month: "long" });
  return d.toDateString() === yesterday.toDateString() ? t("Yesterday, {day}", { day: name }) : d.toLocaleDateString(locale, { weekday: "long", day: "numeric", month: "long" });
}

/**
 * The day's summary at the top of the home page, as a scoreboard: the newest day's numbers counting up, its runs by hour,
 * each app with a ring of the runs that passed and the line the platform's AI wrote about it, and the day's lotería card.
 * Older days since the reader's last visit (at most three) keep their lines. It asks again while a day is being written.
 */
export function ResumenDelDia() {
  const [hidden, setHidden] = useState(() => hiddenOn() === new Date().toDateString());
  const [redoing, setRedoing] = useState(false);
  const { data, setData } = usePoll(() => api.summary(), [], 20000);
  if (hidden || !data) return null;
  const boards = data.boards ?? {};
  const newest = Object.keys(boards).sort().reverse()[0] ?? data.days[0]?.day;
  if (!newest) return null;
  const board = boards[newest];
  const today = data.days.find((d) => d.day === newest);
  const older = data.days.filter((d) => d.day !== newest);
  if (!today && !data.pending && (!board || (board.runs === 0 && board.releases === 0))) return null;
  const hide = () => {
    try { localStorage.setItem(HIDE_KEY, new Date().toDateString()); } catch { /* the next visit shows it */ }
    setHidden(true);
  };
  const redo = (day: string) => {
    setRedoing(true);
    api.redoSummary(day).then(() => setData({ ...data, pending: true, days: data.days.filter((x) => x.day !== day) }), () => {}).finally(() => setRedoing(false));
  };
  const lineOf = (app: string) => today?.lines.find((l) => l.app === app)?.text;
  return (
    <section className="card resumen-dia marcador" aria-label={t("The day's summary")}>
      <div className="marcador-cabeza">
        <div>
          <div className="resumen-dia-fecha">{dayTitle(newest)}</div>
          <h2 className="marcador-titulo">{t("The day's scoreboard")}</h2>
        </div>
        {board && <Insignias b={board} />}
      </div>

      {board && (
        <div className="marcador-cifras">
          <Cifra valor={board.releases} que={tn(board.releases, "release", "releases")} detalle={board.apps.filter((a) => a.releases > 0).map((a) => `${a.releases} ${a.app}`).join(", ") || t("none that day")} color="var(--cempasuchil)" />
          <Cifra valor={board.runs} que={tn(board.runs, "run", "runs")} detalle={t("{ok} passed, {bad} failed", { ok: board.runs - board.failed, bad: board.failed })} color="var(--ok)" />
          <Cifra valor={board.merges} que={tn(board.merges, "merge", "merges")} detalle={t("pull requests rendimiento merged")} color="var(--maiz)" />
          <Cifra valor={board.problems} que={tn(board.problems, "problem", "problems")} detalle={board.problems ? t("that started that day") : t("none started")} color="var(--bad)" />
        </div>
      )}

      {board && (
        <div className="marcador-medio">
          <PorHora horas={board.hours} />
          {board.card && <Carta card={board.card} />}
        </div>
      )}

      {board && board.apps.length > 0 && (
        <ul className="marcador-apps">
          {board.apps.map((a) => (
            <li key={a.app}>
              <Anillo ok={a.runs - a.failed} total={a.runs} />
              <div>
                <Link to={`/apps/${a.app}`} className="marcador-app">{a.app}</Link>
                <div className="small">{lineOf(a.app) ?? t("{n} releases · {ok} of {total} runs passed", { n: a.releases, ok: a.runs - a.failed, total: a.runs })}</div>
              </div>
            </li>
          ))}
        </ul>
      )}
      {today && today.lines.filter((l) => !l.app).map((l, i) => <p key={i} className="marcador-org small">{l.text}</p>)}

      {older.map((d) => (
        <div key={d.day}>
          <div className="resumen-dia-fecha">{dayTitle(d.day)}</div>
          <ul>
            {d.lines.map((l, i) => (
              <li key={i}>{l.app && <><Link to={`/apps/${l.app}`}>{l.app}</Link>: </>}{l.text}</li>
            ))}
          </ul>
        </div>
      ))}
      {data.pending && <p className="small muted" style={{ margin: 0 }}>{t("Writing the summary of the last days…")}</p>}
      <div className="resumen-dia-pie small muted">
        <span>{data.days.some((d) => d.source === "ia") ? t("The numbers are counted from what was recorded; the sentences were written by the platform's AI and can be wrong.") : data.days.some((d) => d.source === "counts") ? t("Only the numbers: the AI's answer could not be used this time. Redo tries again.") : t("Written from what was recorded.")}</span>
        {session.role !== "guest" && today && <button className="link small" disabled={redoing} onClick={() => redo(today.day)}>{t("Redo")}</button>}
        <button className="link small" onClick={hide}>{t("Hide for today")}</button>
      </div>
    </section>
  );
}

/** The two badges: releases all healthy, and work waiting for a review. */
function Insignias({ b }: { b: DayBoard }) {
  return (
    <div className="marcador-insignias">
      {b.releases > 0 && (
        <span className={`badge ${b.healthy === b.releases ? "ok" : "warn"}`}>
          {t("{n} of {total} releases healthy", { n: b.healthy, total: b.releases })}
        </span>
      )}
      {b.toReview > 0 && (
        <Link to="/ruta?tab=encargos" className="badge warn">{tn(b.toReview, "{n} encargo to review", "{n} encargos to review")}</Link>
      )}
    </div>
  );
}

/** A number that counts up to its value when it first shows (still with movement off). */
function useSube(target: number): number {
  const [v, setV] = useState(() => (still() ? target : 0));
  useEffect(() => {
    if (still()) { setV(target); return; }
    const start = performance.now();
    let frame = 0;
    const step = (now: number) => {
      const p = Math.min(1, (now - start) / 1200);
      setV(Math.round(target * (1 - Math.pow(1 - p, 3))));
      if (p < 1) frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    return () => cancelAnimationFrame(frame);
  }, [target]);
  return v;
}

function Cifra({ valor, que, detalle, color }: { valor: number; que: string; detalle: string; color: string }) {
  const v = useSube(valor);
  return (
    <div className="marcador-cifra">
      <span className="marcador-numero" style={{ color }} aria-hidden="true">{v}</span>
      <span className="marcador-que"><span className="sr-only">{valor} </span>{que}</span>
      <span className="small muted">{detalle}</span>
    </div>
  );
}

/** The runs by hour, passed below and failed on top, with the hours at 0, 6, 12, 18 and 23. */
function PorHora({ horas }: { horas: [number, number][] }) {
  const top = Math.max(1, ...horas.map(([ok, bad]) => ok + bad));
  const h = 110, w = 24 * 14;
  const failedAt = horas.map(([, bad], i) => (bad ? i : -1)).filter((i) => i >= 0);
  const label = failedAt.length
    ? t("Runs by hour; the failed ones between {from} and {to} h.", { from: failedAt[0], to: failedAt[failedAt.length - 1] })
    : t("Runs by hour; none failed.");
  return (
    <figure className="marcador-horas">
      <figcaption className="small"><b>{t("Runs by hour")}</b> <span className="marcador-leyenda"><i className="ok" />{t("passed")} <i className="bad" />{t("failed")}</span></figcaption>
      <div className="marcador-horas-caja">
        <svg viewBox={`0 0 ${w} ${h + 18}`} role="img" aria-label={label}>
          <line x1="0" x2={w} y1={h} y2={h} stroke="var(--border)" />
          {horas.map(([ok, bad], i) => {
            const hOk = (ok / top) * (h - 6), hBad = (bad / top) * (h - 6);
            return (
              <g key={i} className="marcador-barra" style={{ animationDelay: `${i * 0.025}s` }}>
                <title>{t("{h} h: {ok} passed, {bad} failed", { h: i, ok, bad })}</title>
                {ok > 0 && <rect x={i * 14 + 2} y={h - hOk} width="10" height={hOk} rx="3" fill="var(--ok)" />}
                {bad > 0 && <rect x={i * 14 + 2} y={h - hOk - hBad - (ok ? 2 : 0)} width="10" height={hBad} rx="3" fill="var(--bad)" />}
              </g>
            );
          })}
          {[0, 6, 12, 18, 23].map((i) => <text key={i} x={i * 14 + 7} y={h + 14} textAnchor="middle" fontSize="10" fill="var(--muted)">{i}</text>)}
        </svg>
      </div>
    </figure>
  );
}

/** A ring of the share of runs that passed (no ring for an app with no runs that day). */
function Anillo({ ok, total }: { ok: number; total: number }) {
  if (total === 0) return <span className="marcador-anillo vacio" aria-hidden="true" />;
  const c = 2 * Math.PI * 22, pct = Math.round((ok / total) * 100);
  return (
    <svg viewBox="0 0 56 56" className="marcador-anillo" role="img" aria-label={t("{ok} of {total} runs passed", { ok, total })}>
      <circle cx="28" cy="28" r="22" fill="none" stroke="var(--bad)" strokeWidth="7" opacity=".85" />
      <circle cx="28" cy="28" r="22" fill="none" stroke="var(--ok)" strokeWidth="7" strokeLinecap="round" strokeDasharray={c} strokeDashoffset={c * (1 - ok / total)} transform="rotate(-90 28 28)" className="llena" />
      <text x="28" y="32" textAnchor="middle" fontSize="12" fontWeight="800" fill="var(--text)">{pct}%</text>
    </svg>
  );
}

const cartas: Record<DayCard["kind"], { numero: string; nombre: string; color: string }> = {
  nube: { numero: "3", nombre: "The cloud", color: "var(--muted)" },
  tropiezo: { numero: "2", nombre: "The stumble", color: "var(--teja)" },
  flor: { numero: "1", nombre: "The flower", color: "var(--cempasuchil)" },
  luna: { numero: "4", nombre: "The moon", color: "var(--anil)" },
  colibri: { numero: "5", nombre: "The hummingbird", color: "var(--nopal)" },
};

/** What a card says on its back. */
function frase(c: DayCard): string {
  const app = c.app ?? "";
  switch (c.kind) {
    case "nube": return tn(c.count, "One problem or rollback in {app}: worth a look.", "{n} problems or rollbacks, most in {app}: worth a look.", { app });
    case "tropiezo": return t("{n} of {of} runs failed, most in {app}: a hard day of tests.", { n: c.count, of: c.of ?? 0, app });
    case "flor": return tn(c.count, "One release, and it stayed healthy.", "{n} releases, and every one stayed healthy.");
    case "luna": return tn(c.count, "One release went out at night.", "{n} releases went out at night.");
    case "colibri": return tn(c.count, "An agent finished something and it waits for your review.", "Agents finished {n} things and they wait for your review.");
  }
}

/** The day's lotería card: the most notable thing of the day. It turns over when touched, to say what it was. */
function Carta({ card }: { card: DayCard }) {
  const [vuelta, setVuelta] = useState(false);
  const c = cartas[card.kind];
  const where = card.kind === "colibri" ? "/ruta?tab=encargos" : card.app ? `/apps/${card.app}` : "";
  return (
    <div className="marcador-carta-lugar">
      <button type="button" className={`marcador-carta ${vuelta ? "vuelta" : ""}`} style={{ ["--carta" as string]: c.color }} onClick={() => setVuelta(!vuelta)} aria-pressed={vuelta}
        aria-label={`${t(c.nombre)}: ${frase(card)}`}>
        <span className="marcador-carta-cara">
          <span className="marcador-carta-arriba"><b>{c.numero}</b><span>{card.app || t("the day")}</span></span>
          <Dibujo kind={card.kind} />
          <span className="marcador-carta-cifra">{card.kind === "tropiezo" ? `${card.count}/${card.of}` : card.count}</span>
          <span className="marcador-carta-nombre">{t(c.nombre)}</span>
        </span>
        <span className="marcador-carta-dorso">
          <span>{frase(card)}</span>
          <span className="marcador-carta-nombre">{t(c.nombre)}</span>
        </span>
      </button>
      {vuelta && where && <Link to={where} className="small">{t("Go there →")}</Link>}
    </div>
  );
}

function Dibujo({ kind }: { kind: DayCard["kind"] }) {
  return (
    <svg viewBox="0 0 120 120" className="marcador-dibujo" aria-hidden="true">
      <circle cx="60" cy="60" r="54" fill="var(--surface-2)" />
      <g className="marcador-late">
        {kind === "flor" && <><circle cx="60" cy="60" r="32" fill="var(--cempasuchil)" /><circle cx="60" cy="60" r="20" fill="var(--cempasuchil-2)" /><circle cx="60" cy="60" r="8" fill="var(--flor-centro)" /></>}
        {kind === "tropiezo" && <><rect x="32" y="56" width="56" height="32" rx="8" fill="var(--teja)" /><circle cx="60" cy="44" r="15" fill="var(--teja)" /><path d="M50 42 l6 5 M64 42 l6 5" stroke="var(--surface)" strokeWidth="3" strokeLinecap="round" /></>}
        {kind === "nube" && <><ellipse cx="60" cy="62" rx="38" ry="18" fill="var(--muted)" /><ellipse cx="49" cy="49" rx="20" ry="16" fill="var(--muted)" /><ellipse cx="72" cy="51" rx="16" ry="13" fill="var(--muted)" /><path d="M48 84 l-5 11 M62 84 l-5 11 M76 84 l-5 11" stroke="var(--anil)" strokeWidth="3" strokeLinecap="round" /></>}
        {kind === "luna" && <path d="M74 26 A35 35 0 1 0 83 83 A29 29 0 1 1 74 26 Z" fill="var(--maiz)" />}
        {kind === "colibri" && <><ellipse cx="56" cy="64" rx="24" ry="11" fill="var(--nopal)" /><path d="M54 56 Q40 24 69 35 Q61 48 59 56 Z" fill="var(--rosa)" /><path d="M78 61 L104 56" stroke="var(--text)" strokeWidth="3" strokeLinecap="round" /><circle cx="73" cy="61" r="2.5" fill="var(--text)" /></>}
      </g>
    </svg>
  );
}
