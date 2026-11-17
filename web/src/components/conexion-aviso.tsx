// What the page says under the top bar about its connection: without one,
// how old what it shows is (ticking), gone once a live answer arrives; and
// when a new version of rendimiento is waiting, a button to reload into it.
// Also the switched-off actions: `apagado()` for a button that changes
// something, which says why while there is no connection.
import { useEffect, useState } from "react";
import { timeAgo } from "../api";
import { useConexion } from "../conexion";
import { recargarNueva, useNuevaVersion } from "../trabajador/registro";
import { t } from "../i18n";

/** Redraws every few seconds while on, so "3m ago" keeps up. */
function useTic(on: boolean) {
  const [, poner] = useState(0);
  useEffect(() => {
    if (!on) return;
    const i = setInterval(() => poner((n) => n + 1), 5000);
    return () => clearInterval(i);
  }, [on]);
}

export function AvisoConexion() {
  const { sin, guardado } = useConexion();
  const nueva = useNuevaVersion();
  useTic(sin && guardado !== undefined);
  return (
    <>
      {nueva && (
        <div className="conexion-aviso conexion-nueva" role="status">
          <span><b>{t("There is a new version of rendimiento.")}</b> {t("It loads when you reload the page.")}</span>
          <button type="button" className="btn primary" onClick={recargarNueva}>{t("Reload")}</button>
        </div>
      )}
      {sin && (
        <div className={`conexion-aviso${guardado !== undefined ? " conexion-guardado" : ""}`} role="status" aria-live="polite">
          <span>
            <b>{t("Without a connection.")}</b>{" "}
            {guardado !== undefined && t("You see what there was {when}.", { when: timeAgo(new Date(guardado).toISOString()) })}{" "}
            {t("It catches up by itself when it is back.")}
          </span>
          <span className="conexion-porque small">{porQueApagado()}</span>
        </div>
      )}
    </>
  );
}

/** The why of a switched-off action. */
export const porQueApagado = () => t("Without a connection: actions that change something wait until it is back, and are never sent later by themselves.");

/**
 * For a button that changes something: `<button {...apagado(conexion, busy)}>`. Without a connection it is switched
 * off and its title says why; otherwise it is as `disabled` says.
 */
export function apagado(c: { sin: boolean }, disabled?: boolean, title?: string): { disabled: boolean; title?: string; "aria-disabled"?: boolean } {
  if (c.sin) return { disabled: true, title: porQueApagado(), "aria-disabled": true };
  return { disabled: !!disabled, title };
}

/** The line beside switched-off actions, shown only without a connection. */
export function PorQueApagado() {
  const { sin } = useConexion();
  if (!sin) return null;
  return <span className="conexion-porque small muted">{porQueApagado()}</span>;
}
