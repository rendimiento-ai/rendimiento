// The pages shown when a page cannot be shown: the address names nothing
// (not found), it names something this account has no entry to (no
// access), the UI itself failed drawing it (crashed), or rendimiento does
// not answer (unreachable). Each says what happened, why it may have, and
// what to do, with a way out. Each one shown is reported once to the
// Problems page (POST /api/fallas), with the page and where the person
// came from, never who they are, so a broken link is seen the same day.
import { Component, useEffect, useRef, useState, type ErrorInfo, type ReactNode } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { api, ApiError } from "../api";
import { ErrorBox } from "./ui";
import { Dibujo } from "./navegacion";
import { t } from "../i18n";

export type ClaseFalla = "not_found" | "forbidden" | "crashed" | "unreachable";

// What the frame knows (set by FallaFrontera): the version that answers,
// the organization the person works in, and the in-app route before this one.
const contexto: { version?: string; org?: string; actual?: string; anterior?: string } = {};

/** The in-app route the person came from, if they came from one. */
export function vinoDe(): string | undefined {
  return contexto.anterior;
}

/** Which full page an error of a page's main load calls for; undefined: a small alert is enough. */
export function claseDe(error: unknown): Exclude<ClaseFalla, "crashed"> | undefined {
  if (error instanceof ApiError) {
    if (error.status === 404) return "not_found";
    if (error.status === 403) return "forbidden";
    if (error.status === 502 || error.status === 503 || error.status === 504) return "unreachable";
    return undefined;
  }
  // fetch rejects with a TypeError when nothing answered (offline, DNS, connection refused).
  if (error instanceof TypeError) return "unreachable";
  return undefined;
}

const enviadas = new Set<string>();

/** Reports a page shown, once per kind and page in this load of the UI; true when it was recorded. */
export async function reportarFalla(kind: ClaseFalla, extra: { message?: string; stack?: string; code?: string } = {}): Promise<boolean> {
  const path = location.pathname.slice(0, 300);
  const clave = `${kind} ${path}`;
  if (enviadas.has(clave)) return false;
  enviadas.add(clave);
  const cuerpo = {
    kind, path, from: contexto.anterior?.slice(0, 300), version: contexto.version?.slice(0, 40),
    message: extra.message?.slice(0, 300), stack: extra.stack?.slice(0, 1000), code: extra.code,
  };
  try {
    const r = await fetch("/api/fallas", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(cuerpo), credentials: "same-origin", keepalive: true });
    return r.ok;
  } catch {
    return false; // not recorded; the page still shows
  }
}

/** Reports the page once, when it shows. */
function useReporte(kind: ClaseFalla) {
  useEffect(() => { void reportarFalla(kind); }, [kind]);
}

const trazos = {
  // A house with a line through it: nothing lives at this address.
  no_existe: "M4 20V9l8-5 8 5v11M3 20h18M9 20v-6h6v6M3 3l18 18",
  // A padlock.
  sin_permiso: "M5 11h14v10H5zM8 11V7a4 4 0 0 1 8 0v4M12 15v2.5",
  // A church bell with a line through it: nobody rings back.
  no_responde: "M7.5 17c1.5-2 1.5-5 1.5-7.5a3 3 0 0 1 6 0c0 2.5 0 5.5 1.5 7.5M6.5 17h11M5.5 4h13M12 4v2.5M12 17v2M4 21L20 5",
};

/** An error page's drawing, in one line, in the accent's color. */
function DibujoFalla({ de }: { de: keyof typeof trazos | "fallo" }) {
  if (de === "fallo") return <span className="falla-dibujo"><Dibujo de="problemas" /></span>;
  return (
    <span className="falla-dibujo">
      <svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
        <path d={trazos[de]} />
      </svg>
    </span>
  );
}

