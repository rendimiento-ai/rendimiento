// Whether rendimiento answers, as the page knows it: connected; without a
// connection (the browser says so, or the last API calls did not reach it);
// and showing what this browser kept (an answer came from the service
// worker's copy, with the time it was kept: X-Rendimiento-Guardado). The
// notice under the top bar reads it, and so do the actions that change
// something: they are switched off without a connection, never queued.
import { useSyncExternalStore } from "react";

export interface Conexion {
  /** No connection: the browser says it is offline, an API call did not arrive, or kept data is on screen. */
  sin: boolean;
  /** When the oldest kept answer on screen was kept (ms), while kept answers are shown. */
  guardado?: number;
}

const SELLO = "X-Rendimiento-Guardado";
const PRUEBA_MS = 5000;

let fallo = false; // the last API call did not reach rendimiento
// The browser says it is offline, but rendimiento answers: it runs on this computer (one computer, the desktop window).
let aqui = false;
let guardado: number | undefined;
let estado: Conexion = calcular();
const oyentes = new Set<() => void>();
let prueba: ReturnType<typeof setInterval> | undefined;

function enLinea() {
  return typeof navigator === "undefined" || navigator.onLine !== false;
}

function calcular(): Conexion {
  return { sin: (!enLinea() && !aqui) || fallo || guardado !== undefined, guardado };
}

function cambio() {
  const nuevo = calcular();
  if (nuevo.sin !== estado.sin || nuevo.guardado !== estado.guardado) {
    const volvio = estado.sin && !nuevo.sin;
    estado = nuevo;
    oyentes.forEach((f) => f());
    if (volvio) window.dispatchEvent(new Event(DE_VUELTA));
  }
  // While it is missing, ask /healthz now and then (the worker never answers it): pages that do not poll catch up too.
  if (estado.sin && !prueba) prueba = setInterval(() => void probar(), PRUEBA_MS);
  if (!estado.sin && prueba) {
    clearInterval(prueba);
    prueba = undefined;
  }
}

/** The event sent when the connection is back: usePoll loads its page again. */
export const DE_VUELTA = "rendimiento:de-vuelta";

/** An API answer arrived: from rendimiento (live), or the worker's copy (with the time it was kept). */
export function respuesta(res: Response) {
  const sello = res.headers.get(SELLO);
  if (sello) {
    const cuando = Date.parse(sello);
    if (!Number.isNaN(cuando)) guardado = guardado === undefined ? cuando : Math.min(guardado, cuando);
    fallo = true;
  } else if (res.status >= 502 && res.status <= 504 && !(res.headers.get("Content-Type") ?? "").includes("json")) {
    fallo = true; // the proxy in front answered, not rendimiento (its errors are JSON): it may be restarting
  } else {
    fallo = false;
    guardado = undefined;
  }
  cambio();
}

/** An API call did not reach rendimiento (fetch rejected). */
export function falloDeRed() {
  fallo = true;
  cambio();
}

/** Asks rendimiento whether it answers (within ms); true and the notice goes when it does. */
export async function probar(ms = 4000): Promise<boolean> {
  const ctl = new AbortController();
  const tope = setTimeout(() => ctl.abort(), ms);
  try {
    const r = await fetch("/healthz", { cache: "no-store", credentials: "same-origin", signal: ctl.signal });
    if (!r.ok) return false;
    aqui = !enLinea();
    fallo = false;
    guardado = undefined;
    cambio();
    return true;
  } catch {
    return false;
  } finally {
    clearTimeout(tope);
  }
}

/** Whether there is no connection now. */
export function sinConexion(): boolean {
  return estado.sin;
}

if (typeof window !== "undefined") {
  window.addEventListener("online", () => { aqui = false; cambio(); void probar(); });
  window.addEventListener("offline", () => { aqui = false; cambio(); void probar(); });
  if (!enLinea()) void probar();
}

/** The connection, for a component: it draws again when it changes. */
export function useConexion(): Conexion {
  return useSyncExternalStore(
    (f) => { oyentes.add(f); return () => { oyentes.delete(f); }; },
    () => estado,
  );
}
