import { useEffect, useState, type ReactNode } from "react";
import { api, envApi, session, timeAgo, type BoundaryCheck, type Capacity, type EnvCheck, type EnvNode, type EnvProblem, type EnvProvider, type EnvReport, type EnvStatus, type LiveSetting, type SettingsView } from "../api";
import { Link } from "react-router-dom";
import { ErrorBox, usePoll } from "../components/ui";
import { outsideWhy } from "../components/barda";
import { lang, locale, t, tn } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "../components/conexion-aviso";
import { AvisoDeploy, BotonActualizar, Publicada, useActualizar } from "./Version";
import { EsqCosa, Esqueleto, Hueso } from "../components/esqueleto";
import { Forjas } from "../components/forjas";

const tone: Record<EnvStatus, string> = { ok: "ok", warning: "warn", missing: "bad", error: "bad" };
const label: Record<EnvStatus, string> = { ok: "OK", warning: "Attention", missing: "Missing", error: "Error" }; // translated where shown
const categories = ["Cluster", "Networking", "TLS", "Build", "Integrations"];
const rank: Record<EnvStatus, number> = { ok: 0, warning: 1, missing: 2, error: 3 };

/** A check's status as a colored label: OK, Attention, Missing or Error. */
export function StatusBadge({ status }: { status: EnvStatus }) {
  return <span className={`badge ${tone[status]}`}>{t(label[status])}</span>;
}

/** The worse of some statuses (ok when there are none). */
function peor(...s: (EnvStatus | undefined)[]): EnvStatus {
  return s.reduce<EnvStatus>((a, b) => (b && rank[b] > rank[a] ? b : a), "ok");
}

/** Days from now until an instant (negative: it is past). */
const diasHasta = (when: string) => Math.floor((new Date(when).getTime() - Date.now()) / 86400000);

/** A key expires within the 30 days the page warns about (llaves.Aviso). */
const venceProximo = (when?: string) => !!when && diasHasta(when) < 30;

/** The Environment page: where the apps live and what the platform connects to, or, on one computer, its version and where its settings are. */
export function Environment() {
  return session.cluster ? <Entorno /> : <EnEstaComputadora />;
}

/** The book's Settings page, at the part about one computer, in the reader's language. */
const LIBRO_AJUSTES = lang === "es" ? "https://aprende.rendimiento.ai/reference/config/#on-one-computer" : "https://learn.rendimiento.ai/reference/config/#on-one-computer";

/**
 * The Environment page on one computer, which has no cluster to draw: the Version card (with its switch for installing fix
 * releases) and where the settings are kept, since they change only by editing a file and restarting.
 */
function EnEstaComputadora() {
  const { data, error } = usePoll(() => envApi.published(), [], 30 * 60000);
  if (error) return <ErrorBox error={error} />;
  if (!data) return <EsqCosa />;
  const a = data.settings;
  return (
    <>
      <div>
        <h1 data-listo="">{t("Environment")}</h1>
        <p className="sub" style={{ marginBottom: 0 }}>{t("This computer, where your apps live: its version and its settings.")}</p>
      </div>
      <div style={{ marginTop: 16 }}><Publicada /></div>
      {session.operator && <Forjas />}
      <section className="card stack ajustes">
        <div>
          <h2 style={{ margin: 0 }}>{t("Platform settings")}</h2>
          <p className="small muted" style={{ margin: 0 }}>{t("rendimiento reads its settings from environment variables when it starts.")}{" "}
            <a href={LIBRO_AJUSTES} target="_blank" rel="noreferrer">{t("Every one is in the book")}</a></p>
        </div>
        {a ? (
          <>
            <span>{a.inside ? t("On this computer they are in this file, inside the WSL distribution (open it with {cmd}):", { cmd: a.inside }) : t("On this computer they are in this file:")}</span>
            <pre className="file">{a.path}</pre>
            <span className="small">{t("One NAME=value per line; a value there wins over the service's own. After a change, restart rendimiento so it reads it:")}</span>
            <pre className="file">{a.restart}</pre>
          </>
        ) : (
          <span className="small">{t("This rendimiento was not started by its installer, so its settings are the environment it was started with: in a container, its -e or --env-file options; by hand, the variables before the command. Change them there and start it again.")}</span>
        )}
      </section>
    </>
  );
}

const picado = ["var(--rosa)", "var(--cempasuchil)", "var(--maiz)", "var(--nopal)", "#1F8A8A", "var(--anil)", "#7B2F8E"];

/** The papel picado hung over the page; it sways a little, and stands still when movement is off. */
export function PapelPicado({ n = 12 }: { n?: number }) {
  return (
    <div className="entorno-picado" aria-hidden="true">
      {Array.from({ length: n }, (_, i) => <i key={i} style={{ background: picado[i % picado.length], ["--d" as string]: `${(i * -0.37) % 2}s`, ["--s" as string]: `${3 + ((i * 7) % 9) / 10}s` }} />)}
    </div>
  );
}

/**
 * The Environment page of a cluster: how it goes, what needs the person, the nodes as small houses, and the services the
 * platform connects to. What the old page had stays reachable: each node's details, the capacity, the dynamic DNS, the test
 * email, every check, and the live settings for administrators (folded).
 */
