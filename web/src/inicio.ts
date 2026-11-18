// The home page as each person lays it out: which sections it shows and in
// what order. Kept on the server by login (internal/api/inicio.go), so the
// phone shows the same; this browser keeps the last one it saw, so the page
// does not jump while the server answers.
import { useEffect, useState } from "react";
import { api, type InicioView, type SeccionInicio } from "./api";

/** The home page's sections, in their built-in order and with whether they show at first (the same in internal/api/inicio.go). */
export const SECCIONES = [
  { id: "corriendo", on: true },
  { id: "esperan", on: true },
  { id: "ayuda", on: true },
  { id: "apps", on: true },
  { id: "marcador", on: false },
  { id: "visitas", on: false },
  { id: "entregas", on: false },
  { id: "lluvia", on: true },
] as const;

export type IdSeccion = (typeof SECCIONES)[number]["id"];

/** Each section's name in the Customize panel. */
export const NOMBRES: Record<IdSeccion, string> = {
  corriendo: "Running",
  esperan: "Waiting for you",
  ayuda: "Need help",
  apps: "Your apps (the mosaic)",
  marcador: "Yesterday's scoreboard, unfolded",
  visitas: "Visits",
  entregas: "Delivery",
  lluvia: "Petal rain on a new release",
};

/** The three cards of the Today band. */
export const TARJETAS: ReadonlySet<string> = new Set<IdSeccion>(["corriendo", "esperan", "ayuda"]);

const GUARDADO = "rendimiento.inicio";
const conocidas = new Set<string>(SECCIONES.map((s) => s.id));

/** The known sections of a list, once each and in its order, with the ones it lacks added as base has them. */
export function completar(lista: readonly SeccionInicio[], base: readonly SeccionInicio[] = SECCIONES): SeccionInicio[] {
  const vistas = new Set<string>();
  const out: SeccionInicio[] = [];
  for (const s of [...lista, ...base]) {
    if (!conocidas.has(s.id) || vistas.has(s.id)) continue;
    vistas.add(s.id);
    out.push({ id: s.id, on: s.on });
  }
  return out;
}

function leer(): SeccionInicio[] | undefined {
  try {
    const raw = localStorage.getItem(GUARDADO);
    const v = raw ? JSON.parse(raw) : undefined;
    return Array.isArray(v) ? v.filter((s) => typeof s?.id === "string" && typeof s?.on === "boolean") : undefined;
  } catch {
    return undefined;
  }
}

function recordar(secciones: SeccionInicio[]) {
  try { localStorage.setItem(GUARDADO, JSON.stringify(secciones)); } catch { /* the server has it */ }
}

/** The person's home page: what this browser kept at once, then the server's. `aplicar` takes an answer of the server. */
export function useInicio() {
  const [vista, setVista] = useState<InicioView>();
  const [secciones, setSecciones] = useState<SeccionInicio[]>(() => completar(leer() ?? []));
  const aplicar = (v: InicioView) => {
    const s = completar(v.secciones);
    setVista(v);
    setSecciones(s);
    recordar(s);
  };
  useEffect(() => {
    api.inicio().then(aplicar, () => undefined); // without an answer, what this browser kept (or the built-in)
  }, []);
  return { secciones, vista, aplicar };
}
