// How rendimiento's own interface is used: which pages are opened, from
// which, how long they take to show their data, and the main actions. Only
// totals reach the server (internal/api/uso.go): the page's route pattern
// ("/apps/:name/runs/:id", never the app's name or the run's number), the
// pattern it came from, an action's fixed name and a time. Never who, never
// text people typed. Nothing is noted when the platform's administrators
// turned it off, nor in a read-only demo (session.uso is false then).

import { useEffect } from "react";
import { matchRoutes, useLocation } from "react-router-dom";
import { session } from "./api";

/** The interface's route patterns (main.tsx's routes). The server accepts these only; add a page here and in internal/api/uso.go. */
export const RUTAS = [
  "/", "/new", "/mercado", "/mercado/nuevo", "/mercado/:app",
  "/environment", "/universe", "/universe/requests/:id", "/services",
  "/addons", "/problems", "/ruta", "/bitacora", "/senales",
  "/ia", "/buzon", "/escribir", "/members", "/addons/:name",
  "/services/:ns/:name", "/apps/:name", "/apps/:name/:tab",
  "/apps/:name/runs/:id", "/uso",
] as const;

/** The actions noted, by a short fixed name: the closed list (the same in internal/api/uso.go). */
export const ACCIONES = [
  "build_now", "retry", "cancel_run", "merge", "dismiss_problem",
  "ask_agent", "select_step", "switch_theme", "switch_language", "rollback",
] as const;
export type Accion = (typeof ACCIONES)[number];

/** A page the list does not know. */
export const DESCONOCIDA = "(unknown)";

const rutas = RUTAS.map((path) => ({ path }));

/** The route pattern of an address: the router's best match, or "(unknown)". */
export function patronDe(pathname: string): string {
  const m = matchRoutes(rutas, pathname);
  return m?.[m.length - 1]?.route.path ?? DESCONOCIDA;
}

type Evento = { t: "v"; r: string; d: string; ms?: number } | { t: "a"; r: string; a: Accion };

const MAX_COLA = 200; // beyond this, new events are dropped
const POR_ENVIO = 100; // the server takes at most 100 at once
const CADA_MS = 15000;
const TOPE_MS = 60000; // longer than this, a page's time is not noted

let cola: Evento[] = [];
let actual = ""; // the pattern of the page on screen
let ultimaClave = ""; // the router's key of the last navigation noted
let ultimoListo: Element | null = null; // the [data-listo] mark the last measured page set
let pendiente: { r: string; d: string; cerrar: (ms?: number) => void } | null = null;
let arrancado = false;

function poner(e: Evento) {
  if (!session.uso) return;
  if (cola.length < MAX_COLA) cola.push(e);
}

/** Notes a main action on the page on screen. One line where the action is taken: `anotar("build_now")`. */
export function anotar(a: Accion) {
  poner({ t: "a", r: actual || patronDe(location.pathname), a });
}

/** Sends what is queued, in batches; silent when it fails (the use is not worth bothering anyone). */
export function enviarUso() {
  if (!session.uso) {
    cola = [];
    return;
  }
  while (cola.length > 0) {
    const lote = cola.splice(0, POR_ENVIO);
    fetch("/api/uso", {
      method: "POST",
      keepalive: true,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ e: lote }),
    }).catch(() => {});
  }
}

/** The page being measured ends: without a time when it was not measured. */
function terminarPendiente() {
  pendiente?.cerrar();
}

/**
 * A page was opened: noted with where it came from, and its time to data, when the page sets a
 * `data-listo` mark on what it shows once its data is in. Pages without the mark send no time.
 */
function vista(r: string) {
  terminarPendiente();
  const d = actual;
  actual = r;
  if (!session.uso) return;
  const t0 = performance.now();
  const antes = ultimoListo;
  let obs: MutationObserver | undefined;
  let reloj: ReturnType<typeof setTimeout> | undefined;
  const p = {
    r,
    d,
    cerrar: (ms?: number) => {
      if (pendiente !== p) return;
      pendiente = null;
      obs?.disconnect();
      if (reloj) clearTimeout(reloj);
      poner(ms === undefined ? { t: "v", r, d } : { t: "v", r, d, ms: Math.min(TOPE_MS, Math.max(0, Math.round(ms))) });
    },
  };
  pendiente = p;
  // A mark already there and the same as the last page's is that page's: not this one's data.
  const mirar = () => {
    const el = document.querySelector("main [data-listo]");
    if (!el || el === antes) return;
    ultimoListo = el;
    p.cerrar(performance.now() - t0);
  };
  if (document.hidden) return p.cerrar(); // a hidden tab is slowed down: its time would be wrong
  mirar();
  if (pendiente !== p) return;
  obs = new MutationObserver(mirar);
  obs.observe(document.querySelector("main") ?? document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ["data-listo"] });
  reloj = setTimeout(() => p.cerrar(), TOPE_MS);
}

function arrancar() {
  if (arrancado) return;
  arrancado = true;
  setInterval(enviarUso, CADA_MS);
  const salir = () => {
    terminarPendiente();
    enviarUso();
  };
  document.addEventListener("visibilitychange", () => { if (document.visibilityState === "hidden") salir(); });
  window.addEventListener("pagehide", salir);
}

/** Follows the pages opened, once the person is signed in (listo). Call it once, in the app's frame. */
export function useUso(listo: boolean) {
  const loc = useLocation();
  useEffect(arrancar, []);
  useEffect(() => {
    if (!listo || loc.key === ultimaClave) return;
    ultimaClave = loc.key;
    vista(patronDe(loc.pathname));
    // Only a new address counts: a search or filter in the query string is the same page.
  }, [listo, loc.pathname]);
}