function Entorno() {
  const [refreshing, setRefreshing] = useState(false);
  const { data: r, error, setData } = usePoll(() => envApi.report(), [], 30000);
  const { data: apps } = usePoll(() => api.apps().catch(() => undefined), [], 60000);
  const version = useActualizar();
  const [sel, setSel] = useState<string>();
  const [abierto, setAbierto] = useState<string>();

  const refresh = async () => {
    setRefreshing(true);
    try {
      setData(await envApi.report(true));
    } finally {
      setRefreshing(false);
    }
  };

  if (error) return <ErrorBox error={error} />;
  if (!r) return <EsqCosa acciones cuerpo="calle" />;
  const problems = r.problems ?? [];
  const nodo = r.nodes.find((n) => n.name === sel);
  const malos = r.checks.filter((c) => c.status !== "ok");
  const caidos = malos.filter((c) => c.status === "error" || (c.status === "missing" && c.required)).length + r.nodes.filter((n) => !n.ready).length;
  const avisos = malos.length - malos.filter((c) => c.status === "error" || (c.status === "missing" && c.required)).length + problems.length;
  const vst = version.data?.status;

  return (
    <div className="entorno">
      <PapelPicado />
      <div className="entorno-cabeza">
        <div>
          <h1 data-listo="">{t("Environment")}</h1>
          <p className="sub" style={{ margin: 0 }}>{t("Where your apps live, what rendimiento connects to, and what it needs from you.")}</p>
        </div>
        <div className="row small muted entorno-acciones">
          <span>{t("checked {when}", { when: timeAgo(r.generatedAt) })}</span>
          <button className="pildora" onClick={refresh} disabled={refreshing}>{refreshing ? t("Checking…") : t("Check again")}</button>
          <BotonActualizar />
        </div>
      </div>

      <section className="entorno-resumen" aria-label={t("Summary")}>
        <ComoVa overall={r.overall} caidos={caidos} avisos={avisos} summary={r.summary} />
        <Cifra color="var(--cempasuchil)" titulo={t("Nodes")} valor={t("{ready} of {n}", { ready: r.cluster.nodesReady, n: r.cluster.nodes })}
          pie={`${r.cluster.name} ${r.cluster.version} · ${tn(r.cluster.pods, "{n} pod", "{n} pods")}`} />
        <Cifra color="var(--nopal)" titulo={t("Apps")}
          valor={apps ? tn(apps.filter((a) => a.status?.phase === "Healthy").length, "{n} online", "{n} online") : "—"}
          pie={apps ? `${tn(apps.length, "{n} app", "{n} apps")} · ${tn((r.cluster.certificates ?? 0) - (r.cluster.certificatesBad ?? 0), "{n} valid certificate", "{n} valid certificates")}` : ""} />
        <Cifra color="var(--anil)" titulo={t("Platform")}
          valor={vst?.current ? t("release #{n}", { n: vst.current.release }) : vst?.running ? vst.running.slice(0, 12) : "rendimiento"}
          pie={vst ? <>{vst.available ? t("A newer one is ready") : t("Up to date")} · <Link to={`/apps/${vst.app}/releases`}>{t("Releases")}</Link></> : ""} />
      </section>

      {/* The version shows once, in the header's button and the Platform figure; below it only what Update cannot do. */}
      <AvisoDeploy />
      <Publicada />

      <Necesita checks={malos} problems={problems} onAbrir={setAbierto} />

      <section className="stack-sm" aria-labelledby="entorno-nodos">
        <h2 id="entorno-nodos" className="entorno-h2">{t("The nodes")}</h2>
        <div className="casitas">
          {r.nodes.map((n, i) => <Casita key={n.name} n={n} i={i} on={sel === n.name} onPick={() => setSel(sel === n.name ? undefined : n.name)} />)}
        </div>
        <p className="small muted" style={{ margin: 0 }}>{t("Lit windows are the processor; the pink skirting rises with memory. A note floats over a node that is missing something; the one with the clock tower is in charge. Press one to see it.")}</p>
        {nodo && <div className="calle-nodo"><NodeCard n={nodo} /></div>}
      </section>

      <Conexiones r={r} abierto={abierto} onAbrir={setAbierto} onSynced={refresh} />

      <details className="entorno-plegable">
        <summary>{t("Capacity")}</summary>
        <Capacidad nodes={r.nodes} />
      </details>
      <details className="entorno-plegable">
        <summary>{t("Every check")} <span className="small muted">· {r.summary}</span></summary>
        <div className="card" style={{ padding: 0, marginTop: 12 }}>
          {categories.map((cat) => {
            const items = r.checks.filter((c) => c.category === cat);
            if (items.length === 0) return null;
            return (
              <div key={cat}>
                <div className="env-cat">{t(cat)}</div>
                {items.map((c) => <CheckRow key={c.id} c={c} />)}
              </div>
            );
          })}
        </div>
      </details>
      {session.operator && (
        <details className="entorno-plegable">
          <summary>{t("Live settings")}</summary>
          <Ajustes />
        </details>
      )}
    </div>
  );
}

/** «How it goes»: the sun turns slowly; its face and the words follow the overall status. */
function ComoVa({ overall, caidos, avisos, summary }: { overall: EnvStatus; caidos: number; avisos: number; summary: string }) {
  const titulo = caidos > 0 ? t("Something is down") : overall !== "ok" || avisos > 0 ? t("Running, with notices") : t("Everything running");
  const boca = caidos > 0 ? "M27 39q5 -4 10 0" : avisos > 0 ? "M27 37h10" : "M27 37q5 4 10 0";
  return (
    <div className={`como-va ${caidos > 0 ? "bad" : avisos > 0 ? "warn" : "ok"}`} title={summary}>
      <svg width="64" height="64" viewBox="0 0 64 64" aria-hidden="true" className="sol">
        <circle cx="32" cy="32" r="30" fill="#F2C14E" />
        <g className="sol-rayos"><path d="M32 6v8M32 50v8M6 32h8M50 32h8M13.6 13.6l5.7 5.7M44.7 44.7l5.7 5.7M13.6 50.4l5.7-5.7M44.7 19.3l5.7-5.7" stroke="#E8890C" strokeWidth="4" strokeLinecap="round" /></g>
        <circle cx="32" cy="32" r="13" fill="#FFFDF8" />
        <path d="M26 31a2 2 0 0 0 4 0M34 31a2 2 0 0 0 4 0" stroke="#8E1B2C" strokeWidth="2.2" fill="none" strokeLinecap="round" />
        <path d={boca} stroke="#8E1B2C" strokeWidth="2.2" fill="none" strokeLinecap="round" />
      </svg>
      <span className="como-va-texto">
        <span className="como-va-titulo">{t("How it goes")}</span>
        <strong>{titulo}</strong>
        <span className="chips">
          <span className={`chip ${caidos ? "bad" : ""}`}>{tn(caidos, "{n} is down", "{n} are down")}</span>
          <span className={`chip ${avisos ? "warn" : ""}`}>{tn(avisos, "{n} notice", "{n} notices")}</span>
        </span>
      </span>
    </div>
  );
}

/** One number of the summary, with a colored top. */
function Cifra({ color, titulo, valor, pie }: { color: string; titulo: string; valor: string; pie: ReactNode }) {
  return (
    <div className="cifra" style={{ ["--c" as string]: color }}>
      <span className="cifra-titulo">{titulo}</span>
      <strong>{valor}</strong>
      <span className="small">{pie}</span>
    </div>
  );
}

export type Dibujo = "codigo" | "nube" | "candado" | "sobre" | "casa" | "cajas" | "respaldo" | "chispa" | "nodo" | "disco" | "red" | "base" | "archivo" | "grafica" | "escudo" | "engrane" | "reloj";

