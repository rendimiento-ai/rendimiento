import { useEffect, useState } from "react";

// The flag on the presidencia municipal (the control plane's house in Environment, and the Universe's), chosen by each
// person on this device. Each flag is a few shapes: stripes, a disc, a canton, a hoist triangle, a diamond, a cross or a
// saltire, drawn small, so the shapes say the country without its finer emblems. Its name comes from the browser
// (Intl.DisplayNames), in the page's language.

type Forma =
  | { franjas: "h" | "v"; c: string[]; p?: number[] } // stripes, with their weights
  | { disco: string; x?: number; y?: number; r?: number } // a disc, in fractions of the width and height
  | { canton: string; w: number; h: number } // the upper hoist corner
  | { triangulo: string; w: number } // a triangle from the hoist
  | { rombo: string } // a diamond
  | { cruz: string; x?: number; t: number } // a cross: its upright at x (0.5 centred), t thick (fraction of the height)
  | { aspa: string; t: number }; // a saltire, corner to corner

const tres = (a: string, b: string, c: string, dir: "h" | "v" = "v", p?: number[]): Forma => ({ franjas: dir, c: [a, b, c], p });
const rayas = (n: number, a: string, b: string): string[] => Array.from({ length: n }, (_, i) => (i % 2 ? b : a));

export const BANDERAS: Record<string, Forma[]> = {
  MX: [tres("#006847", "#FFFFFF", "#CE1126"), { disco: "#8C5A2B", r: 0.08 }],
  US: [{ franjas: "h", c: rayas(13, "#B22234", "#FFFFFF") }, { canton: "#3C3B6E", w: 0.4, h: 7 / 13 }],
  CA: [tres("#D80621", "#FFFFFF", "#D80621", "v", [1, 2, 1]), { disco: "#D80621", r: 0.11 }],
  GT: [tres("#4997D0", "#FFFFFF", "#4997D0")],
  SV: [tres("#0F47AF", "#FFFFFF", "#0F47AF", "h")],
  HN: [tres("#0073CF", "#FFFFFF", "#0073CF", "h")],
  NI: [tres("#0067C6", "#FFFFFF", "#0067C6", "h")],
  CR: [{ franjas: "h", c: ["#002B7F", "#FFFFFF", "#CE1126", "#FFFFFF", "#002B7F"], p: [1, 1, 2, 1, 1] }],
  CU: [{ franjas: "h", c: rayas(5, "#002A8F", "#FFFFFF") }, { triangulo: "#CF142B", w: 0.45 }],
  PR: [{ franjas: "h", c: rayas(5, "#ED0000", "#FFFFFF") }, { triangulo: "#0050F0", w: 0.45 }],
  CO: [tres("#FCD116", "#003893", "#CE1126", "h", [2, 1, 1])],
  VE: [tres("#FFCC00", "#00247D", "#CF142B", "h")],
  EC: [tres("#FFDD00", "#034EA2", "#ED1C24", "h", [2, 1, 1])],
  PE: [tres("#D91023", "#FFFFFF", "#D91023")],
  BO: [tres("#D52B1E", "#F9E300", "#007934", "h")],
  CL: [{ franjas: "h", c: ["#FFFFFF", "#D52B1E"] }, { canton: "#0039A6", w: 1 / 3, h: 0.5 }],
  AR: [tres("#74ACDF", "#FFFFFF", "#74ACDF", "h"), { disco: "#F6B40E", r: 0.07 }],
  UY: [{ franjas: "h", c: rayas(9, "#FFFFFF", "#0038A8") }, { canton: "#FFFFFF", w: 0.38, h: 5 / 9 }, { disco: "#FCD116", x: 0.19, y: 0.27, r: 0.09 }],
  PY: [tres("#D52B1E", "#FFFFFF", "#0038A8", "h")],
  BR: [{ franjas: "h", c: ["#009C3B"] }, { rombo: "#FFDF00" }, { disco: "#002776", r: 0.16 }],
  ES: [tres("#AA151B", "#F1BF00", "#AA151B", "h", [1, 2, 1])],
  PT: [{ franjas: "v", c: ["#006600", "#FF0000"], p: [2, 3] }, { disco: "#FFE900", x: 0.4, r: 0.13 }],
  FR: [tres("#0055A4", "#FFFFFF", "#EF4135")],
  IT: [tres("#009246", "#FFFFFF", "#CE2B37")],
  DE: [tres("#000000", "#DD0000", "#FFCE00", "h")],
  NL: [tres("#AE1C28", "#FFFFFF", "#21468B", "h")],
  BE: [tres("#000000", "#FDDA24", "#EF3340")],
  IE: [tres("#169B62", "#FFFFFF", "#FF883E")],
  AT: [tres("#ED2939", "#FFFFFF", "#ED2939", "h")],
  CH: [{ franjas: "h", c: ["#DA291C"] }, { cruz: "#FFFFFF", t: 0.2 }],
  GB: [{ franjas: "h", c: ["#012169"] }, { aspa: "#FFFFFF", t: 0.2 }, { aspa: "#C8102E", t: 0.07 }, { cruz: "#FFFFFF", t: 0.33 }, { cruz: "#C8102E", t: 0.2 }],
  SE: [{ franjas: "h", c: ["#006AA7"] }, { cruz: "#FECC00", x: 0.375, t: 0.2 }],
  NO: [{ franjas: "h", c: ["#BA0C2F"] }, { cruz: "#FFFFFF", x: 0.36, t: 0.25 }, { cruz: "#00205B", x: 0.36, t: 0.125 }],
  DK: [{ franjas: "h", c: ["#C8102E"] }, { cruz: "#FFFFFF", x: 0.36, t: 0.14 }],
  FI: [{ franjas: "h", c: ["#FFFFFF"] }, { cruz: "#002F6C", x: 0.36, t: 0.27 }],
  PL: [{ franjas: "h", c: ["#FFFFFF", "#DC143C"] }],
  UA: [{ franjas: "h", c: ["#0057B7", "#FFD700"] }],
  RU: [tres("#FFFFFF", "#0039A6", "#D52B1E", "h")],
  CZ: [{ franjas: "h", c: ["#FFFFFF", "#D7141A"] }, { triangulo: "#11457E", w: 0.5 }],
  JP: [{ franjas: "h", c: ["#FFFFFF"] }, { disco: "#BC002D", r: 0.3 }],
  CN: [{ franjas: "h", c: ["#EE1C25"] }, { disco: "#FFFF00", x: 0.17, y: 0.25, r: 0.1 }],
  IN: [tres("#FF9933", "#FFFFFF", "#138808", "h"), { disco: "#000080", r: 0.09 }],
  PH: [{ franjas: "h", c: ["#0038A8", "#CE1126"] }, { triangulo: "#FFFFFF", w: 0.45 }, { disco: "#FCD116", x: 0.14, r: 0.08 }],
  NG: [tres("#008751", "#FFFFFF", "#008751")],
  EG: [tres("#CE1126", "#FFFFFF", "#000000", "h")],
};

