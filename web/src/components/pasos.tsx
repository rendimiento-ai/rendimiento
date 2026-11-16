import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { duration, type Run, type Step } from "../api";
import { t } from "../i18n";
import { statusText } from "./ui";
import { FILA, tejer, totalSegundos } from "./tejido";

/**
 * A run's steps, shown one of two ways (the person picks, and the browser remembers it):
 * «the race», each step a runner in its lane across the run's time, with the place it arrived in; or «the threads»,
 * the run as a weaving, where a step that waits for others starts at the knot of their threads.
 */
type Tema = "carrera" | "hilos";
const CLAVE = "rendimiento.pasos.tema";

function temaGuardado(): Tema {
  try {
    return localStorage.getItem(CLAVE) === "hilos" ? "hilos" : "carrera";
  } catch {
    return "carrera";
  }
}

const icono: Record<Step["status"], string> = {
  succeeded: "M7 12.5l3 3 7-7", failed: "M8.5 8.5l7 7M15.5 8.5l-7 7", running: "M8 12h.01M12 12h.01M16 12h.01",
  pending: "", skipped: "M8 12h8", reused: "M8 10a4 4 0 0 1 7-1M16 14a4 4 0 0 1-7 1",
};

/** A step's color: its own (by its place in the run, the same in both ways), unless its status says more. */
function color(s: Step, i: number): string {
  if (s.status === "failed") return "var(--bad)";
  if (s.status === "skipped" || s.status === "pending") return "var(--muted)";
  if (s.status === "reused") return "var(--anil)";
  return `var(--hebra-${(i % 8) + 1})`;
}

const ms = (x?: string) => (x ? new Date(x).getTime() : undefined);

/** A place: 1st, 2nd… */
function lugar(n: number): string {
  const d = n % 10, c = n % 100;
  if (d === 1 && c !== 11) return t("{n}st", { n });
  if (d === 2 && c !== 12) return t("{n}nd", { n });
  if (d === 3 && c !== 13) return t("{n}rd", { n });
  return t("{n}th", { n });
}

/** What a screen reader says for a step: its id, status and duration. */
function nombre(s: Step): string {
  const d = s.startedAt ? duration(s.startedAt, s.finishedAt) : "";
  return [s.id, statusText(s.status), d].filter(Boolean).join(", ");
}

/**
 * The span a view's scale stands for while the run goes: it is held, and grows (with room to spare) only when what has
 * run passes it, so the bars and threads grow inside a still track instead of every one of them shrinking each second.
 * Once the run has ended, the scale is the exact span.
 */
function useHolgura(real: number, vivo: boolean, minimo: number, conPrevisto: boolean): number {
  const fijo = useRef(0);
  if (!vivo) {
    fijo.current = 0;
    return real;
  }
  if (real > fijo.current) fijo.current = Math.max(minimo, real * (conPrevisto ? 1.1 : 1.5));
  return fijo.current;
}

/** How long each step took in an earlier run that passed (ms), and when it finished from that run's start. */
function previos(antes?: Run): Map<string, { dura: number; llega: number }> {
  const m = new Map<string, { dura: number; llega: number }>();
  const steps = antes?.steps ?? [];
  const starts = steps.map((s) => ms(s.startedAt)).filter((x): x is number => x !== undefined);
  if (!starts.length) return m;
  const t0 = Math.min(...starts);
  for (const s of steps) {
    const a = ms(s.startedAt), b = ms(s.finishedAt);
    if (a !== undefined && b !== undefined && s.status === "succeeded") m.set(s.id, { dura: b - a, llega: b - t0 });
  }
  return m;
}

const andando = (run: Run) => run.status === "queued" || run.status === "running" || !!run.steps?.some((s) => s.status === "running");

function Icono({ s, c, r = 10, x = 12, y = 12 }: { s: Step; c: string; r?: number; x?: number; y?: number }) {
  const k = r / 10;
  return (
    <>
      <circle className="pasos-cuenta" cx={x} cy={y} r={r} fill={s.status === "pending" ? "none" : c} stroke={c} strokeWidth={s.status === "pending" ? 2 : 0} />
      {icono[s.status] && (
        <path d={icono[s.status]} transform={`translate(${x - 12 * k} ${y - 12 * k}) scale(${k})`} stroke="var(--surface)" strokeWidth="2.6" fill="none" strokeLinecap="round" strokeLinejoin="round" />
      )}
    </>
  );
}

