// The interface's use by stages (decision 197): each release of rendimiento that changed the interface is a stage, with
// its little flag over the days' views, in the color the days after it take; and, for the stage chosen, the same days
// before and after it compared: views a day, pages for each run opened, the time to data and the section opened most.
// It says when the days after are too few to conclude. Only totals: never who.
import { useState } from "react";
import { api, type Etapa, type MedidasEtapa } from "../api";
import { ErrorBox, usePoll } from "./ui";
import { lang, t, tn } from "../i18n";

const tonos = ["var(--cempasuchil)", "var(--anil)", "var(--nopal)", "var(--rosa)", "#7B2F8E", "var(--grana)"];
const fmt = (n: number, d = 1) => n.toLocaleString(lang === "es" ? "es-MX" : "en-US", { maximumFractionDigits: d });
const diaCorto = (d: string) => new Date(`${d}T12:00:00Z`).toLocaleDateString(lang === "es" ? "es-MX" : "en-US", { day: "numeric", month: "short" });

/** How each app section reads (the uso.ts actions «open_…»). */
const seccionName: Record<string, string> = {
  open_glance: "At a glance", open_board: "Work board", open_runs: "Runs", open_roads: "Roads", open_releases: "Releases",
  open_services: "Services", open_reliability: "Reliability", open_visits: "Visits", open_notebook: "Notebook", open_settings: "Settings",
};

/** The change between two numbers in words and its tone: lower is better for the ways and the waits. */
function cambio(antes: number, despues: number, menosEsMejor: boolean): { texto: string; tono: string } {
  if (!antes || !despues) return { texto: "—", tono: "" };
  const pct = Math.round(((despues - antes) / antes) * 100);
  if (Math.abs(pct) < 5) return { texto: t("about the same"), tono: "" };
  const mejor = menosEsMejor ? pct < 0 : pct > 0;
  return { texto: `${pct > 0 ? "+" : ""}${pct} %`, tono: menosEsMejor ? (mejor ? "mejor" : "peor") : "" };
}

function Cifra({ titulo, antes, despues, texto, menosEsMejor }: { titulo: string; antes: number; despues: number; texto: (n: number) => string; menosEsMejor?: boolean }) {
  const c = cambio(antes, despues, !!menosEsMejor);
  return (
    <div className="etapa-cifra">
      <span className="etapa-cifra-que">{titulo}</span>
      <strong>{antes ? texto(antes) : "—"} <span aria-hidden="true">→</span><span className="sr-only">{t("then")}</span> {despues ? texto(despues) : "—"}</strong>
      <span className={`small etapa-cambio ${c.tono}`}>{c.texto}</span>
    </div>
  );
}

function Seccion({ m }: { m: MedidasEtapa }) {
  return m.seccion ? <>{t(seccionName[m.seccion] ?? m.seccion)} · {Math.round((m.seccionParte ?? 0) * 100)} %</> : <>—</>;
}