/** An illustration of a service or a requirement: a white drawing on a colored square. */
export function Icono({ d, color, size = 44, redondo }: { d: Dibujo; color: string; size?: number; redondo?: boolean }) {
  const w = "#FFFDF8";
  const trazos: Record<Dibujo, ReactNode> = {
    codigo: <><circle cx="15" cy="13" r="4" fill="none" stroke="#F2C14E" strokeWidth="2.5" /><circle cx="15" cy="31" r="4" fill="none" stroke="#F2C14E" strokeWidth="2.5" /><circle cx="30" cy="20" r="4" fill="none" stroke="#F2C14E" strokeWidth="2.5" /><path d="M15 17v10M30 24c0 5-8 3-12 6" stroke="#F2C14E" strokeWidth="2.5" fill="none" /></>,
    nube: <path d="M11 29a6 6 0 0 1 1.5-11.8A8 8 0 0 1 28 14a6 6 0 0 1 5 11V29z" fill={w} />,
    candado: <><rect x="13" y="20" width="18" height="14" rx="3" fill={w} /><path d="M16 20v-4a6 6 0 0 1 12 0v4" stroke={w} strokeWidth="3" fill="none" /></>,
    sobre: <><rect x="10" y="14" width="24" height="17" rx="3" fill={w} /><path d="M11 16l11 8 11-8" stroke={color} strokeWidth="2.5" fill="none" /></>,
    casa: <path d="M12 32h20M16 32V18h12v14M14 18l8-6 8 6" stroke={w} strokeWidth="2.5" fill="none" strokeLinejoin="round" />,
    cajas: <><rect x="11" y="12" width="22" height="8" rx="2" fill={w} /><rect x="11" y="24" width="22" height="8" rx="2" fill={w} /><circle cx="15" cy="16" r="1.5" fill={color} /><circle cx="15" cy="28" r="1.5" fill={color} /></>,
    respaldo: <><rect x="11" y="13" width="22" height="7" rx="2" fill={w} /><path d="M13 20v11h18V20M19 25h6" stroke={w} strokeWidth="2.5" fill="none" /></>,
    chispa: <path d="M22 10l3 9 9 3-9 3-3 9-3-9-9-3 9-3z" fill="#F2C14E" />,
    nodo: <><rect x="11" y="11" width="22" height="22" rx="4" fill="none" stroke={w} strokeWidth="2.5" /><rect x="17" y="17" width="10" height="10" rx="2" fill={w} /><path d="M16 8v3M22 8v3M28 8v3M16 33v3M22 33v3M28 33v3" stroke={w} strokeWidth="2" /></>,
    disco: <><ellipse cx="22" cy="14" rx="10" ry="4" fill={w} /><path d="M12 14v16c0 2.2 4.5 4 10 4s10-1.8 10-4V14" fill="none" stroke={w} strokeWidth="2.5" /><path d="M12 22c0 2.2 4.5 4 10 4s10-1.8 10-4" fill="none" stroke={w} strokeWidth="2" /></>,
    red: <><circle cx="22" cy="22" r="11" fill="none" stroke={w} strokeWidth="2.5" /><path d="M11 22h22M22 11c-5 6-5 16 0 22M22 11c5 6 5 16 0 22" stroke={w} strokeWidth="2" fill="none" /></>,
    base: <><ellipse cx="22" cy="13" rx="9" ry="3.5" fill={w} /><path d="M13 13v17c0 2 4 3.5 9 3.5s9-1.5 9-3.5V13" fill={w} opacity=".85" /></>,
    archivo: <><path d="M14 10h11l6 6v18H14z" fill={w} /><path d="M17 21h10M17 25h10M17 29h7" stroke={color} strokeWidth="2" /></>,
    grafica: <path d="M12 32h21M15 30v-8M21 30V14M27 30v-11" stroke={w} strokeWidth="3.5" strokeLinecap="round" />,
    escudo: <path d="M22 10l10 4v7c0 7-4.5 11-10 13-5.5-2-10-6-10-13v-7z" fill={w} />,
    engrane: <><circle cx="22" cy="22" r="6" fill="none" stroke={w} strokeWidth="3" /><path d="M22 9v5M22 30v5M9 22h5M30 22h5M13 13l3.5 3.5M27.5 27.5L31 31M13 31l3.5-3.5M27.5 16.5L31 13" stroke={w} strokeWidth="3" strokeLinecap="round" /></>,
    reloj: <><circle cx="22" cy="22" r="11" fill={w} /><path d="M22 15v7l5 3" stroke={color} strokeWidth="2.5" fill="none" strokeLinecap="round" /></>,
  };
  return (
    <svg width={size} height={size} viewBox="0 0 44 44" aria-hidden="true" className="icono">
      {redondo ? <circle cx="22" cy="22" r="22" fill={color} /> : <rect width="44" height="44" rx="12" fill={color} />}
      {trazos[d]}
    </svg>
  );
}

/** How each check is drawn: its illustration and color. */
const dibujoDe: Record<string, [Dibujo, string]> = {
  kubernetes: ["nodo", "var(--anil)"], "platform-services": ["engrane", "var(--teja)"], nodes: ["nodo", "var(--cempasuchil)"],
  metrics: ["grafica", "#1F8A8A"], ingress: ["red", "var(--anil)"], "cert-manager": ["candado", "var(--nopal)"],
  "cluster-issuer": ["candado", "var(--nopal)"], certificates: ["candado", "var(--nopal)"], storage: ["disco", "#7B2F8E"],
  backups: ["respaldo", "#1F8A8A"], buildkit: ["casa", "var(--teja)"], registry: ["cajas", "var(--anil)"],
  "build-namespace": ["casa", "var(--teja)"], "build-isolation": ["escudo", "var(--grana)"], dns: ["nube", "var(--cempasuchil)"],
  notifications: ["sobre", "var(--rosa)"], "log-archive": ["archivo", "#6B5444"], analytics: ["grafica", "var(--rosa)"],
  ai: ["chispa", "#7B2F8E"], github: ["codigo", "#2B1B12"], database: ["base", "var(--anil)"],
};

/**
 * «What needs you»: one illustrated card per requirement that is not well and per unhealthy workload or certificate, each
 * with what to do about it. Nothing to do is said too.
 */
function Necesita({ checks, problems, onAbrir }: { checks: EnvCheck[]; problems: EnvProblem[]; onAbrir: (id: string) => void }) {
  const [todos, setTodos] = useState(false);
  const orden = [...checks].sort((a, b) => rank[b.status] - rank[a.status]);
  const tope = 8;
  const items = orden.length + problems.length;
  const lista: ReactNode[] = [
    ...orden.map((c) => <TarjetaCheck key={c.id} c={c} onAbrir={onAbrir} />),
    ...problems.map((p) => <TarjetaProblema key={p.kind + p.namespace + p.name} p={p} />),
  ];
  return (
    <section className="stack-sm" aria-labelledby="entorno-necesita">
      <h2 id="entorno-necesita" className="entorno-h2">{t("What needs you")}</h2>
      {items === 0 ? (
        <div className="necesita nada">
          <Icono d="chispa" color="var(--nopal)" size={54} redondo />
          <span><strong>{t("Nothing for now")}</strong><span className="small">{t("Every requirement is in place and no workload or certificate is failing.")}</span></span>
        </div>
      ) : (
        <div className="necesitas">{todos ? lista : lista.slice(0, tope)}</div>
      )}
      {items > tope && <button className="link small" onClick={() => setTodos(!todos)}>{todos ? t("Show fewer") : tn(items - tope, "Show {n} more", "Show {n} more")}</button>}
    </section>
  );
}

/** A requirement that is not well: what it says, and its action (change its key, open its service, or see how to fix it). */
function TarjetaCheck({ c, onAbrir }: { c: EnvCheck; onAbrir: (id: string) => void }) {
  const [ver, setVer] = useState(false);
  const [d, color] = dibujoDe[c.id] ?? ["engrane", "var(--teja)"];
  const grave = c.status === "error" || (c.status === "missing" && c.required);
  const conexion = conexionDe[c.id];
  let accion: ReactNode;
  if (c.id === "dns" && session.operator) accion = <Link className="btn primary pildora" to="/environment/llaves?llave=cloudflare">{t("Change the token")}</Link>;
  else if (conexion) accion = <a className="btn pildora" href={`#conexion-${conexion}`} onClick={() => onAbrir(conexion)}>{t("See")}</a>;
  else accion = <button className="pildora" aria-expanded={ver} onClick={() => setVer(!ver)}>{ver ? t("Hide") : t("See")}</button>;
  return (
    <div className={`necesita ${grave ? "bad" : "warn"}`}>
      <div className="necesita-fila">
        <span className={grave ? undefined : "flota"}><Icono d={d} color={color} size={54} redondo /></span>
        <span className="necesita-texto"><strong>{c.name}</strong><span className="small">{c.summary}</span></span>
        {accion}
      </div>
      {ver && (
        <div className="env-more">
          {c.details?.map((x) => <div key={x} className="small">• {x}</div>)}
          {c.fix && <pre className="file" style={{ whiteSpace: "pre-wrap" }}>{c.fix}</pre>}
        </div>
      )}
    </div>
  );
}

