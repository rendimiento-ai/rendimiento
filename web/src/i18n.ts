// Translation: every phrase in the UI is written in English and passed
// through t(); in Spanish, t() looks it up in i18n/es.ts (falling back to
// the English text). The language follows the browser until the user picks
// one with the switch in the top bar; the choice is kept in this browser.
// A browser with neither Spanish nor English starts in its country's (see initial()).
// The API is told the language too (Accept-Language), for server messages.
// Spanish is Mexican Spanish (es-MX): usted, presione, computadora, falla.
import { es } from "./i18n/es";

export type Lang = "en" | "es";

const storageKey = "rendimiento.lang";

/**
 * The language to start in: the one chosen before in this browser; else the first of the browser's
 * languages that is Spanish or English; else, when it names neither, the one of the visitor's country
 * (the server puts it in the page from Cloudflare's CF-IPCountry: only "es" or "en"); else English.
 */
function initial(): Lang {
  try {
    const saved = localStorage.getItem(storageKey);
    if (saved === "en" || saved === "es") return saved;
  } catch {
    /* storage blocked: follow the browser */
  }
  const langs = navigator.languages?.length ? navigator.languages : [navigator.language ?? ""];
  for (const l of langs) {
    const primary = l.toLowerCase().split("-")[0];
    if (primary === "es" || primary === "en") return primary;
  }
  const pais = document.querySelector('meta[name="rendimiento-lang-pais"]')?.getAttribute("content");
  return pais === "es" ? "es" : "en";
}

export const lang: Lang = initial();

/** The locale for dates and numbers. */
export const locale = lang === "es" ? "es-MX" : "en-US";

document.documentElement.lang = lang;

/** Switches the language and reloads, so every page renders in it. */
export function setLang(l: Lang) {
  try {
    localStorage.setItem(storageKey, l);
  } catch {
    /* not remembered; still switch for this load */
  }
  location.reload();
}

/** The phrase in the current language, with {name} placeholders filled in. */
export function t(s: string, vars?: Record<string, string | number>): string {
  let out = lang === "es" ? (es[s] ?? s) : s;
  if (vars) out = out.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m));
  return out;
}

/** t() for a count: `one` when n is 1, else `other`; {n} is filled in. */
export function tn(n: number, one: string, other: string, vars?: Record<string, string | number>): string {
  return t(n === 1 ? one : other, { n, ...vars });
}