/** The frame of the four pages: drawing, title, why, what is known, the ways out, and a small code line. */
function FallaPagina({ clase, dibujo, titulo, porque, sabido, botones, codigo }: {
  clase: ClaseFalla; dibujo: ReactNode; titulo: string; porque: string; sabido: ReactNode[]; botones: ReactNode; codigo: string;
}) {
  return (
    <section className="falla-pagina" data-falla={clase} role={clase === "crashed" || clase === "unreachable" ? "alert" : undefined}>
      {dibujo}
      <h1>{titulo}</h1>
      <p className="falla-porque">{porque}</p>
      {sabido.length > 0 && <ul className="falla-sabido">{sabido.map((s, i) => <li key={i}>{s}</li>)}</ul>}
      <div className="falla-botones">{botones}</div>
      <span className="falla-codigo">{codigo}</span>
    </section>
  );
}

/** "Back": to the page before, or to Apps when this is the first page of the visit. */
function Volver({ primario }: { primario?: boolean }) {
  const navigate = useNavigate();
  return (
    <button type="button" className={primario ? "btn primary" : undefined}
      onClick={() => (contexto.anterior ? navigate(-1) : navigate("/"))}>{t("Back")}</button>
  );
}

/** Where the person came from, as a link back. */
function VinoDe() {
  const de = vinoDe();
  if (!de) return null;
  return <>{t("You came from")} <Link to={de} className="mono">{de}</Link>.</>;
}

// The pages a person may have meant, for an address that names none.
const paginas = ["/universe", "/numeros", "/services", "/addons", "/environment", "/problems", "/ruta", "/bitacora", "/senales", "/ia", "/buzon", "/escribir", "/members", "/mercado", "/new"];

/** Edit distance between two short strings. */
function distancia(a: string, b: string): number {
  let antes = Array.from({ length: b.length + 1 }, (_, i) => i);
  for (let i = 1; i <= a.length; i++) {
    const fila = [i];
    for (let j = 1; j <= b.length; j++) fila[j] = Math.min(antes[j] + 1, fila[j - 1] + 1, antes[j - 1] + (a[i - 1] === b[j - 1] ? 0 : 1));
    antes = fila;
  }
  return antes[b.length];
}

/** The closest of names to a name, if close enough to be a typo (or one inside the other); never the name itself. */
export function parecido(nombre: string, nombres: string[]): string | undefined {
  const n = nombre.toLowerCase();
  let mejor: string | undefined, d = Infinity;
  for (const c of nombres) {
    const x = c.toLowerCase();
    if (x === n) return undefined; // it exists: what is missing is further down the address
    const e = distancia(n, x);
    const tope = Math.max(1, Math.min(2, Math.floor(Math.max(n.length, x.length) / 4)));
    if ((e <= tope || (n.length >= 4 && x.length >= 4 && (x.includes(n) || n.includes(x)))) && e < d) { mejor = c; d = e; }
  }
  return mejor;
}

/** What the person may have meant: an app with a close name, the app itself when only its run is missing, or a page. */
function useSugerencia(): { to: string; texto: ReactNode } | undefined {
  const { pathname } = useLocation();
  const [s, setS] = useState<{ to: string; texto: ReactNode }>();
  useEffect(() => {
    setS(undefined);
    let vivo = true;
    const app = /^\/apps\/([^/]+)(\/.*)?$/.exec(pathname);
    if (app) {
      const nombre = decodeURIComponent(app[1]);
      api.apps().then((apps) => {
        if (!vivo) return;
        const nombres = apps.map((a) => a.name);
        if (nombres.includes(nombre)) {
          // The app is there; what it names under it is not (a run, a tab).
          if (app[2]) setS({ to: `/apps/${app[1]}`, texto: <>{t("The app {name} is there:", { name: nombre })} <Link to={`/apps/${app[1]}`}>{t("open it")}</Link>.</> });
          return;
        }
        const otra = parecido(nombre, nombres);
        if (otra) setS({ to: `/apps/${encodeURIComponent(otra)}`, texto: <>{t("Were you looking for")} <Link to={`/apps/${encodeURIComponent(otra)}`}><b>{otra}</b></Link>? {t("There is an app with that name.")}</> });
      }, () => {});
    } else {
      const primera = "/" + (pathname.split("/")[1] ?? "");
      const otra = parecido(primera, paginas);
      if (otra) setS({ to: otra, texto: <>{t("Were you looking for")} <Link to={otra} className="mono">{otra}</Link>?</> });
    }
    return () => { vivo = false; };
  }, [pathname]);
  return s;
}

