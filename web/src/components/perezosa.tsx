// A page whose code loads when it is opened (React.lazy), and what happens
// when that code does not arrive: offline, or rendimiento was updated and
// the old file is gone. After an update the page reloads once by itself, to
// the new version; otherwise it says so and offers to reload.
import { lazy, type ComponentType } from "react";
import { t } from "../i18n";

const RECARGADA = "rendimiento.recargada";
// A second failure this soon after reloading by itself is not an update: say so instead of looping.
const ESPERA_MS = 60_000;

/** The import failed because the file did not arrive (not because its code threw). */
function faltaArchivo(e: unknown): boolean {
  if (!(e instanceof Error)) return false;
  return e.name === "TypeError" || e.name === "ChunkLoadError" || /dynamically imported module|module script|Importing a module/i.test(e.message);
}

/** Reloads the page, unless it did so a moment ago (sessionStorage may be unavailable: then never by itself). */
function recargarUnaVez(): boolean {
  try {
    const antes = Number(sessionStorage.getItem(RECARGADA) ?? 0);
    if (Date.now() - antes < ESPERA_MS) return false;
    sessionStorage.setItem(RECARGADA, String(Date.now()));
  } catch {
    return false;
  }
  location.reload();
  return true;
}

function NoCargo() {
  return (
    <div className="cargando-fallo" role="alert">
      <p>{t("This page could not be loaded. You may be offline, or rendimiento was just updated.")}</p>
      <button type="button" className="btn primary" onClick={() => location.reload()}>{t("Reload")}</button>
    </div>
  );
}

/** `perezosa(() => import("./pages/X").then((m) => m.X))`: the page X, loaded when it is first shown. */
export function perezosa<T extends ComponentType<any>>(cargar: () => Promise<T>) {
  return lazy(() =>
    cargar().then(
      (pagina) => ({ default: pagina }),
      (e: unknown) => {
        console.warn("a page's code did not load", e);
        // While the reload happens, the placeholder stays: no message flashes.
        if (navigator.onLine !== false && faltaArchivo(e) && recargarUnaVez()) return new Promise<never>(() => {});
        return { default: NoCargo as unknown as T };
      },
    ),
  );
}