/** An unhealthy workload or certificate in the cluster, and how to look at it. */
function TarjetaProblema({ p }: { p: EnvProblem }) {
  const [ver, setVer] = useState(false);
  const cert = p.kind.toLowerCase().includes("certificate");
  return (
    <div className="necesita warn">
      <div className="necesita-fila">
        <Icono d={cert ? "candado" : "cajas"} color={cert ? "var(--nopal)" : "var(--grana)"} size={54} redondo />
        <span className="necesita-texto"><strong>{p.kind} <span className="mono">{p.namespace}/{p.name}</span></strong><span className="small">{p.reason}{p.since ? ` · ${timeAgo(p.since)}` : ""}</span></span>
        <button className="pildora" aria-expanded={ver} onClick={() => setVer(!ver)}>{ver ? t("Hide") : t("See")}</button>
      </div>
      {ver && <pre className="file" style={{ whiteSpace: "pre-wrap" }}>kubectl -n {p.namespace} describe {p.kind.toLowerCase()} {p.name}</pre>}
    </div>
  );
}

const TECHOS = ["#B5532A", "#1F8A8A", "#7B2F8E", "#23408E", "#3E7B3A"];
const LUZ = "#F2C14E";
const APAGADA = "#5A6E7A";

/**
 * A node as a small house: lit windows for how busy its processor is, a pink skirting that rises with its memory, and a note
 * floating over it when something is wrong. The node in charge has a clock tower. Pressed, it shows the node's details.
 */
function Casita({ n, i, on, onPick }: { n: EnvNode; i: number; on: boolean; onPick: () => void }) {
  const cpu = n.cpuUsed !== undefined && n.cpuCores ? Math.min(1, n.cpuUsed / n.cpuCores) : 0;
  const mem = n.memUsed !== undefined && n.memBytes ? Math.min(1, n.memUsed / n.memBytes) : 0;
  const mando = (n.roles ?? []).some((r) => r === "control-plane" || r === "master");
  const nota = !n.ready ? t("down") : n.pressure?.length ? t("pressure") : n.unschedulable ? t("cordoned") : mem >= 0.85 ? t("full") : "";
  const ancho = mando ? 128 : [96, 108, 108][i % 3];
  const x0 = 110 - ancho / 2;
  const top = mando ? 40 : 36;
  const ventanas = mando ? [58, 82, 122, 146] : [x0 + 12, x0 + ancho - 28];
  const lit = Math.round(cpu * ventanas.length);
  const zocalo = Math.round(mem * (86 - top));
  const pct = (x: number) => `${Math.round(x * 100)}%`;
  const etiqueta = t("{name}: processor {cpu}, memory {mem}", { name: n.name, cpu: pct(cpu), mem: pct(mem) }) + (nota ? ` · ${nota}` : "");
  return (
    <button className={`casita${on ? " on" : ""}${nota ? " aviso" : ""}`} style={{ ["--d" as string]: `${i * 120}ms` }} onClick={onPick} aria-pressed={on} aria-label={etiqueta}>
      <svg width="100%" height="96" viewBox="0 0 220 96" aria-hidden="true">
        <rect className="suelo" x="0" y="86" width="220" height="10" />
        <path d={`M${x0 - 6} ${top} L110 ${top - 22} L${x0 + ancho + 6} ${top} Z`} fill={TECHOS[i % TECHOS.length]} />
        <rect x={x0} y={top} width={ancho} height={86 - top} fill="#FBF7EF" stroke="#C9B79F" strokeWidth="2" />
        {mando && (
          <g>
            <rect x="100" y="0" width="20" height="18" fill="#F4E9D8" stroke="#C9B79F" strokeWidth="2" />
            <circle cx="110" cy="9" r="5" fill="#FFFDF8" stroke="#7A3E0B" strokeWidth="1.5" />
            <path d="M110 9V6M110 9l2 1" stroke="#2B1B12" strokeWidth="1.2" />
          </g>
        )}
        <rect className="zocalo" x={x0} y={86 - zocalo} width={ancho} height={zocalo} fill={mem >= 0.85 ? "#D6246E" : "#E86A9A"} opacity={mem >= 0.85 ? 0.55 : 0.35} />
        {ventanas.map((x, j) => (
          <rect key={j} className={j < lit ? "luz" : undefined} x={x} y={top + 10} width="16" height="18" rx="2" fill={j < lit ? LUZ : APAGADA} style={{ ["--d" as string]: `${j * -0.8}s` }} />
        ))}
        <path d="M102 86 V70 Q110 62 118 70 V86 Z" fill="#7A3E0B" />
        {nota && (
          <g className="nota-flota">
            <rect x="150" y="2" width="62" height="24" rx="6" fill="#FFFDF8" stroke="#E8890C" strokeWidth="2" />
            <text x="181" y="19" textAnchor="middle" fontSize="12" fontWeight="800" fill="#8A5300">{nota}</text>
          </g>
        )}
      </svg>
      <span className="casita-nombre"><strong>{n.name}</strong><span className="small muted">{mando ? t("in charge") : (n.roles?.length ? n.roles.join(", ") : t("worker"))}</span></span>
      <Barra label={t("Processor")} v={n.cpuUsed !== undefined ? cpu : undefined} color="var(--cempasuchil)" />
      <Barra label={t("Memory")} v={n.memUsed !== undefined ? mem : undefined} color={mem >= 0.85 ? "var(--rosa)" : "var(--grana)"} />
    </button>
  );
}

/** A thin bar with its label and percent; "—" without live usage. */
function Barra({ label: l, v, color }: { label: string; v?: number; color: string }) {
  const p = v === undefined ? 0 : Math.round(v * 100);
  return (
    <span className="barra">
      <span className="barra-nombre">{l}</span>
      <span className="barra-fondo" role="meter" aria-valuenow={p} aria-valuemin={0} aria-valuemax={100} aria-label={l}><span style={{ width: `${p}%`, background: color }} /></span>
      <span className="barra-pct">{v === undefined ? "—" : `${p}%`}</span>
    </span>
  );
}

/** The connection whose card opens for each check (the «See» of what needs you). */
const conexionDe: Record<string, string> = {
  github: "codigo", dns: "dns", "cert-manager": "certificados", "cluster-issuer": "certificados", certificates: "certificados",
  notifications: "correo", buildkit: "construccion", registry: "imagenes", backups: "respaldos", ai: "ia",
};

interface Conexion {
  id: string;
  titulo: string;
  sub: string;
  status: EnvStatus;
  estado: string;
  off?: boolean;
  dibujo: Dibujo;
  color: string;
  checks: EnvCheck[];
  provider?: EnvProvider;
}

/**
 * «What it connects to»: a card per service the platform talks to (where the code lives, DNS, certificates, email, the
 * builder, the image registry, backups, AI), each with a dot that breathes with its status. Pressed, it opens its window:
 * what it says, its checks, and what can be done there (the dynamic DNS, a test email, the forjas, changing its key).
 */
