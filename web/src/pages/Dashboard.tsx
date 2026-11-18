import { Fragment, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api, envApi, EXAMPLE_URL, isLibrary, timeAgo, type App, type SeccionInicio, session } from "../api";
import { ErrorBox, PhaseBadge, RunBadge, usePoll } from "../components/ui";
import { fmtPct } from "../components/reliability";
import { DeliveryPanel } from "../components/delivery";
import { VisitsPanel } from "../components/visits";
import { Flor, LluviaDePetalos } from "../components/ambiente";
import { Semilla } from "../components/marca";
import { t, tn } from "../i18n";
import { ResumenDelDia, diaCorto, ultimoTablero, useResumen } from "../components/resumen";
import { EsqMosaico } from "../components/esqueleto";
import { HoyAyuda, HoyCorriendo, HoyEsperan, PieDelInicio, necesitanAyuda } from "../components/hoy";
import { Personalizar, ofrecida } from "../components/personalizar";
import { TARJETAS, completar, useInicio } from "../inicio";
import { anotar } from "../uso";

// Each app under an arch gets one of the papel picado colors for its band.
const bandas = ["var(--grana)", "var(--cempasuchil)", "var(--anil)", "var(--rosa)", "var(--nopal)", "var(--maiz)"];

/** An app is withered while one of its checks is in an outage, or its
 *  workloads are failing. */
export function marchita(a: App): boolean {
  return (a.down?.length ?? 0) > 0 || a.status.phase === "Error" || a.status.phase === "Degraded";
}

type Filtro = "todas" | "vivas" | "marchitas" | "fallaron";
const filtros: { id: Filtro; label: string; tone: string; test: (a: App) => boolean }[] = [
  { id: "todas", label: "All of them", tone: "var(--text)", test: () => true },
  { id: "vivas", label: "In bloom", tone: "var(--ok)", test: (a) => !marchita(a) },
  { id: "marchitas", label: "Withered", tone: "var(--bad)", test: marchita },
  { id: "fallaron", label: "Last run failed", tone: "var(--warn)", test: (a) => a.lastRun?.status === "failed" },
];

/** Whether an app matches what was searched: its name, repository or addresses. */
function coincide(a: App, q: string): boolean {
  const s = q.trim().toLowerCase();
  if (!s) return true;
  const urls = (a.status.services ?? []).flatMap((x) => [x.url, x.lanURL]);
  return [a.name, a.repo, ...urls].some((v) => v?.toLowerCase().includes(s));
}

/** The text with what was searched marked. */
function resaltar(text: string, q: string): ReactNode {
  const s = q.trim().toLowerCase();
  const at = s ? text.toLowerCase().indexOf(s) : -1;
  if (at < 0) return text;
  return <>{text.slice(0, at)}<mark>{text.slice(at, at + s.length)}</mark>{text.slice(at + s.length)}</>;
}

/** The cogwheel of Customize home. */
function Engrane() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="12" cy="12" r="3" />
      <path d="M12 2.5v3M12 18.5v3M2.5 12h3M18.5 12h3M5.3 5.3l2.1 2.1M16.6 16.6l2.1 2.1M5.3 18.7l2.1-2.1M16.6 7.4l2.1-2.1" />
    </svg>
  );
}

/**
 * The Apps page, the home: the greeting with the hills, then the sections each person lays out (Customize home): the
 * Today band (Running, Waiting for you, Need help), the mosaic of apps with its filters and search, and, when chosen,
 * yesterday's scoreboard, visits and delivery. Under them one quiet line of links. Only what is urgent stays above
 * whatever was chosen: the environment's warning. The rest of the notices are in the bell.
 */
