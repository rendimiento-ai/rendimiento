// What a page shows while its code arrives: the skeleton of that page
// (components/esqueleto), chosen by its address. The page shows the same one
// while its data arrives, so the two read as one placeholder. It fades in
// only after a moment, so a page that arrives quickly never flashes it.
import type { ReactNode } from "react";
import { matchPath, useLocation } from "react-router-dom";
import { EsqCosa, EsqListaDetalle, EsqMosaico, EsqPueblo } from "./esqueleto";

/** Which skeleton each page has; the first pattern that matches wins. Any other page: a page of one thing. */
const porRuta: [string[], () => ReactNode][] = [
  [["/"], () => <EsqMosaico codigo franja />],
  [["/numeros"], () => <EsqCosa codigo cuerpo="bloques" />],
  [["/apps/:name/runs/:id"], () => <EsqCosa codigo volver acciones cuerpo="pasos" />],
  [["/apps/:name", "/apps/:name/:tab"], () => <EsqCosa codigo flor datos={4} pestanas />],
  [["/universe"], () => <EsqPueblo codigo />],
  [["/environment"], () => <EsqCosa codigo acciones cuerpo="calle" />],
  [["/services", "/services/:ns/:name"], () => <EsqListaDetalle codigo acciones />],
  [["/addons", "/addons/:name", "/mercado", "/buzon"], () => <EsqListaDetalle codigo />],
  [["/problems"], () => <EsqListaDetalle codigo cuentas />],
  [["/ruta"], () => <EsqCosa codigo renglones={2} cuerpo="hoja" />],
];

export function Cargando() {
  const { pathname } = useLocation();
  const hallada = porRuta.find(([patrones]) => patrones.some((p) => matchPath(p, pathname)));
  return <>{hallada ? hallada[1]() : <EsqCosa codigo />}</>;
}
