/// <reference types="vite/client" />
// The page's side of the service worker (sw.ts): registers it, tells it who
// is signed in (so one person's kept answers are never shown to the next),
// and says when a new version of rendimiento is waiting, to reload when the
// person chooses, never by itself.
import { useSyncExternalStore } from "react";

const OPTAR = "rendimiento.trabajador"; // "si": the browser tests written for the worker register it

let esperando: ServiceWorker | undefined;
let recargar = false;
const oyentes = new Set<() => void>();
const avisar = () => oyentes.forEach((f) => f());

/** The page's own version: its entry file, as the worker's (construir.ts). */
function miVersion(): string | undefined {
  const s = document.querySelector<HTMLScriptElement>('script[type="module"][src*="/assets/"]');
  return s ? new URL(s.src, location.href).pathname : undefined;
}

/** The waiting worker's version, or undefined if it does not say within a moment. */
function suVersion(w: ServiceWorker): Promise<string | undefined> {
  return new Promise((listo) => {
    const canal = new MessageChannel();
    const tope = setTimeout(() => listo(undefined), 2000);
    canal.port1.onmessage = (e) => { clearTimeout(tope); listo(typeof e.data === "string" ? e.data : undefined); };
    w.postMessage({ tipo: "version" }, [canal.port2]);
  });
}

/** A new worker installed and waits for this page to let it in. */
async function espera(w: ServiceWorker) {
  if (!navigator.serviceWorker.controller) return; // the first one takes over by itself
  // The page is already that version (it reloaded after Environment → Update): let it in quietly.
  const suya = await suVersion(w);
  if (suya && suya === miVersion()) {
    w.postMessage({ tipo: "saltar" });
    return;
  }
  esperando = w;
  avisar();
}

function vigilar(w: ServiceWorker | null) {
  if (!w) return;
  if (w.state === "installed") void espera(w);
  else w.addEventListener("statechange", () => { if (w.state === "installed") void espera(w); });
}

/** Whether this page may have the worker: a built page, on https or this computer, not a browser test that did not ask. */
function puede(): boolean {
  if (!import.meta.env.PROD || !("serviceWorker" in navigator) || !window.isSecureContext) return false;
  if (navigator.webdriver) {
    try {
      return localStorage.getItem(OPTAR) === "si";
    } catch {
      return false;
    }
  }
  return true;
}

/** Throws away the worker and what it kept (it is turned off on the server). */
async function retirar() {
  for (const r of await navigator.serviceWorker.getRegistrations()) await r.unregister();
  for (const n of await caches.keys()) if (n.startsWith("rendimiento-")) await caches.delete(n);
}

/** Registers the worker, once the page has loaded. Failures are quiet: the page works the same without it. */
export function registrarTrabajador() {
  if (!puede()) return;
  navigator.serviceWorker.addEventListener("controllerchange", () => {
    decirQuien(); // a worker that just took over has not been told
    if (!recargar) return;
    recargar = false;
    location.reload();
  });
  const empezar = async () => {
    // The server can turn it off (SERVICE_WORKER=off): then its /sw.js says so.
    const r = await fetch("/sw.js", { method: "HEAD", cache: "no-store" }).catch(() => undefined);
    if (r?.headers.get("X-Rendimiento-Trabajador") === "retirado") return retirar();
    if (r && !r.ok) return;
    const reg = await navigator.serviceWorker.register("/sw.js", { scope: "/", updateViaCache: "none" });
    if (reg.waiting) void espera(reg.waiting);
    vigilar(reg.installing);
    reg.addEventListener("updatefound", () => vigilar(reg.installing));
    // A page left open for days still hears of a new version.
    const mirar = () => { if (document.visibilityState === "visible") reg.update().catch(() => undefined); };
    setInterval(mirar, 30 * 60000);
    document.addEventListener("visibilitychange", mirar);
    window.addEventListener("online", mirar);
  };
  const ir = () => void empezar().catch(() => undefined);
  if (document.readyState === "complete") ir();
  else window.addEventListener("load", ir, { once: true });
}

/** Tells the worker who is signed in, and in which organization: a change throws the kept answers away. */
export function quienEs(login: string, org?: string) {
  clave = `${login}\n${org ?? ""}`;
  decirQuien();
}

let clave: string | undefined;

/** The login signed in, as told by the page (empty before, or signed out). */
export function quien(): string {
  return clave?.split("\n")[0] ?? "";
}
function decirQuien() {
  if (clave) navigator.serviceWorker?.controller?.postMessage({ tipo: "quien", clave });
}

/** Signing out: the worker throws away what it kept. */
export function olvidarGuardado() {
  clave = undefined;
  navigator.serviceWorker?.controller?.postMessage({ tipo: "olvidar" });
}

/** Lets the waiting version in and reloads the page once it took over. */
export function recargarNueva() {
  if (!esperando) return location.reload();
  recargar = true;
  esperando.postMessage({ tipo: "saltar" });
  // If it does not take over (another tab let it in already), reload anyway.
  setTimeout(() => { if (recargar) location.reload(); }, 3000);
}

/** Whether a new version of rendimiento waits for the person to reload. */
export function useNuevaVersion(): boolean {
  return useSyncExternalStore(
    (f) => { oyentes.add(f); return () => { oyentes.delete(f); }; },
    () => esperando !== undefined,
  );
}
