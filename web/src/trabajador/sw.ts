// The service worker: what lets the page open without a connection, or
// while rendimiento restarts in a new version, from what this browser kept.
//
// It is written here as one function so tsc checks it with the rest, and
// the build (trabajador/construir.ts) writes it to dist/sw.js as
// `(<this function>)(<the build's files>)`. So it must not use anything
// from outside its own body: no imports, no constants of this file.
//
// What it does:
// - the page (index.html, the manifest, the icons) and the code of the
//   first screen are kept when it installs; any other file under /assets/
//   is kept the first time it is used (they never change: their names
//   carry their hash). Only the current build's files stay.
// - opening a page asks rendimiento first, and waits for it a few seconds;
//   without an answer (or with a 502-504 while it restarts) it opens the
//   page it kept.
// - the API's GETs ask rendimiento first; each 200 answer is kept with
//   the time, and without a connection the copy answers, marked with the
//   X-Rendimiento-Guardado header (the time it was kept), which the page
//   reads (conexion.ts) to say how old what it shows is. "Without a
//   connection" is: the request failed, the proxy answered 502-504 while
//   rendimiento restarts, or no answer came in 8 s while the browser says
//   it is offline. Online, a slow answer is waited for up to 30 s: a slow
//   page is not an offline one (it said so, and the notice blinked).
// What it never does: keep or answer anything that is not a GET (an order
// never waits here to leave later), event streams, logs, sign-in, /mcp,
// /healthz, the signals, other sites' files, answers that are not 200 or
// say no-store. The API's copies are at most 200 and 7 days old, and are
// thrown away when the person signs out, or when whoever signed in changes
// (the page says who, with a message), or when an answer is a 401.

/** What the build tells the worker: its version (the page's entry file) and the files to keep. */
export interface DatosTrabajador {
  version: string; // "/assets/index-<hash>.js": the page knows its own and compares
  fijos: string[]; // the page's other files at the root (manifest, icons)
  inicio: string[]; // the first screen's code and styles
  todos: string[]; // every file of the build under /assets/
}

interface Evento { waitUntil(p: Promise<unknown>): void }
interface EventoFetch extends Evento { request: Request; respondWith(r: Response | Promise<Response>): void }
interface EventoMensaje extends Evento { data: unknown; ports: readonly MessagePort[] }
interface Ambito {
  location: Location;
  navigator: { onLine: boolean };
  clients: { claim(): Promise<void> };
  skipWaiting(): Promise<void>;
  addEventListener(tipo: "install" | "activate", f: (e: Evento) => void): void;
  addEventListener(tipo: "fetch", f: (e: EventoFetch) => void): void;
  addEventListener(tipo: "message", f: (e: EventoMensaje) => void): void;
}

