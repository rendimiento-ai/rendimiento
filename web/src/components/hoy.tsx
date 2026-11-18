// The Today band at the top of the home page: three cards of the same size,
// Running, Waiting for you and Need help, each a count and a few lines that
// lead where the thing is. On a phone they stack. Then, under the mosaic, one
// quiet line of links to what used to fill the page's foot.
import { useEffect, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { api, duration, session, timeAgo, type App, type DayBoard, type RunNow, type Step } from "../api";
import { t, tn } from "../i18n";
import { usePoll } from "./ui";
import { Hueso } from "./esqueleto";

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

/** How many lines a card shows; the rest are counted. */
const LINEAS = 3;

/** A card of the band: its title with its count, and what is under it (bones while it loads). */
function Tarjeta({ id, titulo, cifra, tono, children }: { id: string; titulo: string; cifra?: number; tono?: "mal" | "va"; children: ReactNode }) {
  return (
    <section className={`card hoy-tarjeta hoy-${id}`} aria-labelledby={`hoy-${id}-t`} data-cuenta={cifra}>
      <header>
        <h2 id={`hoy-${id}-t`}>{titulo}</h2>
        {cifra !== undefined && <b className={`hoy-cifra${cifra > 0 && tono ? ` ${tono}` : ""}`}>{cifra}</b>}
      </header>
      {children}
    </section>
  );
}

function Huesos() {
  return <div aria-hidden="true">{[86, 64].map((w) => <Hueso key={w} className="esq-renglon" w={`${w}%`} />)}</div>;
}

function Mas({ n }: { n: number }) {
  return n > 0 ? <p className="hoy-mas small muted">{tn(n, "and {n} more", "and {n} more")}</p> : null;
}

/** What ended last, in words. */
function loUltimo(r: RunNow): string {
  return r.status === "succeeded" ? t("{app}: run {n} passed", { app: r.app, n: r.id })
    : r.status === "failed" ? t("{app}: run {n} failed", { app: r.app, n: r.id })
      : t("{app}: run {n} was cancelled", { app: r.app, n: r.id });
}

/** One run going on: its app, the step at hand and for how long, as a way to the run. */
function Carril({ run }: { run: RunNow }) {
  const pasos = enOrden(run.steps ?? []);
  const ahora = pasos.filter((s) => s.status === "running");
  const desde = ahora.map((s) => s.startedAt).filter((x): x is string => !!x).sort()[0] ?? run.startedAt ?? run.createdAt;
  return (
    <li className={`hoy-corre${run.status === "queued" ? " cola" : ""}`}>
      <Link to={`/apps/${run.app}/runs/${run.id}`} title={t("See run {n} →", { n: run.id })}>
        <i className="ahora-punto" aria-hidden="true" />
        <b>{run.app}</b>
        <span className="hoy-paso">{ahora.length
          ? <><span className="mono">{ahora[0].id}</span>{ahora.length > 1 ? ` +${ahora.length - 1}` : ""}</>
          : run.status === "queued" ? t("Waiting for its turn") : t("Between steps")}</span>
        <span className="hoy-tiempo">{duration(desde)}</span>
      </Link>
    </li>
  );
}

/** Running: the runs queued or running, read again every few seconds while there are some, more calmly while all is quiet. */
export function HoyCorriendo() {
  const [vivo, setVivo] = useState(false);
  const { data } = usePoll(api.corriendo, [vivo], vivo ? 5000 : 15000);
  useEffect(() => { if (data) setVivo(data.ahora.length > 0); }, [data]);
  const n = data?.ahora.length;
  const ultimo = data?.recientes[0];
  return (
    <Tarjeta id="corriendo" titulo={t("Running")} cifra={n} tono="va">
      {!data ? <Huesos /> : n === 0 ? (
        <p className="ahora-calma">
          <i aria-hidden="true" />
          <span>{t("All quiet.")}</span>
          {ultimo && (
            <span className="muted">
              {t("Last:")} <Link to={`/apps/${ultimo.app}/runs/${ultimo.id}`}>{loUltimo(ultimo)}</Link> · {timeAgo(ultimo.finishedAt ?? ultimo.createdAt)}
            </span>
          )}
        </p>
      ) : (
        <>
          <ul className="hoy-lineas">{data.ahora.slice(0, LINEAS).map((r) => <Carril key={r.id} run={r} />)}</ul>
          <Mas n={data.ahora.length - LINEAS} />
        </>
      )}
    </Tarjeta>
  );
}

/** Waiting for you: what each app's work board has on your turn, each a door to that board. */
export function HoyEsperan() {
  const { data } = usePoll(() => api.mesaCounts().catch(() => []), [], 60000);
  const apps = (data ?? []).filter((c) => c.you > 0).sort((a, b) => b.you - a.you || a.app.localeCompare(b.app));
  const total = apps.reduce((n, c) => n + c.you, 0);
  return (
    <Tarjeta id="esperan" titulo={t("Waiting for you")} cifra={data ? total : undefined} tono="mal">
      {!data ? <Huesos /> : total === 0 ? <p className="hoy-nada">{t("Nothing waits for you.")}</p> : (
        <>
          <ul className="hoy-lineas">
            {apps.slice(0, LINEAS).map((c) => (
              <li key={c.app}>
                <Link to={`/apps/${c.app}/mesa`}><b>{c.app}</b><span className="hoy-cuenta">{c.you}</span></Link>
              </li>
            ))}
          </ul>
          <Mas n={apps.length - LINEAS} />
        </>
      )}
    </Tarjeta>
  );
}

/** An app that needs help, and why: in the order a person would look. */
interface Ayuda { app: string; por: string; a: string }

/** The apps that need a hand: withered (a check down, or failing workloads), their last release failed, or none of yesterday's runs passed. */
export function necesitanAyuda(apps: App[], board: DayBoard | undefined, marchita: (a: App) => boolean): Ayuda[] {
  const out: Ayuda[] = [];
  const ya = new Set<string>();
  const poner = (x: Ayuda) => { if (!ya.has(x.app)) { ya.add(x.app); out.push(x); } };
  for (const a of apps) {
    if (marchita(a)) poner({ app: a.name, por: a.down?.length ? t("down: {checks}", { checks: a.down.join(", ") }) : t("its workloads are failing"), a: `/apps/${a.name}` });
  }
  for (const a of apps) {
    if (a.lastRun?.deploy && a.lastRun.status === "failed") poner({ app: a.name, por: t("its last release failed"), a: `/apps/${a.name}/runs/${a.lastRun.id}` });
  }
  const conocidas = new Set(apps.map((a) => a.name));
  for (const d of board?.apps ?? []) {
    if (d.runs > 0 && d.failed === d.runs && conocidas.has(d.app)) poner({ app: d.app, por: t("0% of its runs passed yesterday"), a: `/apps/${d.app}/runs` });
  }
  return out;
}

/** Need help: the apps in trouble, each with its reason and a way to it. */
export function HoyAyuda({ ayuda }: { ayuda?: Ayuda[] }) {
  return (
    <Tarjeta id="ayuda" titulo={t("Need help")} cifra={ayuda?.length} tono="mal">
      {!ayuda ? <Huesos /> : ayuda.length === 0 ? <p className="ahora-calma"><i aria-hidden="true" /><span>{t("Every app is well.")}</span></p> : (
        <>
          <ul className="hoy-lineas">
            {ayuda.slice(0, LINEAS).map((x) => (
              <li key={x.app}>
                <Link to={x.a}><i className="hoy-mal" aria-hidden="true" /><b>{x.app}</b><span className="hoy-paso">{x.por}</span></Link>
              </li>
            ))}
          </ul>
          <Mas n={ayuda.length - LINEAS} />
        </>
      )}
    </Tarjeta>
  );
}

/** The quiet line under the mosaic: the week's visitors (to Numbers), yesterday's releases (opens the scoreboard), the Mercado. */
export function PieDelInicio({ board, abrirMarcador }: { board?: DayBoard; abrirMarcador: () => void }) {
  const { data: visitas } = usePoll(() => api.visitsSummary().catch(() => undefined), [], 300000);
  const v = visitas?.configured ? visitas : undefined;
  return (
    <nav className="inicio-pie small" aria-label={t("More numbers")}>
      <Link to="/numeros">
        {v ? tn(v.current.visitors, "{n} visitor in the last {days} days", "{n} visitors in the last {days} days", { days: v.days }) : t("Numbers")} ▸
      </Link>
      {board && board.releases > 0 && (
        <button type="button" className="link" onClick={abrirMarcador}>
          {t("{n} of {total} releases healthy", { n: board.healthy, total: board.releases })} ▸
        </button>
      )}
      {session.operator && session.mercado && (
        <Link to="/mercado" title={t("Pages without code for people who don't program live in the Mercado.")}>{t("Mercado")} ▸</Link>
      )}
    </nav>
  );
}