/** A shape to paint, in pixels of a flag w wide and h high. */
export type Trazo =
  | { k: "rect"; x: number; y: number; w: number; h: number; c: string }
  | { k: "poly"; pts: [number, number][]; c: string }
  | { k: "disco"; cx: number; cy: number; r: number; c: string };

/** The flag of code (MX when unknown) as shapes to paint, w by h pixels. */
export function trazos(code: string, w: number, h: number): Trazo[] {
  const out: Trazo[] = [];
  const franjas = (dir: "h" | "v", c: string[], p: number[] | undefined, x0: number, y0: number, ww: number, hh: number) => {
    const pesos = p ?? c.map(() => 1), total = pesos.reduce((a, b) => a + b, 0);
    let at = 0;
    c.forEach((col, i) => {
      const f = pesos[i] / total;
      out.push(dir === "h" ? { k: "rect", x: x0, y: y0 + at * hh, w: ww, h: f * hh, c: col } : { k: "rect", x: x0 + at * ww, y: y0, w: f * ww, h: hh, c: col });
      at += f;
    });
  };
  for (const f of BANDERAS[code] ?? BANDERAS.MX) {
    if ("franjas" in f) franjas(f.franjas, f.c, f.p, 0, 0, w, h);
    else if ("disco" in f) out.push({ k: "disco", cx: (f.x ?? 0.5) * w, cy: (f.y ?? 0.5) * h, r: (f.r ?? 0.2) * h, c: f.disco });
    else if ("canton" in f) out.push({ k: "rect", x: 0, y: 0, w: f.w * w, h: f.h * h, c: f.canton });
    else if ("triangulo" in f) out.push({ k: "poly", pts: [[0, 0], [f.w * w, h / 2], [0, h]], c: f.triangulo });
    else if ("rombo" in f) out.push({ k: "poly", pts: [[w / 2, h * 0.1], [w * 0.92, h / 2], [w / 2, h * 0.9], [w * 0.08, h / 2]], c: f.rombo });
    else if ("cruz" in f) {
      const t = f.t * h, cx = (f.x ?? 0.5) * w;
      out.push({ k: "rect", x: 0, y: h / 2 - t / 2, w, h: t, c: f.cruz }, { k: "rect", x: cx - t / 2, y: 0, w: t, h, c: f.cruz });
    } else {
      const d = f.t * h;
      out.push({ k: "poly", pts: [[0, 0], [d, 0], [w, h - d], [w, h], [w - d, h], [0, d]], c: f.aspa },
        { k: "poly", pts: [[w, 0], [w, d], [d, h], [0, h], [0, h - d], [w - d, 0]], c: f.aspa });
    }
  }
  return out;
}