export function Dashboard() {
  const { data: apps, error } = usePoll(api.apps, [], 10000);
  const nav = useNavigate();
  // The environment report is the cluster's: a computer has none.
  const { data: env } = usePoll(() => (session.cluster ? envApi.report() : Promise.resolve(undefined)), [], 60000);
  const resumen = useResumen();
  const ultimo = ultimoTablero(resumen.data);
  const inicio = useInicio();
  // While Customize home is open, the page shows what is being chosen.
  const [borrador, setBorrador] = useState<SeccionInicio[]>();
  const secciones = (borrador ?? inicio.secciones).filter((s) => ofrecida(s.id));
  const on = (id: string) => secciones.some((s) => s.id === id && s.on);
  // The scoreboard: folded into a line of the greeting, unless chosen unfolded.
  const [marcadorAbierto, setMarcadorAbierto] = useState<boolean>();
  const abierto = marcadorAbierto ?? on("marcador");

  // Petals fall when an app's live release changes while the page is open.
  const [lluvia, setLluvia] = useState(0);
  const releases = useRef<Map<string, number> | undefined>(undefined);
  useEffect(() => {
    if (!apps) return;
    const now = new Map(apps.map((a) => [a.name, a.status.release ?? 0]));
    const before = releases.current;
    if (before && apps.some((a) => (a.status.release ?? 0) > (before.get(a.name) ?? Infinity))) setLluvia(Date.now());
    releases.current = now;
  }, [apps]);

  const caidas = apps?.filter(marchita).length ?? 0;
  const vivas = (apps?.length ?? 0) - caidas;

  // The search (the mosaic's; the top bar's goes anywhere) and the filter live in the address (?q=…&ver=…), so a view can be shared and survives a reload.
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const ver = (filtros.find((f) => f.id === params.get("ver"))?.id ?? "todas") as Filtro;
  const poner = (k: string, v: string) => setParams((p) => { if (v) p.set(k, v); else p.delete(k); return p; }, { replace: true });
  const filtro = filtros.find((f) => f.id === ver)!;
  // Withered first, then by name; each keeps the band color of its place.
  const shown = (apps ?? []).map((a, i) => ({ a, i }))
    .filter(({ a }) => coincide(a, q) && filtro.test(a))
    .sort((x, y) => Number(marchita(y.a)) - Number(marchita(x.a)) || x.a.name.localeCompare(y.a.name));
  const hay = !!apps && apps.length > 0;

  const abrirAjustes = () => {
    if (borrador) return setBorrador(undefined);
    anotar("customize_home");
    setBorrador(completar(inicio.secciones));
  };
  const marcador = (plegable: boolean) => (
    <ResumenDelDia resumen={resumen} plegar={plegable ? () => setMarcadorAbierto(false) : undefined} />
  );

  const mosaico = (
    <>
      {hay && (
        <>
          <div className="mosaico-cuentas" role="group" aria-label={t("Show")}>
            {filtros.map((f) => (
              <button key={f.id} className="mosaico-cuenta" aria-pressed={ver === f.id} onClick={() => poner("ver", f.id === "todas" ? "" : f.id)}>
                <span>{t(f.label)}</span>
                <b style={{ color: f.tone }}>{apps.filter((a) => coincide(a, q) && f.test(a)).length}</b>
              </button>
            ))}
          </div>
          <label className="mosaico-buscar">
            <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5" /><path d="M15.5 15.5 21 21" /></svg>
            <input type="search" value={q} onChange={(e) => poner("q", e.target.value)} onKeyDown={(e) => { if (e.key === "Escape") poner("q", ""); }}
              placeholder={t("Search by name, repository or address")} aria-label={t("Search apps")} />
          </label>
          {shown.length === 0 && (
            <div className="card empty">
              <p>{q ? t("No app matches “{q}”.", { q }) : t("No app here.")}</p>
              <button onClick={() => setParams({}, { replace: true })}>{t("Show them all")}</button>
            </div>
          )}
        </>
      )}
      {/* The key replays the entrance when the view changes. */}
      <div className="mosaico" key={`${ver}|${q}`}>
        {shown.map(({ a, i }, n) => {
          const url = (a.status.services ?? []).map((s) => s.url ?? s.lanURL).find((u): u is string => !!u);
          const caida = marchita(a);
          const banda = bandas[i % bandas.length];
          const lib = isLibrary(a.spec);
          const estado = lib ? t("A library: tested on every push") : caida
            ? (a.down?.length ? t("Withered: {checks}", { checks: a.down.join(", ") }) : t("Withered: its workloads are failing"))
            : a.uptime24h !== undefined
              ? t("Blooming · {pct} available", { pct: fmtPct(Math.round(a.uptime24h * 100000), 100000) })
              : t("Blooming");
          return (
            <article key={a.id} className={`teja${caida ? " caida" : ""}`} style={{ ["--banda" as string]: banda, ["--i" as string]: Math.min(n, 12) }}
              onClick={() => nav(`/apps/${a.name}`)}>
              <div className="teja-picado" aria-hidden="true" />
              <div className="teja-cuerpo">
                <div className="teja-nombre">
                  <Flor estado={!a.status.release ? "brote" : caida ? "marchita" : "viva"} size={30} label={!a.status.release ? t("A bud: not live yet") : caida ? t("Withered flower") : t("Blooming flower")} />
                  <h3><Link to={`/apps/${a.name}`} onClick={(e) => e.stopPropagation()}>{resaltar(a.name, q)}</Link></h3>
                </div>
                <div className={`teja-estado ${caida ? "mal" : "bien"}`} title={t("Share of successful uptime checks in the last 24 hours")}>{estado}</div>
                {url && <a className="teja-url small" href={url} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()}>{resaltar(url.replace(/^https?:\/\//, ""), q)}</a>}
                {a.lastRun && (
                  <div className="row small muted" title={`${a.lastRun.branch} · ${a.lastRun.sha.slice(0, 7)}`}>
                    <RunBadge status={a.lastRun.status} />
                    <span>{timeAgo(a.lastRun.createdAt)}</span>
                  </div>
                )}
                {a.status.phase !== "Healthy" && !caida && !lib && <PhaseBadge status={a.status} />}
              </div>
            </article>
          );
        })}
      </div>
    </>
  );

  // Each section chosen, in its order; the band's cards side by side while they come one after another.
  const ayuda = apps ? necesitanAyuda(apps, ultimo?.board, marchita) : undefined;
  const tarjeta: Record<string, () => ReactNode> = {
    corriendo: () => <HoyCorriendo />,
    esperan: () => <HoyEsperan />,
    ayuda: () => <HoyAyuda ayuda={ayuda} />,
  };
  const bloques: ReactNode[] = [];
  let franja: ReactNode[] = [];
  const cerrarFranja = () => {
    if (franja.length) bloques.push(<div key={`hoy-${bloques.length}`} className="hoy" style={{ ["--n" as string]: franja.length }}>{franja}</div>);
    franja = [];
  };
  for (const s of secciones) {
    if (!s.on) continue;
    if (TARJETAS.has(s.id)) {
      if (hay) franja.push(<Fragment key={s.id}>{tarjeta[s.id]()}</Fragment>);
      continue;
    }
    cerrarFranja();
    if (s.id === "apps") bloques.push(<Fragment key="apps">{mosaico}</Fragment>);
    else if (s.id === "marcador" && hay && abierto) bloques.push(<Fragment key="marcador">{marcador(false)}</Fragment>);
    else if (s.id === "visitas" && hay) bloques.push(<VisitsPanel key="visitas" />);
    else if (s.id === "entregas" && hay) bloques.push(<DeliveryPanel key="entregas" />);
  }
  cerrarFranja();

  return (
    <>
      {on("lluvia") && <LluviaDePetalos cuando={lluvia} />}
      <div className="hero-vida">
        <div>
          <h1>{t("Your apps,")} <em>{t("alive")}</em>.</h1>
          <p className="sub inicio-sub" style={{ marginBottom: 0 }} data-listo={apps ? "" : undefined}>
            {apps && (caidas > 0
              ? `${tn(vivas, "{n} blooming", "{n} blooming")} · ${tn(caidas, "{n} withered", "{n} withered")}`
              : tn(vivas, "{n} app blooming on the internet", "{n} apps blooming on the internet"))}
            {hay && ultimo && (ultimo.board.runs > 0 || ultimo.board.releases > 0) && (
              <>
                {" · "}
                <button type="button" className="link inicio-ayer" aria-expanded={abierto} aria-controls="marcador-del-dia" onClick={() => setMarcadorAbierto(!abierto)}>
                  {diaCorto(ultimo.day)}: {tn(ultimo.board.runs, "{n} run", "{n} runs")}, {tn(ultimo.board.failed, "{n} failed", "{n} of them failed")} <span aria-hidden="true">{abierto ? "▾" : "▸"}</span>
                </button>
              </>
            )}
          </p>
          {!session.demo && (
            <button type="button" className="link inicio-personalizar" aria-expanded={!!borrador} aria-controls="inicio-ajustes" onClick={abrirAjustes}>
              <Engrane />{t("Customize home")}
            </button>
          )}
        </div>
        {/* The hills of Cotija: the ridge, the ground below it and the sun, drawn in one after another. */}
        <svg className="cerros" width="300" height="45" viewBox="0 0 600 90" aria-hidden="true" fill="none" strokeWidth="5" strokeLinecap="round" strokeLinejoin="round">
          <path pathLength={1} style={{ "--i": 0 } as CSSProperties} d="M0 72L70 46l50 16 70-40 70 38 60-26 80 30 70-36 60 24 70-10" />
          <path pathLength={1} style={{ "--i": 1 } as CSSProperties} className="cerros-suelo" d="M0 84q150-14 300 0t300 0" />
          <circle pathLength={1} style={{ "--i": 2 } as CSSProperties} cx="330" cy="22" r="10" />
          <path pathLength={1} style={{ "--i": 3 } as CSSProperties} d="M330 4v5M312 22h-5M353 22h-5M317 9l3.5 3.5M343 9l-3.5 3.5" />
        </svg>
      </div>
      {borrador && (
        <Personalizar lista={borrador} poner={setBorrador} vista={inicio.vista} guardado={inicio.aplicar} cerrar={() => setBorrador(undefined)} />
      )}
      <ErrorBox error={error} />
      {env && env.overall !== "ok" && (
        <Link to="/environment" className={`card env-hero ${env.overall === "warning" ? "warn" : "bad"}`} style={{ marginBottom: 16, color: "var(--text)" }}>
          <span className={`badge ${env.overall === "warning" ? "warn" : "bad"}`}>{t("Environment")}</span>
          <span>{env.summary}</span>
          <span className="small" style={{ color: "var(--link)" }}>{t("View details →")}</span>
        </Link>
      )}
      {/* Opened from the greeting's line while it is not one of the chosen sections: right under it. */}
      {hay && abierto && !on("marcador") && marcador(true)}
      {apps && apps.length === 0 && (
        <div className="card empty">
          <p>{t("No apps yet.")}</p>
          <Link to="/new" className="btn primary big"><Semilla />{t("Deploy your first app")}</Link>
          <p className="small muted" style={{ margin: "12px 0 0" }}>{t("Nothing of your own to deploy yet?")} <a href={EXAMPLE_URL} target="_blank" rel="noreferrer">{t("Copy the example to your GitHub account")}</a>{t(", then choose your copy in New app.")}</p>
        </div>
      )}
      {!apps && !error ? <EsqMosaico cabeza={false} franja /> : bloques}
      {hay && <PieDelInicio board={ultimo?.board} abrirMarcador={() => { setMarcadorAbierto(true); requestAnimationFrame(() => document.getElementById("marcador-del-dia")?.scrollIntoView({ block: "nearest" })); }} />}
    </>
  );
}
