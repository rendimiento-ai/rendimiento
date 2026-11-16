// The pages' drawings, of things of the town (a cempasúchil, a clay pot, a
// tiled house…), and the sheet that holds the menu on a phone: it always
// peeks from the bottom with the first pages and the edge of the rest, so
// nothing hides behind a button; its handle raises it to every page, then
// to the account and preferences.
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useLocation } from "react-router-dom";
import { t } from "../i18n";

const f = (n: number) => n.toFixed(2);
const pt = (r: number, a: number) => `${f(12 + r * Math.cos(a))} ${f(12 + r * Math.sin(a))}`;
/** A scalloped ring: n petals on a circle of radius r. */
function olan(n: number, r: number, petalo: number, giro = 0) {
  let d = `M${pt(r, giro)}`;
  for (let i = 1; i <= n; i++) d += `A${petalo} ${petalo} 0 0 1 ${pt(r, giro + (i * 2 * Math.PI) / n)}`;
  return d;
}
/** n points around a circle, from r0 out to r1. */
function picos(n: number, r0: number, r1: number, ancho: number) {
  let d = "";
  for (let i = 0; i < n; i++) {
    const a = -Math.PI / 2 + (i * 2 * Math.PI) / n;
    d += `M${pt(r0, a - ancho)}L${pt(r1, a)}L${pt(r0, a + ancho)}`;
  }
  return d;
}
const circulo = (r: number) => `M${12 - r} 12a${r} ${r} 0 1 0 ${2 * r} 0a${r} ${r} 0 1 0 ${-2 * r} 0`;

const trazos = {
  // A cempasúchil: each app's flower.
  apps: olan(9, 9, 3.3) + olan(6, 5, 2.7, 0.5) + circulo(1.3),
  // The lotería's sun.
  universo: circulo(4.2) + picos(8, 6.2, 10.3, 0.24),
  // A clay pot: what serves the apps.
  servicios: "M8 5h8M9 5c0 2-4 4-4 8a7 7 0 0 0 14 0c0-4-4-6-4-8M5.3 10.6c-2.3-.4-2.3 3.2 0 3.2M18.7 10.6c2.3-.4 2.3 3.2 0 3.2M7 14.5l2-1.6 2 1.6 2-1.6 2 1.6 2-1.6",
  // A piñata with its points: what is added.
  complementos: circulo(3.6) + picos(5, 3.9, 10.5, 0.5),
  // A house with its roof tiles: the town, the machines.
  entorno: "M2.5 10.5 12 4l9.5 6.5M4 11.6q1.35 1.7 2.67 0q1.35 1.7 2.67 0q1.35 1.7 2.66 0q1.35 1.7 2.67 0q1.35 1.7 2.67 0q1.35 1.7 2.66 0M5.5 13.2v7.3h13v-7.3M10 20.5v-4a2 2 0 0 1 4 0v4",
  // The Ruta's monarch.
  ruta: "M12 7.5v10.5M12 7.5c-.5-2-1.5-3-3-3.5M12 7.5c.5-2 1.5-3 3-3.5M12 10C10 5 4 4 3.5 8c-.4 3.5 4 5 8.5 4M12 10c2-5 8-6 8.5-2 .4 3.5-4 5-8.5 4M12 13c-4 0-7 3-5.5 6 1.3 2.3 4.5 0 5.5-4M12 13c4 0 7 3 5.5 6-1.3 2.3-4.5 0-5.5-4",
  // A rolled codex with its fret: what was written.
  bitacora: "M8 6v12a2 2 0 0 1-2 2h10a2 2 0 0 0 2-2V4H8zM8 6a2 2 0 0 0-4 0v2h4M11 8.5h4v3h-2.5M11 15.5h4",
  // A talavera star.
  ia: "M6.3 6.3h11.4v11.4H6.3zM12 3.94l8.06 8.06L12 20.06 3.94 12z" + circulo(1.8),
  // A withered flower, as an app's withers.
  problemas: "M11 21c0-6 0-9 4-11M15 10c2-1 4.5 0 4.5 2.5-1.5 0-2 1.2-2.2 2.5-1.2-.8-2.3-.3-3 .5C13 13.5 13.5 11 15 10zM11 17c-2.5 0-4-1-4.5-3 2.5 0 4 1 4.5 3M5 20.8q1.5-1.5 3 0",
  // The speech scroll of the codices.
  comentarios: "M6 20.5c0-7 3-12.5 8-12.5a4 4 0 1 1 0 8 2.5 2.5 0 1 1 0-5M7 6.5V4.8M10.5 5V3.3",
};
export type Pagina = keyof typeof trazos;

/** A page's drawing, in one line, in the text's color. */
export function Dibujo({ de }: { de: Pagina }) {
  return (
    <svg className="dibujo" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
      <path d={trazos[de]} />
    </svg>
  );
}