/** Paints code's flag on a canvas, its top left corner at x, y. */
export function pintarBandera(ctx: CanvasRenderingContext2D, code: string, x: number, y: number, w: number, h: number) {
  for (const s of trazos(code, w, h)) {
    ctx.fillStyle = s.c;
    if (s.k === "rect") ctx.fillRect(x + s.x, y + s.y, s.w, s.h);
    else {
      ctx.beginPath();
      if (s.k === "disco") ctx.arc(x + s.cx, y + s.cy, s.r, 0, Math.PI * 2);
      else s.pts.forEach(([px, py], i) => (i ? ctx.lineTo(x + px, y + py) : ctx.moveTo(x + px, y + py)));
      ctx.fill();
    }
  }
}

/** code's flag as SVG shapes, at x, y, w by h. */
export function BanderaSVG({ code, x = 0, y = 0, w, h }: { code: string; x?: number; y?: number; w: number; h: number }) {
  return (
    <g transform={`translate(${x} ${y})`}>
      {trazos(code, w, h).map((s, i) => s.k === "rect" ? <rect key={i} x={s.x} y={s.y} width={s.w} height={s.h} fill={s.c} />
        : s.k === "disco" ? <circle key={i} cx={s.cx} cy={s.cy} r={s.r} fill={s.c} />
        : <polygon key={i} points={s.pts.map((p) => p.join(",")).join(" ")} fill={s.c} />)}
      <rect width={w} height={h} fill="none" stroke="rgba(0,0,0,.25)" strokeWidth=".6" />
    </g>
  );
}

const CLAVE = "rendimiento.bandera";
const CAMBIO = "rendimiento:bandera";

/** The country of the browser's language (es-MX → MX), when there is a flag for it. */
function delNavegador(): string | undefined {
  for (const l of navigator.languages ?? [navigator.language]) {
    const region = l.split("-")[1]?.toUpperCase();
    if (region && BANDERAS[region]) return region;
  }
  return undefined;
}

/** The flag this person chose on this device: their choice, else their browser's country, else Mexico's. */
export function banderaActual(): string {
  try {
    const v = localStorage.getItem(CLAVE);
    if (v && BANDERAS[v]) return v;
  } catch { /* storage blocked: the browser's country */ }
  return delNavegador() ?? "MX";
}

export function ponerBandera(code: string) {
  try { localStorage.setItem(CLAVE, code); } catch { /* this visit only */ }
  window.dispatchEvent(new Event(CAMBIO));
}

/** The flag chosen now, re-rendering when it changes. */
export function useBandera(): string {
  const [b, setB] = useState(banderaActual);
  useEffect(() => {
    const on = () => setB(banderaActual());
    window.addEventListener(CAMBIO, on);
    return () => window.removeEventListener(CAMBIO, on);
  }, []);
  return b;
}

/** A country's name in the page's language. */
export function nombrePais(code: string, locale: string): string {
  try { return new Intl.DisplayNames([locale], { type: "region" }).of(code) ?? code; } catch { return code; }
}