/** This page does not exist: an address no route has, or a thing (an app, a run, a request) the API does not have. */
export function NoExiste() {
  useReporte("not_found");
  const sugerencia = useSugerencia();
  const { pathname } = useLocation();
  const sabido: ReactNode[] = [];
  if (sugerencia) sabido.push(sugerencia.texto);
  if (vinoDe()) sabido.push(<VinoDe />);
  return (
    <FallaPagina clase="not_found" dibujo={<DibujoFalla de="no_existe" />}
      titulo={t("This page does not exist")}
      porque={t("The link may be old, the app may have changed its name, or a letter may be missing in the address.")}
      sabido={sabido}
      botones={<><Link to="/" className="btn primary">{t("Go to Apps")}</Link><Volver /></>}
      codigo={`404 · ${pathname}`} />
  );
}

/** No access: what the page is about exists, but this account has no entry to it. */
export function SinPermiso({ mensaje }: { mensaje?: string }) {
  useReporte("forbidden");
  const { pathname } = useLocation();
  const sabido: ReactNode[] = [];
  if (mensaje) sabido.push(mensaje);
  if (contexto.org) sabido.push(<>{t("You are in the organization")} <b>{contexto.org}</b>. {t("An administrator of the organization it belongs to can give you access.")}</>);
  else sabido.push(t("An administrator of the organization it belongs to can give you access."));
  return (
    <FallaPagina clase="forbidden" dibujo={<DibujoFalla de="sin_permiso" />}
      titulo={t("You have no access to this page")}
      porque={t("It exists, but this account has no entry to it. It is not a fault of rendimiento.")}
      sabido={sabido}
      botones={<><Volver primario /><Link to="/" className="btn">{t("Go to Apps")}</Link></>}
      codigo={`403 · ${pathname}`} />
  );
}

/** Seconds since a moment, ticking every second. */
function useSegundos(desde: number) {
  const [ahora, setAhora] = useState(Date.now());
  useEffect(() => {
    const i = setInterval(() => setAhora(Date.now()), 1000);
    return () => clearInterval(i);
  }, []);
  return Math.max(0, Math.round((ahora - desde) / 1000));
}

/**
 * rendimiento does not answer: it asks again by itself every 5 seconds and,
 * once it answers, goes back to the page (reintentar, or a reload) and
 * reports that it had not answered (it could not while it did not).
 */
export function NoResponde({ reintentar }: { reintentar?: () => void }) {
  const desde = useRef(Date.now()).current;
  const s = useSegundos(desde);
  const [probando, setProbando] = useState(false);
  const probar = useRef<() => void>(() => {});
  probar.current = () => {
    setProbando(true);
    fetch("/healthz", { cache: "no-store", credentials: "same-origin" }).then((r) => {
      if (!r.ok) return setProbando(false);
      void reportarFalla("unreachable");
      if (reintentar) { setProbando(false); reintentar(); } else location.reload();
    }, () => setProbando(false));
  };
  useEffect(() => {
    const i = setInterval(() => probar.current(), 5000);
    return () => clearInterval(i);
  }, []);
  return (
    <FallaPagina clase="unreachable" dibujo={<DibujoFalla de="no_responde" />}
      titulo={t("rendimiento does not answer")}
      porque={t("It may be your connection, or the platform may be updating: that takes less than a minute.")}
      sabido={[t("It tries again by itself every 5 seconds, and comes back to this page when it answers.")]}
      botones={<button type="button" className="btn primary" disabled={probando} onClick={() => probar.current()}>{probando ? t("Trying…") : t("Try now")}</button>}
      codigo={t("no answer · {n} s ago", { n: s })} />
  );
}

/**
 * A page's main load that failed, as the page it calls for: not found, no
 * access, or rendimiento does not answer (which retries with reintentar).
 * Any other error is the small red alert, as before.
 */