function Conexiones({ r, abierto, onAbrir, onSynced }: { r: EnvReport; abierto?: string; onAbrir: (id?: string) => void; onSynced: () => void }) {
  const ch = (id: string) => r.checks.find((c) => c.id === id);
  const prov = (slot: string) => r.providers.find((p) => p.slot === slot);
  const una = (...ids: string[]) => ids.map(ch).filter((c): c is EnvCheck => !!c);
  const estado = (s: EnvStatus, okText: string) => (s === "ok" ? okText : t(label[s]));
  const dnsP = prov("dns");
  const dnsS = peor(dnsP?.status, ch("dns")?.status);
  const certs = una("cert-manager", "cluster-issuer", "certificates");
  const certS = peor(...certs.map((c) => c.status));
  const fuente = prov("source");
  const ia = ch("ai");
  const lista: Conexion[] = [
    { id: "codigo", titulo: t("Code"), sub: fuente?.name ?? "—", status: peor(fuente?.status, ch("github")?.status), estado: estado(peor(fuente?.status, ch("github")?.status), t("Connected")), dibujo: "codigo", color: "#2B1B12", checks: una("github"), provider: fuente },
    {
      id: "dns", titulo: "DNS", sub: dnsP ? (dnsP.zones ? `${dnsP.name} · ${tn(dnsP.zones, "{n} zone", "{n} zones")}` : dnsP.name) : "—",
      status: venceProximo(dnsP?.expiresAt) && dnsS === "ok" ? "warning" : dnsS,
      estado: dnsP?.expiresAt && venceProximo(dnsP.expiresAt) ? t("Expires in {n} days", { n: Math.max(0, diasHasta(dnsP.expiresAt)) }) : dnsP?.active === "manual" ? t("By hand") : estado(dnsS, t("Works")),
      dibujo: "nube", color: "var(--cempasuchil)", checks: una("dns"), provider: dnsP,
    },
    { id: "certificados", titulo: t("Certificates"), sub: tn(r.cluster.certificates ?? 0, "{n} certificate", "{n} certificates"), status: certS, estado: certS === "ok" ? t("All valid") : tn(r.cluster.certificatesBad ?? 0, "{n} needs attention", "{n} need attention"), dibujo: "candado", color: "var(--nopal)", checks: certs },
    { id: "correo", titulo: t("Email"), sub: "Resend", status: ch("notifications")?.status ?? "ok", estado: estado(ch("notifications")?.status ?? "ok", t("Works")), dibujo: "sobre", color: "var(--rosa)", checks: una("notifications") },
    { id: "construccion", titulo: t("Building"), sub: "BuildKit", status: ch("buildkit")?.status ?? "ok", estado: estado(ch("buildkit")?.status ?? "ok", t("Ready to build")), dibujo: "casa", color: "var(--teja)", checks: una("buildkit", "build-namespace", "build-isolation") },
    { id: "imagenes", titulo: t("Images"), sub: prov("registry")?.name ?? "—", status: ch("registry")?.status ?? "ok", estado: estado(ch("registry")?.status ?? "ok", t("Answers")), dibujo: "cajas", color: "var(--anil)", checks: una("registry") },
    { id: "respaldos", titulo: t("Backups"), sub: "Longhorn", status: ch("backups")?.status ?? "ok", estado: estado(ch("backups")?.status ?? "ok", t("Up to date")), dibujo: "respaldo", color: "#1F8A8A", checks: una("backups", "storage") },
    { id: "ia", titulo: t("AI"), sub: t("Diagnosis and agents"), status: ia?.status ?? "ok", off: !ia || ia.off, estado: !ia || ia.off ? t("Not set up") : estado(ia.status, t("Answers")), dibujo: "chispa", color: "#7B2F8E", checks: una("ai") },
  ];
  const v = lista.find((c) => c.id === abierto);
  return (
    <section className="stack-sm" aria-labelledby="entorno-conexiones">
      <div className="row between" style={{ alignItems: "baseline" }}>
        <h2 id="entorno-conexiones" className="entorno-h2">{t("What it connects to")}</h2>
        {session.operator && <Link to="/environment/llaves" style={{ fontWeight: 800 }}>{t("Keys and settings")} →</Link>}
      </div>
      <div className="conexiones">
        {lista.map((c, i) => (
          <button key={c.id} id={`conexion-${c.id}`} className={`conexion ${c.off ? "off" : tone[c.status]}${abierto === c.id ? " on" : ""}`} aria-expanded={abierto === c.id}
            onClick={() => onAbrir(abierto === c.id ? undefined : c.id)}>
            <Icono d={c.dibujo} color={c.color} />
            <span className="conexion-texto">
              <strong>{c.titulo}</strong>
              <span className="small">{c.sub}</span>
              <span className="conexion-estado"><i className="punto" style={{ ["--d" as string]: `${i * -0.4}s` }} />{c.estado}</span>
            </span>
          </button>
        ))}
      </div>
      {v && (
        <div className="ventanilla" key={v.id}>
          <div className="row between" style={{ alignItems: "baseline" }}>
            <h3>{v.titulo}: {v.sub}</h3>
            <StatusBadge status={v.status} />
          </div>
          {v.provider && <p style={{ margin: 0 }}>{v.provider.detail}</p>}
          {v.id === "dns" && r.ddns && <DynamicDNS ddns={r.ddns} onSynced={onSynced} />}
          {v.id === "codigo" && session.operator && <Forjas />}
          {v.checks.map((c) => <CheckRow key={c.id} c={c} />)}
          {session.operator && (v.id === "dns" || v.id === "correo" || v.id === "ia") && (
            <div className="row">
              <Link className="btn pildora" to={`/environment/llaves?llave=${v.id === "dns" ? "cloudflare" : v.id}`}>{t("Change its key")}</Link>
              {v.id === "ia" && <Link to="/ia">{t("The AI page")}</Link>}
            </div>
          )}
        </div>
      )}
    </section>
  );
}