/** The stages over the days' views, and the comparison of the one chosen. */
export function EtapasUso({ dias }: { dias: number }) {
  const { data, error } = usePoll(() => api.usoEtapas(Math.max(dias, 30)), [dias], 0);
  const [elegida, setElegida] = useState<number>();
  if (error) return <ErrorBox error={error} />;
  if (!data) return null;
  const etapas = data.etapas;
  const e: Etapa | undefined = etapas.find((x) => x.release === elegida) ?? etapas[etapas.length - 1];
  const max = Math.max(1, ...data.dias.map((d) => d.vistas));
  const n = data.dias.length;
  const diaDe = (at: string) => at.slice(0, 10);
  // Each day takes the color of the last stage on or before it.
  const tramo = (d: string) => etapas.filter((x) => diaDe(x.at) <= d).length;
  const ancho = 100 / Math.max(n, 1);
  return (
    <section className="card stack uso-etapas" aria-labelledby="uso-etapas-titulo">
      <div className="row between" style={{ flexWrap: "wrap", gap: 8 }}>
        <h3 id="uso-etapas-titulo" style={{ margin: 0 }}>{t("Stages")}</h3>
        <span className="small muted">{t("Each release of rendimiento that changed the interface is a stage: its flag shows what it did to the clicks.")}</span>
      </div>
      {data.sin === "propia" && <p className="small muted" style={{ margin: 0 }}>{t("The stages come from rendimiento's own releases (SELF_APP), and this installation does not build itself.")}</p>}
      {n > 0 && (
        <div className="etapas-grafica" role="img" aria-label={t("Views a day over {n} days, with {m} stages", { n, m: etapas.length })}>
          <svg viewBox="0 0 100 60" preserveAspectRatio="none" aria-hidden="true">
            {data.dias.map((d, i) => {
              const h = Math.max(1, (d.vistas / max) * 46);
              return <rect key={d.dia} className="etapas-barra" x={i * ancho + ancho * 0.12} y={58 - h} width={ancho * 0.76} height={h} rx="0.8"
                fill={tonos[tramo(d.dia) % tonos.length]} style={{ animationDelay: `${i * 0.02}s` }}><title>{`${diaCorto(d.dia)}: ${d.vistas}`}</title></rect>;
            })}
          </svg>
          {etapas.map((x, k) => {
            const i = data.dias.findIndex((d) => d.dia >= diaDe(x.at));
            if (i < 0) return null;
            // A flag past the middle hangs to the left of its line, so it stays inside the chart.
            const derecha = i * ancho > 55;
            const lado = derecha ? { right: `${100 - i * ancho}%` } : { left: `${i * ancho}%` };
            const c = { ["--c" as string]: tonos[(k + 1) % tonos.length], ["--d" as string]: `${k * -0.7}s` };
            // Its line is drawn apart (it takes no clicks); the flags go on two rows, so neighbours never cover each other.
            return (
              <span key={x.release}>
                <span className={`etapa-linea${derecha ? " derecha" : ""}`} aria-hidden="true" style={{ ...lado, ...c }} />
                <button type="button" className={`etapa-bandera${derecha ? " derecha" : ""}${e?.release === x.release ? " on" : ""}`}
                  style={{ ...lado, ...c, top: `${(k % 2) * 26}px` }} onClick={() => setElegida(x.release)} title={x.texto}>
                  <span>#{x.release}{x.rama ? ` · ${x.rama}` : ""}</span>
                </button>
              </span>
            );
          })}
        </div>
      )}
      {etapas.length === 0 && data.sin !== "propia" && <p className="small muted" style={{ margin: 0 }}>{t("No release changed the interface in these days.")}</p>}
      {e && (
        <div className="stack" style={{ gap: 10 }}>
          <label className="row small" style={{ gap: 8, flexWrap: "wrap" }}>
            <strong>{t("Before and after")}</strong>
            <select value={e.release} onChange={(ev) => setElegida(Number(ev.target.value))}>
              {[...etapas].reverse().map((x) => (
                <option key={x.release} value={x.release}>{t("Release #{n} · {day} · {what}", { n: x.release, day: diaCorto(diaDe(x.at)), what: x.rama || x.texto })}</option>
              ))}
            </select>
          </label>
          <p className="small" style={{ margin: 0 }}>{e.texto}</p>
          <div className="etapa-cifras">
            <Cifra titulo={t("Views a day")} antes={e.antes.vistasDia} despues={e.despues.vistasDia} texto={(v) => fmt(v)} />
            <Cifra titulo={t("Pages for each run opened")} antes={e.antes.paginasPorCorrida ?? 0} despues={e.despues.paginasPorCorrida ?? 0} texto={(v) => fmt(v)} menosEsMejor />
            <Cifra titulo={t("Time to data")} antes={e.antes.msDatos ?? 0} despues={e.despues.msDatos ?? 0} texto={(v) => `${fmt(v / 1000)} s`} menosEsMejor />
            <div className="etapa-cifra">
              <span className="etapa-cifra-que">{t("Section opened most")}</span>
              <strong><Seccion m={e.despues} /></strong>
              <span className="small muted">{t("before: {what}", { what: e.antes.seccion ? t(seccionName[e.antes.seccion] ?? e.antes.seccion) : "—" })}</span>
            </div>
          </div>
          <p className="small muted" style={{ margin: 0 }}>
            {tn(e.dias, "The same {n} day before and after its day (its own day left out, it holds both).", "The same {n} days before and after its day (its own day left out, it holds both).")}{" "}
            {e.pocos && <strong className="etapa-pocos">{t("Too few days after it yet to conclude anything.")}</strong>}
          </p>
        </div>
      )}
    </section>
  );
}