export function Pasos({ run, antes, selected, onSelect }: { run: Run; antes?: Run; selected?: string; onSelect: (id: string) => void }) {
  const [tema, setTema] = useState<Tema>(temaGuardado);
  const elegir = (x: Tema) => {
    setTema(x);
    try { localStorage.setItem(CLAVE, x); } catch { /* this visit only */ }
  };
  // While a step runs, its bar or thread grows with the clock.
  const [, setTic] = useState(0);
  const corre = run.steps?.some((s) => s.status === "running");
  useEffect(() => {
    if (!corre) return;
    const id = setInterval(() => setTic((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, [corre]);

  return (
    <section className={`pasos pasos-${tema}`} aria-label={t("Steps")}>
      <div className="pasos-cab">
        <strong>{t("Steps")}</strong>
        <div className="pasos-tema" role="group" aria-label={t("How to show the steps")}>
          <button type="button" aria-pressed={tema === "carrera"} onClick={() => elegir("carrera")}>{t("The race")}</button>
          <button type="button" aria-pressed={tema === "hilos"} onClick={() => elegir("hilos")}>{t("The threads")}</button>
        </div>
      </div>
      {tema === "carrera" ? <Carrera run={run} antes={antes} selected={selected} onSelect={onSelect} /> : <Hilos run={run} antes={antes} selected={selected} onSelect={onSelect} />}
    </section>
  );
}

type Vista = { run: Run; antes?: Run; selected?: string; onSelect: (id: string) => void };

/**
 * The race: every step in its lane, the lane the run's whole time. Its bar goes from when it started to when it
 * finished (or now), a dotted stretch before it when it waited; at the right, the place it arrived in and how long it took.
 * While the run goes, a running bar shines and its head beats, a finished one stops where it arrived, and a faint mark
 * says where the step finished the last time the run passed.
 */
function Carrera({ run, antes, selected, onSelect }: Vista) {
  const steps = run.steps ?? [];
  const now = Date.now();
  const vivo = andando(run);
  const prev = previos(antes);
  const starts = steps.map((s) => ms(s.startedAt)).filter((x): x is number => x !== undefined);
  const t0 = starts.length ? Math.min(...starts) : now;
  const marca = (s: Step) => {
    const p = prev.get(s.id);
    if (!p) return undefined;
    const a = ms(s.startedAt);
    return a !== undefined ? a + p.dura : t0 + p.llega;
  };
  const real = Math.max(1000, ...steps.map((s) => (ms(s.finishedAt) ?? (s.startedAt ? now : t0)) - t0));
  const previsto = vivo ? Math.max(0, ...steps.map((s) => (marca(s) ?? t0) - t0)) : 0;
  const t1 = t0 + useHolgura(Math.max(real, previsto), vivo, 20000, prev.size > 0);
  const pct = (x: number) => ((x - t0) / (t1 - t0)) * 100;
  const llegada = steps
    .filter((s) => s.startedAt && s.finishedAt && s.status !== "running")
    .sort((a, b) => ms(a.finishedAt)! - ms(b.finishedAt)!)
    .map((s) => s.id);
  return (
    <>
      <ol className="carrera">
        {steps.map((s, i) => {
          const c = color(s, i);
          const a = ms(s.startedAt), b = ms(s.finishedAt) ?? now;
          const x = a !== undefined ? pct(a) : 0;
          const w = a !== undefined ? Math.max(1.5, Math.min(pct(b) - x, 100 - x)) : 0;
          const espero = a !== undefined && (s.dependsOn.length > 0 || a - t0 >= 1000);
          const puesto = llegada.indexOf(s.id);
          const m = vivo ? marca(s) : undefined;
          return (
            <li key={s.id}>
              <button type="button" className={`carrera-carril${s.id === selected ? " on" : ""}${s.status === "running" ? " vivo" : ""} es-${s.status}`}
                style={{ ["--c" as string]: c, ["--d" as string]: `${i * 50}ms` }}
                onClick={() => onSelect(s.id)} aria-current={s.id === selected} aria-label={nombre(s)}>
                <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true"><Icono s={s} c={c} /></svg>
                <span className="carrera-nombre">
                  <strong>{s.id}</strong>{" "}
                  <span className="small muted">{t(s.kind)}{s.dependsOn.length > 0 ? ` · ${t("after {steps}", { steps: s.dependsOn.join(", ") })}` : ""}</span>
                </span>
                <span className="carrera-llegada small muted" aria-hidden="true">
                  {puesto >= 0 && <><b>{lugar(puesto + 1)}</b> · </>}
                  {s.startedAt ? duration(s.startedAt, s.finishedAt) : ""}
                </span>
                <span className="carrera-pista" aria-hidden="true">
                  {espero && <span className="carrera-espera" style={{ width: `${x}%` }} />}
                  {m !== undefined && <span className="carrera-antes" style={{ left: `${Math.min(100, pct(m))}%` }} />}
                  {a !== undefined && s.status !== "skipped" && <span className="carrera-tramo" style={{ left: `${x}%`, width: `${w}%` }} />}
                </span>
              </button>
            </li>
          );
        })}
      </ol>
      <div className="carrera-escala small muted" aria-hidden="true">
        <span>{duration(new Date(t0).toISOString(), new Date(t0).toISOString())}</span>
        <span>{duration(new Date(t0).toISOString(), new Date(t1).toISOString())}</span>
      </div>
    </>
  );
}

/**
 * The threads: from the change, a thread for every step that waits for nobody; a thread as long as its step took,
 * ending in its bead. A step that waits for others starts at the knot where their threads come together.
 */
function Hilos({ run, antes, selected, onSelect }: Vista) {
  const steps = run.steps ?? [];
  const now = Date.now();
  const vivo = andando(run);
  const prev = vivo ? previos(antes) : new Map<string, { dura: number }>();
  const por = new Map(steps.map((s, i) => [s.id, { s, i }]));
  const hebras = steps.map((s) => {
    const a = ms(s.startedAt), p = prev.get(s.id);
    return {
      id: s.id, dependsOn: s.dependsOn, previsto: p && p.dura / 1000,
      segundos: a === undefined ? undefined : Math.max(0, ((ms(s.finishedAt) ?? now) - a) / 1000),
    };
  });
  // While it goes, the scale is held (and so the canvas): the threads grow inside it.
  const escala = useHolgura(totalSegundos(hebras), vivo, 20, prev.size > 0);
  const tejido = tejer(hebras, vivo ? escala : undefined);
  const fin = new Map(tejido.hilos.map((h) => [h.id, h]));
  const { origen } = tejido;
  // A touch chooses the step without the browser scrolling the box to bring the focused thread into view.
  const toque = (e: PointerEvent<SVGGElement>) => {
    if (e.pointerType === "mouse" && e.button !== 0) return;
    e.preventDefault();
    e.currentTarget.focus({ preventScroll: true });
  };
  const tecla = (id: string) => (e: KeyboardEvent) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onSelect(id);
    }
  };
  return (
    <div className="hilos-caja">
      <svg className="hilos" width={tejido.ancho} height={tejido.alto} viewBox={`0 0 ${tejido.ancho} ${tejido.alto}`} role="group" aria-label={t("The steps as threads")}>
        {/* The dotted lines first, under the threads: from each bead to the knot of the step that waits for it. */}
        {tejido.hilos.map((h) => h.deps.map((d) => {
          const o = fin.get(d)!;
          const k = Math.max(o.x1 + 10, h.x0 - 28);
          return <path key={`${d}>${h.id}`} className="hilos-espera" d={`M${o.x1} ${o.y}H${k}C${k + 18} ${o.y} ${h.x0 - 14} ${h.y} ${h.x0} ${h.y}`} />;
        }))}
        {tejido.hilos.filter((h) => h.deps.length > 0).map((h) => (
          <circle key={`nudo-${h.id}`} className="hilos-nudo" cx={h.x0} cy={h.y} r={6} />
        ))}
        <circle className="hilos-origen" cx={origen.x} cy={origen.y} r={10} />
        <text className="hilos-m" x={origen.x - 10} y={origen.y + 28}>{t("the change")}</text>
        {tejido.hilos.map((h) => {
          const { s, i } = por.get(h.id)!;
          const c = color(s, i);
          const d = h.deps.length
            ? `M${h.x0} ${h.y}H${h.x1}`
            : `M${origen.x} ${origen.y}C${origen.x + 36} ${origen.y} ${h.x0 - 34} ${h.y} ${h.x0} ${h.y}H${h.x1}`;
          const on = s.id === selected;
          const corto = h.corto && s.status !== "reused"; // not started (pending, skipped): a faint dashed thread
          const corre = s.status === "running";
          return (
            <g key={s.id} className={`hilos-paso${on ? " on" : ""}${corre ? " vivo" : ""}${corto ? " corto" : ""}`}
              style={{ ["--c" as string]: c, ["--d" as string]: `${i * 70}ms` }}
              role="button" tabIndex={0} aria-label={nombre(s)} aria-current={on}
              onPointerDown={toque} onClick={() => onSelect(s.id)} onKeyDown={tecla(s.id)}>
              <rect className="hilos-toque" x={h.x0 - 4} y={h.y - FILA / 2 - 4} width={Math.max(h.x1 - h.x0 + 70, s.id.length * 7.2 + 12)} height={FILA} rx={10} />
              {on && <path className="hilos-elegido" d={`M${h.x0} ${h.y}H${h.x1}`} />}
              {h.xPrevisto !== undefined && <path className="hilos-antes" d={`M${h.xPrevisto} ${h.y - 8}V${h.y + 8}`} />}
              <path className="hilos-hilo" d={d} pathLength={corto ? undefined : 1} />
              {corre && <path className="hilos-avanza" d={`M${h.x0} ${h.y}H${h.x1}`} />}
              <Icono s={s} c={c} r={9} x={h.x1} y={h.y} />
              <text x={h.x0 + 2} y={h.y - 11}>{s.id}</text>
              <text className="hilos-m" x={h.x1 + 15} y={h.y + 4}>{s.startedAt ? duration(s.startedAt, s.finishedAt) : statusText(s.status)}</text>
            </g>
          );
        })}
      </svg>
    </div>
  );
}