export function trabajador(datos: DatosTrabajador) {
  const sw = self as unknown as Ambito;
  const PAGINA = "rendimiento-pagina-" + datos.version;
  const RECURSOS = "rendimiento-recursos";
  const API = "rendimiento-api";
  const QUIEN = "rendimiento-quien";
  const SELLO = "X-Rendimiento-Guardado";
  const MAXIMO = 200;
  const EDAD_MS = 7 * 24 * 3600 * 1000;
  const ESPERA_PAGINA_MS = 3000;
  const ESPERA_API_MS = 8000; // the browser says it is offline
  const ESPERA_LENTA_MS = 30000; // it says it is online: rendimiento is only slow
  const actuales = new Set(datos.todos);

  // Paths whose page is the server's, not the UI's: never answered with the kept page.
  const delServidor = /^\/(api|mcp|healthz|demo|instalar|llms\.txt|sw\.js)(\/|$)/;

  const pedir = (url: string) => new Request(url, { cache: "reload", credentials: "same-origin" });

  sw.addEventListener("install", (e) => {
    e.waitUntil((async () => {
      const pagina = await caches.open(PAGINA);
      const inicio = await fetch(pedir("/"));
      const html = inicio.ok ? await inicio.clone().text() : "";
      // During an update both versions answer for a few seconds: keep the page only if it is this build's.
      if (!html.includes(datos.version)) throw new Error("the page is not this build's: try again later");
      await pagina.put("/", inicio);
      await pagina.addAll(datos.fijos.map(pedir));
      const recursos = await caches.open(RECURSOS);
      await recursos.addAll(datos.inicio.map(pedir));
    })());
  });

  sw.addEventListener("activate", (e) => {
    e.waitUntil((async () => {
      for (const nombre of await caches.keys()) {
        if (nombre.startsWith("rendimiento-pagina-") && nombre !== PAGINA) await caches.delete(nombre);
      }
      // Only this build's files stay: an older one's are never asked for again.
      const recursos = await caches.open(RECURSOS);
      for (const r of await recursos.keys()) {
        if (!actuales.has(new URL(r.url).pathname)) await recursos.delete(r);
      }
      await sw.clients.claim();
    })());
  });

  /** Throws the API's copies away, and who they were for. */
  const olvidar = async () => {
    await caches.delete(API);
    await caches.delete(QUIEN);
  };

  sw.addEventListener("message", (e) => {
    const m = (e.data ?? {}) as { tipo?: string; clave?: string };
    if (m.tipo === "saltar") e.waitUntil(sw.skipWaiting());
    else if (m.tipo === "version") e.ports[0]?.postMessage(datos.version);
    else if (m.tipo === "olvidar") e.waitUntil(olvidar());
    else if (m.tipo === "quien" && typeof m.clave === "string") {
      const clave = m.clave;
      e.waitUntil((async () => {
        const quien = await caches.open(QUIEN);
        const antes = await quien.match("/quien");
        if (antes && (await antes.text()) === clave) return;
        // Someone else signed in (or another organization was chosen): the copies were not theirs.
        await caches.delete(API);
        await quien.put("/quien", new Response(clave));
      })());
    }
  });

  /** A fetch that gives up after ms (undefined: the network did not answer in time). */
  const aTiempo = (p: Promise<Response>, ms: number) =>
    Promise.race([p, new Promise<undefined>((ok) => setTimeout(() => ok(undefined), ms))]);

  // The proxy in front answers 502-504 while rendimiento restarts; rendimiento's own errors are JSON, and shown.
  const reiniciando = (r: Response) => r.status >= 502 && r.status <= 504 && !(r.headers.get("Content-Type") ?? "").includes("json");

  /** Opening a page: rendimiento's, if it answers in a few seconds; else the one kept. */
  const abrir = async (req: Request) => {
    const red = fetch(req);
    red.catch(() => undefined); // its failure is handled below
    const guardada = await caches.match("/", { cacheName: PAGINA });
    if (!guardada) return red;
    try {
      const r = await aTiempo(red, ESPERA_PAGINA_MS);
      if (r && !reiniciando(r)) return r;
    } catch {
      /* no connection */
    }
    return guardada;
  };

  /** A file under /assets/: never changes, so the copy if there is one; else the network, kept if it is a 200. */
  const recurso = async (req: Request) => {
    const cache = await caches.open(RECURSOS);
    const copia = await cache.match(req);
    if (copia) return copia;
    const r = await fetch(req);
    if (r.status === 200 && r.type === "basic" && actuales.has(new URL(req.url).pathname)) await cache.put(req, r.clone());
    return r;
  };

  /** An API GET that may be kept: not a stream, not a log, not sign-in or signals. */
  const guardable = (req: Request, url: URL) => {
    const p = url.pathname;
    if (/^\/api\/(auth|senales|webhooks|setup\/github)(\/|$)/.test(p)) return false;
    if (/\/(events|stream|log|logs)$/.test(p)) return false;
    if ((req.headers.get("Accept") ?? "").includes("text/event-stream")) return false;
    if (req.cache === "no-store") return false;
    return true;
  };

  const guardar = async (req: Request, r: Response) => {
    if (r.status !== 200 || r.type !== "basic") return;
    if (/no-store/i.test(r.headers.get("Cache-Control") ?? "")) return;
    if (/text\/event-stream/i.test(r.headers.get("Content-Type") ?? "")) return;
    const headers = new Headers(r.headers);
    headers.set(SELLO, new Date().toISOString());
    const copia = new Response(await r.blob(), { status: 200, statusText: r.statusText, headers });
    const cache = await caches.open(API);
    await cache.delete(req); // so it moves to the end: the oldest go first
    await cache.put(req, copia);
    const llaves = await cache.keys();
    for (const vieja of llaves.slice(0, Math.max(0, llaves.length - MAXIMO))) await cache.delete(vieja);
  };

  /** The copy of an API answer, if there is one young enough. */
  const copiaDe = async (req: Request) => {
    const cache = await caches.open(API);
    const r = await cache.match(req);
    if (!r) return undefined;
    const cuando = Date.parse(r.headers.get(SELLO) ?? "");
    if (!(Date.now() - cuando < EDAD_MS)) {
      await cache.delete(req);
      return undefined;
    }
    return r;
  };

  /** An API GET: rendimiento's answer, kept; without one, the copy (if any). */
  const preguntar = async (e: EventoFetch, req: Request) => {
    const red = fetch(req).then(async (r) => {
      if (r.status === 401) await olvidar(); // signed out: what was kept was someone's
      else await guardar(req, r.clone()).catch(() => undefined);
      return r;
    });
    e.waitUntil(red.catch(() => undefined));
    const copia = await copiaDe(req).catch(() => undefined);
    if (!copia) return red; // a failure stays a failure: the page says rendimiento does not answer
    try {
      let r = await aTiempo(red, ESPERA_API_MS);
      if (!r && sw.navigator.onLine !== false) r = await aTiempo(red, ESPERA_LENTA_MS - ESPERA_API_MS);
      if (r && !reiniciando(r)) return r;
    } catch {
      /* no connection */
    }
    return copia;
  };

  sw.addEventListener("fetch", (e) => {
    const req = e.request;
    const url = new URL(req.url);
    if (url.origin !== sw.location.origin) return; // other sites: untouched, never kept
    if (req.method !== "GET") return; // orders go to the network, now or never
    if (req.mode === "navigate") {
      if (!delServidor.test(url.pathname)) e.respondWith(abrir(req));
      return;
    }
    if (url.pathname.startsWith("/assets/")) return e.respondWith(recurso(req));
    if (url.pathname.startsWith("/api/")) {
      if (guardable(req, url)) e.respondWith(preguntar(e, req));
      return;
    }
    if (datos.fijos.includes(url.pathname)) {
      e.respondWith(fetch(req).catch(async () => (await caches.match(url.pathname, { cacheName: PAGINA })) ?? Response.error()));
    }
  });
}

