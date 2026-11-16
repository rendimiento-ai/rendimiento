/**
 * The layout of a run's steps as threads (RunPage's «the threads»), without the DOM: where each step's thread starts and
 * ends, and on which row. A step's level is the longest chain of dependsOn before it (0 for a step that waits for
 * nobody); every level is a column, and a level starts where the longest thread of the previous one ends (plus room for
 * its duration and the knot). The rows of a level are centered on the tallest level, so a chain stays on one line and a
 * step that waits for several sits between them. A thread's length is its duration, at one scale for the whole run.
 */

/** A step as the layout sees it: seconds it has run (so far), or undefined when it has not started. */
export interface Hebra {
  id: string;
  dependsOn: string[];
  segundos?: number;
}

/** A step placed: its thread from (x0, y) to (x1, y); deps are the steps whose threads knot at (x0, y). */
export interface Hilo {
  id: string;
  nivel: number;
  x0: number;
  x1: number;
  y: number;
  /** The steps it waits for that are in this run (empty: its thread leaves from the change). */
  deps: string[];
  /** It has not started: a fixed, short thread. */
  corto: boolean;
}

export interface Tejido {
  hilos: Hilo[];
  /** The change: where the threads of level 0 leave from. */
  origen: { x: number; y: number };
  /** Where each level's threads start. */
  niveles: number[];
  ancho: number;
  alto: number;
}

export const FILA = 40; // between rows
const ARRIBA = 30; // above the first row: its id
const ORIGEN_X = 22;
const SALIDA = 64; // from the change to where the threads of level 0 start
const LARGO = 520; // the longest chain's threads, together
const MINIMO = 14; // a thread that took a second is still seen
const CORTO = 44; // a thread not started
const DURACION = 78; // room after a bead for its duration
const NUDO = 34; // room before a knot for the dotted lines to come together
const LETRA = 7.2; // a character of an id, roughly, at the threads' size

/** Each step's level: the longest chain of dependsOn (inside the run) before it. A cycle counts as no dependency. */
export function niveles(pasos: Hebra[]): Map<string, number> {
  const por = new Map(pasos.map((p) => [p.id, p]));
  const nivel = new Map<string, number>();
  const viendo = new Set<string>();
  const de = (id: string): number => {
    const ya = nivel.get(id);
    if (ya !== undefined) return ya;
    if (viendo.has(id)) return -1;
    viendo.add(id);
    let n = 0;
    for (const d of por.get(id)?.dependsOn ?? []) if (por.has(d)) n = Math.max(n, de(d) + 1);
    viendo.delete(id);
    nivel.set(id, n);
    return n;
  };
  pasos.forEach((p) => de(p.id));
  return nivel;
}

export function tejer(pasos: Hebra[]): Tejido {
  const nivel = niveles(pasos);
  const cuantos = pasos.length ? Math.max(...nivel.values()) + 1 : 0;
  const porNivel: Hebra[][] = Array.from({ length: cuantos }, () => []);
  pasos.forEach((p) => porNivel[nivel.get(p.id)!].push(p));

  // One scale for the run: the longest thread of every level, end to end, fills LARGO.
  const mas = porNivel.map((ps) => Math.max(0, ...ps.map((p) => p.segundos ?? 0)));
  const escala = LARGO / Math.max(1, mas.reduce((a, b) => a + b, 0));
  const largo = (p: Hebra) => (p.segundos === undefined ? CORTO : Math.max(MINIMO, p.segundos * escala));

  const filas = Math.max(1, ...porNivel.map((ps) => ps.length));
  const centro = ARRIBA + ((filas - 1) * FILA) / 2;
  const y = new Map<string, number>();
  const hilos: Hilo[] = [];
  const inicios: number[] = [];
  let x = ORIGEN_X + SALIDA;
  porNivel.forEach((ps, n) => {
    // Inside a level, by the rows of what each waits for (fewer crossings), then as the run lists them.
    const orden = ps
      .map((p, i) => {
        const ds = p.dependsOn.filter((d) => y.has(d));
        return { p, i, ds, peso: ds.length ? ds.reduce((a, d) => a + y.get(d)!, 0) / ds.length : 0 };
      })
      .sort((a, b) => a.peso - b.peso || a.i - b.i);
    const x0 = n === 0 ? x : x + NUDO;
    inicios.push(x0);
    let fin = x0;
    orden.forEach(({ p, ds }, j) => {
      const yy = centro + (j - (orden.length - 1) / 2) * FILA;
      y.set(p.id, yy);
      const x1 = x0 + largo(p);
      hilos.push({ id: p.id, nivel: n, x0, x1, y: yy, deps: ds, corto: p.segundos === undefined });
      // What follows starts after this bead's duration and after this step's id, whichever is further.
      fin = Math.max(fin, x1 + DURACION, x0 + p.id.length * LETRA + 12);
    });
    x = fin;
  });
  // Back in the run's order, so each step keeps its place (and its color).
  const pos = new Map(pasos.map((p, i) => [p.id, i]));
  hilos.sort((a, b) => pos.get(a.id)! - pos.get(b.id)!);
  return {
    hilos,
    origen: { x: ORIGEN_X, y: centro },
    niveles: inicios,
    ancho: Math.ceil(x + 8),
    alto: Math.ceil(Math.max(ARRIBA + (filas - 1) * FILA + 22, centro + 40)),
  };
}