/** Dynamic DNS: the public IP the records follow, when it last changed, and a button to check it now. */
function DynamicDNS({ ddns, onSynced }: { ddns: NonNullable<EnvReport["ddns"]>; onSynced: () => void }) {
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string>();
  const sync = async () => {
    setBusy(true);
    setMsg(undefined);
    try {
      const res = await envApi.dnsSync();
      const n = res.updated?.length ?? 0;
      setMsg(res.changed ? tn(n, "IP changed to {ip}; {n} record updated", "IP changed to {ip}; {n} records updated", { ip: res.ip ?? "" })
        : n ? tn(n, "{n} record corrected", "{n} records corrected") : t("Still {ip}; nothing to update", { ip: res.ip ?? "" }));
      onSynced();
    } catch (e) {
      setMsg((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="env-more" style={{ margin: 0 }}>
      <div className="small"><strong>{t("Dynamic DNS")}</strong> · {t("public IP")} <span className="mono">{ddns.ip ?? t("unknown")}</span></div>
      <div className="small muted">
        {t("checked {when}", { when: timeAgo(ddns.checkedAt) })}{ddns.changedAt && ` · ${t("last change {when}", { when: timeAgo(ddns.changedAt) })}`}
      </div>
      {ddns.error && <div className="small" style={{ color: "var(--bad)" }}>{ddns.error}</div>}
      <div className="row" style={{ marginTop: 6 }}>
        <button onClick={sync} disabled={busy}>{busy ? t("Checking…") : t("Update now")}</button>
        {msg && <span className="small muted">{msg}</span>}
      </div>
      <div className="small muted" style={{ marginTop: 6 }}>
        {t("App records follow your IP automatically. To include a record you made by hand, add")} <code>rendimiento-ddns</code> {t("to its comment in Cloudflare.")}
      </div>
    </div>
  );
}

/**
 * One requirement: its status and summary; open (already open when it fails) it shows the details and how to fix it.
 */
function CheckRow({ c }: { c: EnvCheck }) {
  const [open, setOpen] = useState(c.status !== "ok");
  const hasMore = (c.details?.length ?? 0) > 0 || !!c.fix;
  return (
    <div className="env-check">
      <div className="row between" style={{ cursor: hasMore ? "pointer" : undefined }} onClick={() => hasMore && setOpen(!open)}>
        <div className="row" style={{ gap: 12, flexWrap: "nowrap", minWidth: 0 }}>
          <StatusBadge status={c.status} />
          <div style={{ minWidth: 0 }}>
            <div>
              <strong>{c.name}</strong>
              {c.version && <span className="mono small muted"> {c.version}</span>}
              {!c.required && <span className="small muted"> · {t("optional")}</span>}
            </div>
            <div className="small muted">{c.summary}</div>
          </div>
        </div>
        <div className="row" style={{ gap: 8, flexWrap: "nowrap" }}>
          {c.id === "notifications" && c.status === "ok" && <TestEmail />}
          {hasMore && <span className="small muted">{open ? "▾" : "▸"}</span>}
        </div>
      </div>
      {open && hasMore && (
        <div className="env-more">
          {c.details?.map((d) => <div key={d} className="small">• {d}</div>)}
          {c.fix && c.status !== "ok" && (
            <div style={{ marginTop: 8 }}>
              <div className="small muted">{t("How to fix")}</div>
              <pre className="file" style={{ whiteSpace: "pre-wrap" }}>{c.fix}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** Sends a sample notification now, and says what happened. */
function TestEmail() {
  const [state, setState] = useState<"idle" | "sending" | "sent" | string>("idle");
  const send = async (e: React.MouseEvent) => {
    e.stopPropagation();
    setState("sending");
    try {
      const r = await envApi.testEmail();
      setState(t("sent to {to}", { to: r.to.join(", ") }));
    } catch (err) {
      setState(t("failed: {msg}", { msg: (err as Error).message }));
    }
  };
  return (
    <>
      {state !== "idle" && state !== "sending" && <span className="small muted">{state}</span>}
      <button disabled={state === "sending"} onClick={send}>{state === "sending" ? t("Sending…") : t("Send test email")}</button>
    </>
  );
}

/** A bar of how much of something is used (green, then yellow from 75%, red from 90%). */
function Meter({ label: l, used, total, fmt }: { label: string; used?: number; total: number; fmt: (n: number) => string }) {
  if (used === undefined) return <div className="small muted">{l}: {fmt(total)} ({t("no live usage")})</div>;
  const pct = Math.min(100, Math.round((used / total) * 100));
  const color = pct >= 90 ? "var(--bad)" : pct >= 75 ? "var(--warn)" : "var(--ok)";
  return (
    <div>
      <div className="row between small"><span>{l}</span><span className="muted">{fmt(used)} / {fmt(total)} · {pct}%</span></div>
      <div className="meter" role="meter" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={l}>
        <div style={{ width: `${pct}%`, background: color }} />
      </div>
    </div>
  );
}

/**
 * Capacity: every node's processor and memory (in use, and reserved by its pods, which is what decides where a new
 * pod fits) and whether it takes runs; and how much the platform runs at once. Administrators change it here, and
 * the platform follows within 15 seconds, with no redeploy.
 */
function Capacidad({ nodes }: { nodes: EnvNode[] }) {
  const conexion = useConexion();
  const { data: v, setData } = usePoll(envApi.capacity, [], 60000);
  const [edit, setEdit] = useState<Capacity>();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  if (!v) return null;
  // The platform's administrators: those of its own organization.
  const admin = session.operator;
  const c = edit ?? v.saved;
  const set = (patch: Partial<Capacity>) => setEdit({ ...c, ...patch });
  const changed = !!edit && JSON.stringify(edit) !== JSON.stringify(v.saved);
  const always = v.defaults.nodesOff;
  const pct = (a: number, b: number) => (b ? Math.min(100, Math.round((a / b) * 100)) : 0);
  const color = (p: number) => (p >= 90 ? "var(--bad)" : p >= 75 ? "var(--warn)" : "var(--ok)");
  const bar = (p: number, label: string) => (
    <div className="meter" role="meter" aria-valuenow={p} aria-valuemin={0} aria-valuemax={100} aria-label={label}>
      <div style={{ width: `${p}%`, background: color(p) }} />
    </div>
  );
  const save = () => {
    if (!edit) return;
    setBusy(true); setErr(undefined);
    envApi.saveCapacity(edit).then((nv) => { setBusy(false); setEdit(undefined); setData(nv); }, (e) => { setBusy(false); setErr(e); });
  };
  return (
    <section className="card stack capacidad">
      <div>
        <h2 style={{ margin: 0 }}>{t("Capacity")}</h2>
        <p className="small muted" style={{ margin: 0 }}>{t("Reserved is what the pods on a node ask for: a new pod goes only where it fits, however little the node really uses. Runs' steps go only to the nodes that take runs.")}</p>
      </div>
      <div className="capacidad-nodos">
        {nodes.map((n) => {
          const cpuRes = pct(n.cpuReserved, n.cpuCores);
          const memRes = pct(n.memReserved, n.memBytes);
          const fixed = always.includes(n.name);
          const takes = !fixed && !c.nodesOff.includes(n.name);
          return (
            <div key={n.name} className="capacidad-nodo">
              <div className="row between" style={{ gap: 6 }}>
                <strong>{n.name}{v.busy.includes(n.name) && <> <span className="badge warn" title={t("Too full now: new steps and builds go to other nodes until it eases.")}>{t("busy")}</span></>}</strong>
                <label className="small" title={fixed ? t("Off in BUILD_EXCLUDE_NODES") : undefined}>
                  <input type="checkbox" checked={takes} disabled={!admin || fixed}
                    onChange={(e) => set({ nodesOff: e.target.checked ? c.nodesOff.filter((x) => x !== n.name) : [...c.nodesOff, n.name] })} />{" "}
                  {t("takes runs")}
                </label>
              </div>
              <span className="small muted">{(n.roles?.length ? n.roles.join(", ") : t("worker"))} · {cores(n.cpuCores)} · {gib(n.memBytes)}</span>
              <div className="small row between"><span>{t("CPU in use")}</span><span className="muted">{n.cpuUsed !== undefined ? `${pct(n.cpuUsed, n.cpuCores)}%` : "—"}</span></div>
              {n.cpuUsed !== undefined && bar(pct(n.cpuUsed, n.cpuCores), t("CPU in use"))}
              <div className="small row between"><span>{t("CPU reserved")}</span><span className="muted">{cores(n.cpuReserved)} · {cpuRes}%</span></div>
              {bar(cpuRes, t("CPU reserved"))}
              <div className="small row between"><span>{t("Memory in use / reserved")}</span><span className="muted">{n.memUsed !== undefined ? `${pct(n.memUsed, n.memBytes)}%` : "—"} / {memRes}%</span></div>
              {bar(memRes, t("Memory reserved"))}
            </div>
          );
        })}
      </div>
      <div className="form-grid">
        <label className="field"><span>{t("Runs at once")}</span>
          <input type="number" min={0} max={v.maxWorkers} disabled={!admin} value={c.parallelRuns || ""} placeholder={String(v.defaults.parallelRuns)}
            onChange={(e) => set({ parallelRuns: Number(e.target.value) })} />
          <span className="small muted">{t("empty: {n}, from PARALLEL_RUNS", { n: v.defaults.parallelRuns })}</span>
        </label>
        <label className="field"><span>{t("Runs of one app at once")}</span>
          <input type="number" min={0} max={v.maxWorkers} disabled={!admin} value={c.runsPerApp || ""} placeholder={String(v.defaults.runsPerApp)}
            onChange={(e) => set({ runsPerApp: Number(e.target.value) })} />
          <span className="small muted">{t("its deploys still go one at a time; empty: {n}", { n: v.defaults.runsPerApp })}</span>
        </label>
        <label className="field"><span>{t("Caches of each test")}</span>
          <input type="number" min={0} max={v.maxTestCaches} disabled={!admin} value={c.testCaches || ""} placeholder={String(v.defaults.testCaches)}
            onChange={(e) => set({ testCaches: Number(e.target.value) })} />
          <span className="small muted">{t("that many runs test at once without waiting for a cache; each one is a volume; empty: {n}", { n: v.defaults.testCaches })}</span>
        </label>
        <label className="field"><span>{t("A node is busy from (%)")}</span>
          <input type="number" min={50} max={100} step={5} disabled={!admin} value={c.busyPercent || ""} placeholder={String(v.defaultBusyPercent)}
            onChange={(e) => set({ busyPercent: Number(e.target.value) })} />
          <span className="small muted">{t("CPU reserved or in use; a busy node takes nothing new until it eases. 100: never busy")}</span>
        </label>
        <label className="field"><span>{t("Most CPU a step reserves (cores)")}</span>
          <input type="number" min={0} step={0.25} disabled={!admin} value={c.stepCpuMillis ? c.stepCpuMillis / 1000 : ""} placeholder={t("what each app asks")}
            onChange={(e) => set({ stepCpuMillis: Math.round(Number(e.target.value) * 1000) })} />
          <span className="small muted">{t("lower it so steps fit on busy nodes; they still use free cores")}</span>
        </label>
      </div>
      <ErrorBox error={err} />
      {admin ? changed && (
        <div className="row" style={{ gap: 8 }}>
          <button className="primary" {...apagado(conexion, busy)} onClick={save}>{busy ? t("Saving…") : t("Save")}</button>
          <button disabled={busy} onClick={() => setEdit(undefined)}>{t("Discard")}</button>
        </div>
      ) : <p className="small muted" style={{ margin: 0 }}>{t("Only administrators change this.")}</p>}
      {v.saved.updatedBy && <span className="small muted">{t("changed by {who} {when}", { who: v.saved.updatedBy, when: timeAgo(v.saved.updatedAt!) })}</span>}
    </section>
  );
}

/** What each live setting does, in a sentence. */
const settingHelp: Record<string, () => string> = {
  ORG_NETWORK_POLICY: () => t("On: each app's namespace lets in only its own organization's namespaces, the ingress, rendimiento and the ones listed below. It holds only where the cluster's network plugin enforces network policies."),
  INGRESS_NAMESPACE: () => t("Where the ingress controller runs: public traffic comes in from there. Empty: the usual one for the ingress class."),
  ORG_NETWORK_POLICY_NAMESPACES: () => t("Other namespaces always let in, such as monitoring, separated by commas."),
  ORG_NETWORK_POLICY_CIDRS: () => t("Address ranges always let in, such as the nodes' network when the ingress runs on the host network."),
  ORG_QUOTA_CPU: () => t("The most CPU the apps of one organization other than the platform's may ask for together, such as 4. Empty: no limit."),
  ORG_QUOTA_MEMORY: () => t("The most memory they may ask for together, such as 8Gi. Empty: no limit."),
};

/**
 * The platform's settings that change while it runs: the boundary between organizations and its quota. A value saved here
 * wins over the ConfigMap's until it is taken back; every process follows it within 15 seconds, with no restart.
 */
function Ajustes() {
  const { data: v, setData, error, reload } = usePoll(envApi.settings, [], 60000);
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string>();
  const [err, setErr] = useState<unknown>();
  const [check, setCheck] = useState<BoundaryCheck>();
  // The cluster was slow to say which apps are outside the wall: the settings came without it, so ask again shortly.
  const pending = !!v?.outsidePending;
  useEffect(() => {
    if (!pending) return;
    const id = setTimeout(reload, 3000);
    return () => clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pending, v]);
  // Who calls in comes from the services catalog, still being read: ask again shortly (unless the check was put away).
  const checkPending = !!check?.pending;
  useEffect(() => {
    if (!checkPending) return;
    const id = setTimeout(() => envApi.boundaryCheck().then((c) => setCheck((was) => was && c), setErr), 3000);
    return () => clearTimeout(id);
  }, [checkPending, check]);
  if (error) return <section className="card"><ErrorBox error={error} /></section>;
  if (!v) return null;
  const run = (key: string, p: Promise<SettingsView>) => {
    setBusy(key); setErr(undefined);
    p.then((nv) => { setBusy(undefined); setData(nv); setEdits((e) => { const n = { ...e }; delete n[key]; return n; }); },
      (e) => { setBusy(undefined); setErr(e); });
  };
  const save = (key: string, value: string) => run(key, envApi.saveSetting(key, value));
  const on = v.live.find((s) => s.key === "ORG_NETWORK_POLICY")?.value === "true";
  const listed = v.live.find((s) => s.key === "ORG_NETWORK_POLICY_NAMESPACES")?.value ?? "";
  const review = () => { setErr(undefined); envApi.boundaryCheck().then(setCheck, setErr); };
  const letIn = (ns: string) => {
    const next = [...listed.split(/[,\s]+/).filter(Boolean), ns].join(",");
    run("ORG_NETWORK_POLICY_NAMESPACES", envApi.saveSetting("ORG_NETWORK_POLICY_NAMESPACES", next).then((nv) => {
      setCheck((c) => c && { ...c, callers: c.callers.map((x) => (x.namespace === ns ? { ...x, in: true } : x)) });
      return nv;
    }));
  };
  const group = (g: LiveSetting["group"], title: string, intro: string) => (
    <div className="stack" style={{ gap: 10 }}>
      <div>
        <h3 style={{ margin: 0 }}>{title}</h3>
        <p className="small muted" style={{ margin: 0 }}>{intro}</p>
      </div>
      {v.live.filter((s) => s.group === g).map((s) => <AjusteRow key={s.key} s={s} edit={edits[s.key]} busy={busy === s.key}
        onEdit={(x) => setEdits({ ...edits, [s.key]: x })} onSave={(x) => save(s.key, x)} onForget={() => run(s.key, envApi.forgetSetting(s.key))}
        onTurnOn={s.key === "ORG_NETWORK_POLICY" && !on ? review : undefined} />)}
    </div>
  );
  const blocked = check?.callers.filter((c) => !c.in) ?? [];
  return (
    <section className="card stack ajustes">
      <div>
        <h2 style={{ margin: 0 }}>{t("Platform settings")}</h2>
        <p className="small muted" style={{ margin: 0 }}>{t("A value saved here wins over the ConfigMap's until you take it back. Every part of the platform follows it within 15 seconds, with no restart, and each change goes to the bitácora.")}</p>
      </div>
      {v.invalid && <div className="badge bad" style={{ whiteSpace: "normal" }}>{t("A saved value does not check out, so the ConfigMap's are in force: {why}", { why: v.invalid })}</div>}
      {group("boundary", t("Boundary between organizations"), t("Which namespaces may reach each app. Public traffic comes in through {ns}; rendimiento ({platform}) is always let in.", { ns: v.ingressNamespace || "—", platform: v.platformNamespace }))}
      {session.cluster && v.apps > 0 && (
        <div className="small stack" style={{ gap: 4 }}>
          {on && v.outsidePending ? <Esqueleto><Hueso className="esq-renglon" w="45%" /></Esqueleto> : <span>{on
            ? t("{inside} of {n} apps are inside the wall.", { inside: v.apps - Object.keys(v.outside ?? {}).length, n: v.apps })
            : t("The wall is down: none of the {n} apps is walled.", { n: v.apps })}{" "}<Link to="/universe">{t("See it in the Universe")}</Link></span>}
          {Object.entries(v.outside ?? {}).map(([app, why]) => <span key={app} className="muted"><Link to={`/apps/${app}`}>{app}</Link>: {outsideWhy(why, v.nodes?.[app])}</span>)}
        </div>
      )}
      {check && (
        <div className="card stack" style={{ background: "var(--surface-2, transparent)" }}>
          <strong>{t("Before turning it on")}</strong>
          {!check.ingressFound && <span className="badge warn" style={{ whiteSpace: "normal" }}>{t("The ingress namespace {ns} does not exist: public traffic would be stopped.", { ns: check.ingressNamespace || "—" })}</span>}
          <p className="small muted" style={{ margin: 0 }}>{t("Namespaces whose workloads point at an app's service in their environment. rendimiento does not see traffic, so this is a guide, not a proof: something that calls an app without saying so in its environment is not listed.")}</p>
          {check.pending ? <Esqueleto><Hueso className="esq-renglon" w="60%" /></Esqueleto> : <>
            {check.callers.length === 0 && <span className="small">{t("Nothing outside an app's organization points at its services.")}</span>}
            {check.callers.map((c) => (
              <div key={c.namespace} className="row between" style={{ gap: 8 }}>
                <span><strong className="mono">{c.namespace}</strong> <span className="small muted">{t("calls {what}", { what: c.calls.join(", ") })}</span></span>
                {c.in ? <span className="badge ok">{t("let in")}</span> : <button disabled={!!busy} onClick={() => letIn(c.namespace)}>{t("Let it in")}</button>}
              </div>
            ))}
          </>}
          <div className="row" style={{ gap: 8 }}>
            <button className="primary" disabled={!!busy || !check.ingressFound || !!check.pending} onClick={() => { setCheck(undefined); save("ORG_NETWORK_POLICY", "true"); }}>
              {blocked.length ? t("Turn it on anyway") : t("Turn it on")}
            </button>
            <button disabled={!!busy} onClick={() => setCheck(undefined)}>{t("Cancel")}</button>
          </div>
          <span className="small muted">{t("If an app stops answering, turn it off here: the policies go within a minute.")}</span>
        </div>
      )}
      {group("quota", t("Quota of each organization"), t("Checked before each release of an app of another organization; the platform's own apps have no quota."))}
      <ErrorBox error={err} />
      <details>
        <summary className="small">{t("Settings that change only with a deploy")}</summary>
        <p className="small muted">{t("The platform needs these to start, or they change what already exists (addresses, volumes, records): change them in the ConfigMap and deploy. Secrets show only whether they are set.")}</p>
        <div className="form-grid">
          {v.fixed.map((f) => (
            <div key={f.key} className="small"><span className="mono">{f.key}</span>: {f.secret ? (f.set ? t("set") : t("not set")) : (f.value || <span className="muted">{t("not set")}</span>)}</div>
          ))}
        </div>
      </details>
    </section>
  );
}

/** One live setting: its value, where it comes from, and a control to change it or take it back. */
function AjusteRow({ s, edit, busy, onEdit, onSave, onForget, onTurnOn }: {
  s: LiveSetting; edit?: string; busy: boolean; onEdit: (v: string) => void; onSave: (v: string) => void; onForget: () => void; onTurnOn?: () => void;
}) {
  const conexion = useConexion();
  const value = edit ?? s.value;
  const differs = !!s.saved && s.saved.value !== s.env;
  const source = s.saved
    ? t("saved here by {who} {when}", { who: s.saved.updatedBy, when: timeAgo(s.saved.updatedAt) })
    : s.env ? t("from the ConfigMap") : t("not set");
  return (
    <div className="field">
      <span className="row between" style={{ gap: 8 }}>
        <span className="mono">{s.key}</span>
        <span className="small muted">{source}</span>
      </span>
      {!s.applies ? <span className="small muted">{t("Does not apply on one computer.")}</span> : s.key === "ORG_NETWORK_POLICY" ? (
        <span className="row" style={{ gap: 8 }}>
          <span className={`badge ${s.value === "true" ? "ok" : ""}`}>{s.value === "true" ? t("on") : t("off")}</span>
          {s.value === "true"
            ? <button disabled={busy} onClick={() => onSave("false")}>{t("Turn it off")}</button>
            : <button disabled={busy} onClick={onTurnOn}>{t("Review and turn it on")}</button>}
        </span>
      ) : (
        <span className="row" style={{ gap: 8 }}>
          <input id={`ajuste-${s.key}`} value={value} onChange={(e) => onEdit(e.target.value)} placeholder={t("not set")} style={{ flex: 1, minWidth: 0 }} />
          {edit !== undefined && edit !== s.value && <button className="primary" {...apagado(conexion, busy)} onClick={() => onSave(edit)}>{busy ? t("Saving…") : t("Save")}</button>}
        </span>
      )}
      <span className="small muted">{settingHelp[s.key]?.()}</span>
      {differs && (
        <span className="small">
          {t("The ConfigMap says {env}, and is ignored while this is saved.", { env: s.env || t("nothing") })}{" "}
          <button className="link" disabled={busy} onClick={onForget}>{t("Back to the ConfigMap's value")}</button>
        </span>
      )}
      {s.saved && !differs && <button className="link small" disabled={busy} onClick={onForget}>{t("Back to the ConfigMap's value")}</button>}
    </div>
  );
}

const gib = (b: number) => `${(b / 1024 ** 3).toLocaleString(locale, { maximumFractionDigits: 1, minimumFractionDigits: 1 })} GiB`;
const cores = (c: number) => t("{n} cores", { n: c.toLocaleString(locale, { maximumFractionDigits: c < 10 ? 2 : 0, minimumFractionDigits: c < 10 ? 2 : 0 }) });

/** One node: ready or not, pressure, its CPU and memory in use, and what it runs on. */
function NodeCard({ n }: { n: EnvNode }) {
  const status: EnvStatus = !n.ready ? "error" : (n.pressure?.length || n.unschedulable) ? "warning" : "ok";
  return (
    <div className="card stack">
      <div className="row between">
        <strong>{n.name}</strong>
        <span className={`badge ${tone[status]}`}>{n.ready ? (status === "ok" ? t("Ready") : t("Degraded")) : t("NotReady")}</span>
      </div>
      <div className="small muted">
        {(n.roles?.length ? n.roles.join(", ") : t("worker"))} · {n.arch} · {tn(n.pods, "{n} pod", "{n} pods")}
        <br />{n.os} · {n.kubelet}
      </div>
      {n.pressure?.map((p) => <div key={p} className="small" style={{ color: "var(--bad)" }}>{p}</div>)}
      {n.unschedulable && <div className="small" style={{ color: "var(--warn)" }}>{t("cordoned (no new pods)")}</div>}
      <Meter label={t("CPU")} used={n.cpuUsed} total={n.cpuCores} fmt={cores} />
      <Meter label={t("Memory")} used={n.memUsed} total={n.memBytes} fmt={gib} />
    </div>
  );
}