export function Falla({ error, reintentar }: { error: unknown; reintentar?: () => void }) {
  if (!error) return null;
  switch (claseDe(error)) {
    case "not_found": return <NoExiste />;
    case "forbidden": return <SinPermiso mensaje={error instanceof Error ? error.message : undefined} />;
    case "unreachable": return <NoResponde reintentar={reintentar} />;
    default: return <ErrorBox error={error} />;
  }
}

/** A short random code that ties what the person saw to what was recorded. */
function codigoCorto() {
  const b = new Uint8Array(2);
  crypto.getRandomValues(b);
  return [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
}

/** The top of a component stack: its first few lines, trimmed. */
function cima(pila: string | null | undefined) {
  return (pila ?? "").split("\n").map((l) => l.trim()).filter(Boolean).slice(0, 6).join("\n").slice(0, 1000);
}

/** Something failed showing this page: the UI threw while drawing it. */
function FallaAqui({ codigo, anotada, reintentar }: { codigo: string; anotada: boolean; reintentar: () => void }) {
  const version = contexto.version ? t("version {v}", { v: contexto.version }) : "";
  const linea = [t("failure"), version, codigo].filter(Boolean).join(" · ");
  const texto = t("A page failed to show: {path} ({code}). What I was doing:", { path: location.pathname, code: linea });
  const sabido: ReactNode[] = [];
  if (anotada) sabido.push(t("It was noted on the Problems page, with the page and the version."));
  sabido.push(t("The rest of rendimiento keeps working."));
  return (
    <FallaPagina clase="crashed" dibujo={<DibujoFalla de="fallo" />}
      titulo={t("Something failed showing this page")}
      porque={t("It is a fault of rendimiento, not yours. What you were doing on the server was not lost.")}
      sabido={sabido}
      botones={<>
        <button type="button" className="btn primary" onClick={reintentar}>{t("Try again")}</button>
        <Link to={`/escribir?texto=${encodeURIComponent(texto)}`} className="btn">{t("Tell the team")}</Link>
      </>}
      codigo={linea} />
  );
}

type EstadoFrontera = { error?: Error; codigo: string; anotada: boolean };

/** Catches what the pages throw while drawing, so the frame (top bar, menu) stays and the page says so. */
class Frontera extends Component<{ clave: string; children: ReactNode }, EstadoFrontera> {
  state: EstadoFrontera = { codigo: "", anotada: false };

  static getDerivedStateFromError(e: unknown): Partial<EstadoFrontera> {
    return { error: e instanceof Error ? e : new Error(String(e)), codigo: codigoCorto(), anotada: false };
  }

  componentDidCatch(e: unknown, info: ErrorInfo) {
    const codigo = this.state.codigo || codigoCorto();
    const message = e instanceof Error ? `${e.name}: ${e.message}` : String(e);
    void reportarFalla("crashed", { message, stack: cima(info.componentStack), code: codigo }).then((ok) => {
      if (ok && this.state.error) this.setState({ anotada: true });
    });
  }

  componentDidUpdate(antes: { clave: string }) {
    // Going to another page leaves the failure behind.
    if (antes.clave !== this.props.clave && this.state.error) this.setState({ error: undefined, anotada: false });
  }

  render() {
    const { error, codigo, anotada } = this.state;
    if (!error) return this.props.children;
    return <FallaAqui codigo={codigo} anotada={anotada} reintentar={() => this.setState({ error: undefined, anotada: false })} />;
  }
}

/**
 * Around the routes: remembers the route the person came from, knows the
 * version and organization for the pages above, and catches a page that
 * fails while drawing (reset when the route changes).
 */
export function FallaFrontera({ version, org, children }: { version?: string; org?: string; children: ReactNode }) {
  const { pathname } = useLocation();
  contexto.version = version;
  contexto.org = org;
  if (contexto.actual !== pathname) {
    if (contexto.actual !== undefined) contexto.anterior = contexto.actual;
    contexto.actual = pathname;
  }
  return <Frontera clave={pathname}>{children}</Frontera>;
}
