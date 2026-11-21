// The Log page: every line rendimiento's own processes wrote at INFO and
// above, in one place and newest first, for the platform's administrators.
// Filters by process, app, level, time and text; older lines come by a
// cursor; each line opens to its attributes.
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, timeAgo, type LogFilter, type LogLine } from "../api";
import { locale, t } from "../i18n";
import { ErrorBox, usePoll } from "../components/ui";
import { Vigia } from "../components/vigia";
import { EsqTabla } from "../components/esqueleto";

/** The processes of a cluster, and the single one of a computer. */
const PROCESSES = ["api", "control", "worker", "agents", "traces", "all", "local"];

/** How far back the page looks. */
const RANGES: { id: string; label: string; hours: number }[] = [
  { id: "1h", label: "Last hour", hours: 1 },
  { id: "24h", label: "Last 24 hours", hours: 24 },
  { id: "7d", label: "Last 7 days", hours: 24 * 7 },
  { id: "30d", label: "Last 30 days", hours: 24 * 30 },
];

function levelBadge(l: LogLine["level"]) {
  return l === "ERROR" ? "bad" : l === "WARN" ? "warn" : "ok";
}

/** One line: when, level, process, app and message; it opens to its attributes. */
function Linea({ l }: { l: LogLine }) {
  const [open, setOpen] = useState(false);
  const attrs = Object.entries(l.attrs ?? {});
  const detalle = attrs.length > 0 || l.run !== undefined;
  return (
    <>
      <tr className={`registro-fila registro-${l.level.toLowerCase()}`}>
        <td title={new Date(l.at).toLocaleString(locale)}>{timeAgo(l.at)}</td>
        <td><span className={`badge ${levelBadge(l.level)}`}>{l.level}</span></td>
        <td className="mono">{l.process}</td>
        <td>{l.app ? <Link to={`/apps/${encodeURIComponent(l.app)}`}>{l.app}</Link> : <span className="muted">—</span>}</td>
        <td className="registro-mensaje">
          {detalle ? (
            <button type="button" className="registro-abrir" aria-expanded={open} onClick={() => setOpen(!open)}>
              <span aria-hidden="true">{open ? "▾" : "▸"}</span> {l.msg}
            </button>
          ) : l.msg}
        </td>
      </tr>
      {open && detalle && (
        <tr className="registro-detalle">
          <td colSpan={5}>
            <dl className="registro-attrs">
              {l.run !== undefined && (
                <div className="registro-attr"><dt className="mono">run</dt><dd className="mono">
                  {l.app ? <Link to={`/apps/${encodeURIComponent(l.app)}/runs/${l.run}`}>#{l.run}</Link> : `#${l.run}`}
                </dd></div>
              )}
              {attrs.map(([k, v]) => (
                <div key={k} className="registro-attr"><dt className="mono">{k}</dt><dd className="mono">{typeof v === "string" ? v : JSON.stringify(v)}</dd></div>
              ))}
            </dl>
          </td>
        </tr>
      )}
    </>
  );
}

/** The Log page (/registro). */
export function Registro() {
  const [proceso, setProceso] = useState("");
  const [app, setApp] = useState("");
  const [nivel, setNivel] = useState<LogFilter["nivel"]>("");
  const [rango, setRango] = useState("24h");
  const [hasta, setHasta] = useState("");
  const [q, setQ] = useState("");
  const [lines, setLines] = useState<LogLine[]>();
  const [next, setNext] = useState<string>();
  const [error, setError] = useState<unknown>();
  const { data: apps } = usePoll(api.apps, [], 0);

  const filtro = (): LogFilter => {
    const hours = RANGES.find((r) => r.id === rango)?.hours ?? 24;
    const until = hasta ? new Date(hasta) : new Date();
    return {
      proceso, app, nivel, q: q.trim(),
      desde: new Date(until.getTime() - hours * 3600_000).toISOString().replace(/\.\d+Z$/, "Z"),
      hasta: hasta ? until.toISOString().replace(/\.\d+Z$/, "Z") : "",
    };
  };

  useEffect(() => {
    const timer = setTimeout(() => {
      api.registro(filtro()).then((p) => { setLines(p.lines); setNext(p.next); setError(undefined); }, setError);
    }, 300);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [proceso, app, nivel, rango, hasta, q]);

  const older = () => {
    if (!next) return;
    api.registro({ ...filtro(), antes: next }).then((p) => { setLines([...(lines ?? []), ...p.lines]); setNext(p.next); }, setError);
  };

  // A process seen in the lines but not in the usual list (a test server) can be picked too.
  const processes = [...new Set([...PROCESSES, ...(lines ?? []).map((l) => l.process).filter(Boolean)])];

  return (
    <>
      <h1>{t("Log")}</h1>
      <p className="sub">
        {t("Every line rendimiento's own processes (api, control, worker, agents, traces) wrote at INFO and above, in one place: a restart no longer loses them. Requests slower than a second and answers that failed (5xx) are noted with their route. Lines are kept for 30 days. Secrets are removed before a line is kept: values that look like tokens, passwords or keys, and every attribute whose name says it holds one.")}{" "}
        <Link to="/problems">{t("Warnings and errors, grouped, are on the Problems page.")}</Link>
      </p>
      <Vigia />
      <div className="rel-filters registro-filtros">
        <label className="small">{t("Process")}{" "}
          <select value={proceso} onChange={(e) => setProceso(e.target.value)}>
            <option value="">{t("Every process")}</option>
            {processes.map((p) => <option key={p} value={p}>{p}</option>)}
          </select>
        </label>
        <label className="small">{t("App")}{" "}
          <select value={app} onChange={(e) => setApp(e.target.value)}>
            <option value="">{t("Every app")}</option>
            {apps?.map((a) => <option key={a.name} value={a.name}>{a.name}</option>)}
          </select>
        </label>
        <label className="small">{t("Level")}{" "}
          <select value={nivel} onChange={(e) => setNivel(e.target.value as LogFilter["nivel"])}>
            <option value="">{t("Everything")}</option>
            <option value="WARN">{t("Warnings and errors")}</option>
            <option value="ERROR">{t("Errors only")}</option>
          </select>
        </label>
        <label className="small">{t("Period")}{" "}
          <select value={rango} onChange={(e) => setRango(e.target.value)}>
            {RANGES.map((r) => <option key={r.id} value={r.id}>{t(r.label)}</option>)}
          </select>
        </label>
        <label className="small">{t("Until")}{" "}
          <input type="datetime-local" value={hasta} onChange={(e) => setHasta(e.target.value)} />
        </label>
        <input type="search" className="registro-buscar" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("Search the messages")} aria-label={t("Search the messages")} />
      </div>
      <ErrorBox error={error} />
      {!lines && !error && <EsqTabla filas={12} columnas={5} />}
      {lines && lines.length === 0 && <div className="card empty"><p>{t("No lines match.")}</p></div>}
      {lines && lines.length > 0 && (
        <div style={{ overflowX: "auto" }}>
          <table className="bitacora small registro-tabla">
            <thead><tr><th>{t("When")}</th><th>{t("Level")}</th><th>{t("Process")}</th><th>{t("App")}</th><th>{t("Message")}</th></tr></thead>
            <tbody>{lines.map((l) => <Linea key={l.id} l={l} />)}</tbody>
          </table>
        </div>
      )}
      {next && <p><button type="button" onClick={older}>{t("Show older lines")}</button></p>}
    </>
  );
}
