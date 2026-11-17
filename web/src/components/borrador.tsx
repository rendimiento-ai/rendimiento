// Drafts: what the person was writing (to the team, an answer, a note) is
// kept in this browser as it is typed, per box and per login, and comes back
// in its box when the page opens again; it is cleared once it is sent. It is
// never sent by itself: without a connection, the Send button is switched
// off and the text waits here.
import { useState } from "react";
import { quien } from "../trabajador/registro";
import { t } from "../i18n";

function leer(llave: string): string | null {
  try {
    return localStorage.getItem(llave);
  } catch {
    return null;
  }
}

const llaveDe = (caja: string) => `rendimiento.borrador.${quien() || "-"}.${caja}`;

/** The draft kept for a box, if any. */
export function borradorDe(caja: string): string | undefined {
  return leer(llaveDe(caja)) || undefined;
}

/** Keeps a box's text as its draft (an empty one is dropped); true if it was kept. */
export function guardarBorrador(caja: string, texto: string): boolean {
  try {
    if (!texto.trim()) {
      localStorage.removeItem(llaveDe(caja));
      return false;
    }
    localStorage.setItem(llaveDe(caja), texto);
    return true;
  } catch {
    return false; // storage blocked: the text stays in the box only
  }
}

/** Drops a box's draft (it was sent). */
export function soltarBorrador(caja: string) {
  guardarBorrador(caja, "");
}

/**
 * The text of a box kept as a draft: `caja` names the box ("al-equipo", "respuesta-12"). `inicial` is used when there
 * is no draft (or wins over it when `primero`).
 */
export function useBorrador(caja: string, inicial = "", primero = false) {
  const llave = llaveDe(caja);
  const [estado, setEstado] = useState(() => {
    const kept = leer(llave);
    if (kept && !(primero && inicial)) return { llave, texto: kept, guardado: true };
    return { llave, texto: inicial, guardado: false };
  });
  // Another box (another comment opened): its own draft.
  if (estado.llave !== llave) {
    const kept = leer(llave);
    setEstado({ llave, texto: kept ?? "", guardado: !!kept });
  }
  const poner = (texto: string) => setEstado({ llave, texto, guardado: guardarBorrador(caja, texto) });
  const limpiar = () => {
    soltarBorrador(caja);
    setEstado({ llave, texto: "", guardado: false });
  };
  return { texto: estado.texto, poner, limpiar, guardado: estado.guardado };
}

/** "Draft kept on this device", under a box whose text is kept. */
export function BorradorGuardado({ si }: { si: boolean }) {
  if (!si) return null;
  return <span className="conexion-borrador" role="status">{t("Draft kept on this device")}</span>;
}
