// "Running now", at the top of the Apps page: a card for each run queued or
// running, with its app, its branch, its steps as small bars, the step at
// hand and how long it has run, and the way to the run. With nothing
// running it is one line: all quiet, and the last thing that happened.
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, duration, timeAgo, type RunNow, type Step } from "../api";
import { t } from "../i18n";
import { usePoll } from "./ui";

/** The steps in the order they run: by how deep each is in what it waits for, then by name. */
export function enOrden(steps: Step[]): Step[] {
  const por = new Map(steps.map((s) => [s.id, s]));
  const hondo = new Map<string, number>();
  const de = (id: string, camino: Set<string>): number => {
    const ya = hondo.get(id);
    if (ya !== undefined) return ya;
    if (camino.has(id)) return 0; // a loop would be the plan's mistake; it does not hang the page
    camino.add(id);
    const deps = (por.get(id)?.dependsOn ?? []).filter((d) => por.has(d));
    const v = deps.length ? 1 + Math.max(...deps.map((d) => de(d, camino))) : 0;
    hondo.set(id, v);
    return v;
  };
  return [...steps].sort((a, b) => de(a.id, new Set()) - de(b.id, new Set()) || a.id.localeCompare(b.id));
}

const barra: Record<Step["status"], string> = { succeeded: "ok", reused: "ok", running: "va", failed: "mal", skipped: "salto", pending: "" };
const estado: Record<Step["status"], string> = { succeeded: "passed", reused: "reused", running: "running", failed: "failed", skipped: "skipped", pending: "pending" };

function Carril({ run }: { run: RunNow }) {
  const pasos = enOrden(run.steps ?? []);
  const ahora = pasos.filter((s) => s.status === "running");
  const desde = ahora.map((s) => s.startedAt).filter((x): x is string => !!x).sort()[0] ?? run.startedAt ?? run.createdAt;
  const hechos = pasos.filter((s) => s.status !== "pending" && s.status !== "running").length;
  return (
    <li className={`ahora-carril${run.status === "queued" ? " cola" : ""}`}>
      <div className="ahora-arriba">
        <i className="ahora-punto" aria-hidden="true" />
        <Link to={`/apps/${run.app}`}>{run.app}</Link>
      </div>
      <div className="ahora-rama mono" title={run.branch}>{run.branch}{run.deploy ? ` · ${t("to deploy")}` : ""}</div>
      {pasos.length > 0 && (
        <div className="ahora-pasos" role="img" aria-label={t("{done} of {n} steps done", { done: hechos, n: pasos.length })}>
          {pasos.map((s) => <i key={s.id} className={barra[s.status] ?? ""} title={`${s.id}: ${t(estado[s.status] ?? s.status)}`} />)}
        </div>
      )}
      <div className="ahora-paso">
        <span>{ahora.length
          ? <>{t("Now:")} <b>{ahora[0].id}</b>{ahora.length > 1 ? ` +${ahora.length - 1}` : ""}</>
          : run.status === "queued" ? t("Waiting for its turn") : t("Between steps")}</span>
        <span>{duration(desde)}</span>
      </div>
      <Link className="ahora-ir" to={`/apps/${run.app}/runs/${run.id}`}>{t("See run {n} →", { n: run.id })}</Link>
    </li>
  );
}

/** What ended last, in words. */
function loUltimo(r: RunNow): string {
  return r.status === "succeeded" ? t("{app}: run {n} passed", { app: r.app, n: r.id })
    : r.status === "failed" ? t("{app}: run {n} failed", { app: r.app, n: r.id })
      : t("{app}: run {n} was cancelled", { app: r.app, n: r.id });
}

/** The strip: it reads again every few seconds while something runs, more calmly while all is quiet. */
export function Corriendo() {
  const [vivo, setVivo] = useState(false);
  const { data } = usePoll(api.corriendo, [vivo], vivo ? 5000 : 15000);
  useEffect(() => { if (data) setVivo(data.ahora.length > 0); }, [data]);
  if (!data) return null;
  if (data.ahora.length === 0) {
    const ultimo = data.recientes[0];
    return (
      <p className="ahora-calma" data-corriendo="0">
        <i aria-hidden="true" />
        <span>{t("All quiet.")}</span>
        {ultimo && (
          <span className="muted">
            {t("Last:")} <Link to={`/apps/${ultimo.app}/runs/${ultimo.id}`}>{loUltimo(ultimo)}</Link> · {timeAgo(ultimo.finishedAt ?? ultimo.createdAt)}
          </span>
        )}
      </p>
    );
  }
  return (
    <section className="ahora" aria-labelledby="ahora-titulo" data-corriendo={data.ahora.length}>
      <header>
        <h2 id="ahora-titulo">{t("Running now · {n}", { n: data.ahora.length })}</h2>
        <small className="muted">{t("updates by itself")}</small>
      </header>
      <ul className="ahora-carriles">
        {data.ahora.map((r) => <Carril key={r.id} run={r} />)}
      </ul>
    </section>
  );
}