/** Whether the screen is a phone's (the sheet instead of the bar's links). */
export function useTelefono() {
  const mq = typeof matchMedia === "function" ? matchMedia("(max-width: 640px)") : undefined;
  const [si, setSi] = useState(!!mq?.matches);
  useEffect(() => {
    if (!mq) return;
    const f = () => setSi(mq.matches);
    mq.addEventListener("change", f);
    return () => mq.removeEventListener("change", f);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  return si;
}

/** How far the sheet is raised: it peeks, every page, or the account too. */
export type Altura = 0 | 1 | 2;

/** The phone's menu: the sheet at the bottom, with the pages and, raised whole, the account and preferences. */
export function Hoja({ altura, poner, paginas, cuenta }: { altura: Altura; poner: (a: Altura) => void; paginas: ReactNode; cuenta: ReactNode }) {
  const { pathname } = useLocation();
  // Going to a page, scrolling it or Escape lowers the sheet.
  useEffect(() => poner(0), [pathname]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (altura === 0) return;
    const bajar = () => poner(0);
    const tecla = (e: KeyboardEvent) => e.key === "Escape" && poner(0);
    window.addEventListener("scroll", bajar, { passive: true });
    window.addEventListener("keydown", tecla);
    return () => { window.removeEventListener("scroll", bajar); window.removeEventListener("keydown", tecla); };
  }, [altura, poner]);
  const que = altura === 0 ? t("Show every page") : altura === 1 ? t("Show your account and preferences") : t("Lower the menu");

  // Dragging the sheet with a finger, from anywhere on it: it follows, and on
  // release settles on the nearest of its three heights (a clear flick moves
  // it one step that way). A touch that did not move is a tap, as before.
  const caja = useRef<HTMLDivElement>(null);
  const gesto = useRef<{ y: number; x: number; alto: number; arrastra: boolean; id: number } | undefined>(undefined);
  const arrastro = useRef(false);
  const alturas = () => {
    const el = caja.current!;
    const prev = el.style.height;
    el.style.height = "";
    const medir = (a: Altura) => { el.dataset.altura = String(a); return el.getBoundingClientRect().height; };
    const hs = [medir(0), medir(1), medir(2)];
    el.dataset.altura = String(altura);
    el.style.height = prev;
    return hs;
  };
  const baja = (e: React.PointerEvent) => {
    if (e.pointerType === "mouse" && e.button !== 0) return;
    arrastro.current = false;
    gesto.current = { y: e.clientY, x: e.clientX, alto: caja.current!.getBoundingClientRect().height, arrastra: false, id: e.pointerId };
  };
  const mueve = (e: React.PointerEvent) => {
    const g = gesto.current;
    const el = caja.current;
    if (!g || !el || e.pointerId !== g.id) return;
    const dy = e.clientY - g.y;
    if (!g.arrastra) {
      if (Math.abs(dy) < 8 || Math.abs(dy) < Math.abs(e.clientX - g.x)) return;
      g.arrastra = true;
      arrastro.current = true;
      try { el.setPointerCapture(g.id); } catch { /* the pointer is gone already: the moves still reach the sheet */ }
      el.classList.add("arrastrando");
    }
    const [min, , max] = alturas();
    el.style.height = `${Math.max(min, Math.min(max, g.alto - dy))}px`;
  };
  const suelta = (e: React.PointerEvent) => {
    const g = gesto.current;
    const el = caja.current;
    gesto.current = undefined;
    if (!g || !el || !g.arrastra) return;
    const dy = e.clientY - g.y;
    const hs = alturas();
    const ahora = g.alto - dy;
    let a = hs.reduce((mejor, h, i) => (Math.abs(h - ahora) < Math.abs(hs[mejor] - ahora) ? i : mejor), 0);
    if (a === altura && Math.abs(dy) > 24) a = Math.max(0, Math.min(2, altura + (dy < 0 ? 1 : -1)));
    el.classList.remove("arrastrando");
    el.style.height = "";
    poner(a as Altura);
  };
  return (
    <div className="menu-hoja" data-altura={altura} ref={caja} onPointerDown={baja} onPointerMove={mueve} onPointerUp={suelta} onPointerCancel={suelta}
      onClickCapture={(e) => { if (arrastro.current) { arrastro.current = false; e.preventDefault(); e.stopPropagation(); } }}>
      <button type="button" className="menu-hoja-agarradera" aria-expanded={altura > 0} title={que} onClick={() => poner(((altura + 1) % 3) as Altura)}>
        <span className="sr-only">{que}</span>
      </button>
      <nav className="menu-hoja-paginas" aria-label={t("Pages")}>{paginas}</nav>
      <div className="menu-hoja-cuenta">{cuenta}</div>
    </div>
  );
}
