// What a page shows while its code arrives: a few quiet blocks where the
// title and the first cards will be, the same height every time. It fades in
// only after a moment, so a page that arrives quickly never flashes it.
export function Cargando() {
  return (
    <div className="cargando" aria-busy="true">
      <div className="cargando-titulo" />
      <div className="cargando-linea" />
      <div className="cargando-bloque" />
      <div className="cargando-bloque" />
    </div>
  );
}
