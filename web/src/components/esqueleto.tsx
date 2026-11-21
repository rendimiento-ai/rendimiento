// Skeletons: grey shapes with a slow sheen where each thing of a page will
// be, shown while the page's code or its data is on its way, so the page does
// not jump when it arrives. One primitive (Hueso, a bone) and the shapes most
// pages have: a mosaic of cards, a list with the detail beside it, the page
// of one thing, and the Universe's town in silhouette. Their outer sizes
// follow the real pages' (styles.css, "Skeletons"). They live in the main
// chunk: keep them light, and import no page.
//
// A skeleton has no heading and none of the real content's classes: the
// browser tests wait for those, and must not find them on a page that has
// not loaded. Nor does it carry data-listo: it is not the data.
import { useEffect, useState, type CSSProperties, type ReactNode } from "react";
import { t } from "../i18n";

/** How long a skeleton waits before it fades in (.esq in styles.css): a page that arrives sooner never shows it. */
const APARECE_MS = 250;
/** The skeletons on the page, and when the last one that was seen went away. */
const montados = new Set<{ desde: number; sigue: boolean }>();
let vistoHasta = 0;

/**
 * What every skeleton is inside (codigo: the page's code is on its way, the Suspense fallback): busy for a screen
 * reader, which hears "Loading…". One that comes while or right after another that was seen (the page's code
 * arrived, now its data) shows at once: one continuous placeholder. The new one renders before the old one goes,
 * so both are looked for.
 */
export function Esqueleto({ children, className, codigo }: { children: ReactNode; className?: string; codigo?: boolean }) {
  const [yo] = useState(() => {
    const ahora = performance.now();
    const visto = (m: { desde: number; sigue: boolean }) => m.sigue || ahora - m.desde >= APARECE_MS;
    return { desde: ahora, sigue: ahora - vistoHasta < 400 || [...montados].some(visto) };
  });
  useEffect(() => {
    montados.add(yo);
    return () => {
      montados.delete(yo);
      if (yo.sigue || performance.now() - yo.desde >= APARECE_MS) vistoHasta = performance.now();
    };
  }, [yo]);
  const sigue = yo.sigue;
  // .cargando marks the page's code on its way (the browser tests wait for it to go).
  const clases = ["esq", sigue && "esq-sigue", codigo && "cargando", className].filter(Boolean).join(" ");
  return (
    <div className={clases} aria-busy="true">
      <span className="sr-only">{t("Loading…")}</span>
      {children}
    </div>
  );
}

/** A bone: a grey block with the sheen. */
export function Hueso({ w, h, r, className, style }: { w?: number | string; h?: number | string; r?: number | string; className?: string; style?: CSSProperties }) {
  return <div className={className ? `esq-hueso ${className}` : "esq-hueso"} aria-hidden="true" style={{ width: w, height: h, borderRadius: r, ...style }} />;
}

/** A page's title and the line under it, with its buttons on the right when it has them. */
function Cabeza({ acciones, renglones = 1, ancho = "34%" }: { acciones?: boolean; renglones?: number; ancho?: string }) {
  const texto = (
    <div className="esq-cabeza-texto">
      <Hueso className="esq-titulo" w={ancho} />
      {Array.from({ length: renglones }, (_, i) => <Hueso key={i} className="esq-renglon" w={i === renglones - 1 ? "58%" : "92%"} />)}
    </div>
  );
  if (!acciones) return texto;
  return (
    <div className="row between">
      {texto}
      <div className="row"><Hueso w={110} h={20} /><Hueso w={96} h={38} r={10} /></div>
    </div>
  );
}

/** (a) A mosaic of cards with its counters row and search above (Apps, with the Today band above them). Without its head, under the page's real one. */
export function EsqMosaico({ cabeza = true, tejas = 6, codigo, franja }: { cabeza?: boolean; tejas?: number; codigo?: boolean; franja?: boolean }) {
  return (
    <Esqueleto codigo={codigo}>
      {cabeza && (
        <div className="hero-vida">
          <div className="esq-cabeza-texto"><Hueso className="esq-titulo esq-titulo-grande" w={340} /><Hueso className="esq-renglon" w={220} /></div>
        </div>
      )}
      {/* The home's Today band: three cards of the same size. */}
      {franja && <div className="esq-franja">{[0, 1, 2].map((i) => <Hueso key={i} className="esq-tarjeta" />)}</div>}
      <div className="mosaico-cuentas">{[120, 104, 112, 150].map((w, i) => <Hueso key={i} w={w} h={44} r={14} />)}</div>
      <Hueso className="esq-buscar" />
      <div className="mosaico">{Array.from({ length: tejas }, (_, i) => <Hueso key={i} className="esq-teja" />)}</div>
    </Esqueleto>
  );
}

/** (b) A list on the left and the detail of the one chosen beside it (Services, Add-ons, Problems, Mercado, Comments). */
export function EsqListaDetalle({ cabeza = true, acciones, cuentas, codigo }: { cabeza?: boolean; acciones?: boolean; cuentas?: boolean; codigo?: boolean }) {
  return (
    <Esqueleto codigo={codigo}>
      {cabeza && <Cabeza acciones={acciones} />}
      {cuentas && <div className="problema-cuentas">{Array.from({ length: 4 }, (_, i) => <Hueso key={i} className="esq-cuenta" />)}</div>}
      <div className="lista-detalle">
        <div className="esq-ld-lista">
          <Hueso h={44} r={12} />
          <Hueso h={48} r={12} />
          <Hueso className="esq-grupo" />
          {Array.from({ length: 6 }, (_, i) => <Hueso key={i} className="esq-fila" w={`${88 - (i % 3) * 12}%`} />)}
        </div>
        <div className="esq-ld-detalle">
          <div className="row"><Hueso w={44} h={44} r={12} /><Hueso className="esq-titulo" w="46%" /></div>
          <div><Hueso className="esq-renglon" w="94%" /><Hueso className="esq-renglon" w="70%" /></div>
          <Hueso h={120} r={12} />
          <Hueso h={160} r={12} />
        </div>
      </div>
    </Esqueleto>
  );
}

/**
 * (c) The page of one thing: a head with its title and actions, a row of figures, tabs and a body. The flower and
 * the tabs are an app's; "pasos" is a run's body (its steps beside the log), "calle" the Environment's street and
 * signs, "hoja" one large sheet (the Ruta's notebook). "vistazo" is an app's glance (components/vistazo.tsx): its head
 * carries the buttons, so the page's head has none, and three columns below.
 */
export function EsqCosa({ flor, volver, datos = 0, pestanas, cuerpo: pedido = "bloques", acciones, renglones, codigo, vistazo }: {
  codigo?: boolean; flor?: boolean; volver?: boolean; datos?: number; pestanas?: boolean; cuerpo?: "bloques" | "pasos" | "calle" | "hoja"; acciones?: boolean; renglones?: number; vistazo?: boolean;
}) {
  const cuerpo = vistazo ? "vistazo" : pedido;
  return (
    <Esqueleto codigo={codigo}>
      {volver && <Hueso className="esq-volver" />}
      {flor ? (
        <div className="app-cabeza">
          <Hueso w={64} h={64} r="50%" />
          <div className="app-titulo"><Hueso className="esq-titulo" w="52%" /><Hueso className="esq-renglon" w="70%" /></div>
          {!vistazo && <div className="row"><Hueso w={132} h={40} r={999} /><Hueso w={128} h={40} r={10} /></div>}
        </div>
      ) : <Cabeza acciones={acciones} renglones={renglones} />}
      {datos > 0 && <div className="datos">{Array.from({ length: datos }, (_, i) => <Hueso key={i} className="esq-dato" />)}</div>}
      {pestanas && <div className="esq-pestanas">{[64, 72, 92, 60, 84, 70].map((w, i) => <Hueso key={i} w={w} h={18} />)}</div>}
      {cuerpo === "bloques" && <div className="esq-cuerpo"><Hueso h={150} r={14} /><Hueso h={230} r={14} /></div>}
      {cuerpo === "pasos" && (
        <div className="esq-lado">
          <div className="esq-pasos">{Array.from({ length: 6 }, (_, i) => <Hueso key={i} h={42} r={10} />)}</div>
          <div className="esq-registro"><Hueso h={24} w="40%" /><Hueso className="esq-reg" r={14} /></div>
        </div>
      )}
      {cuerpo === "calle" && (
        <>
          <Hueso className="esq-aviso" r={14} />
          <Hueso className="esq-calle" r={20} />
          <div className="letreros">{Array.from({ length: 4 }, (_, i) => <Hueso key={i} w={150} h={110} r={12} />)}</div>
        </>
      )}
      {cuerpo === "vistazo" && (
        <div className="esq-vistazo">
          <div className="esq-vistazo-cabeza">
            <Hueso w={96} h={88} r={20} />
            <div className="esq-cabeza-texto">
              <Hueso className="esq-titulo" w="28%" />
              <Hueso className="esq-renglon" w="66%" />
              <div className="row"><Hueso w={110} h={28} r={999} /><Hueso w={150} h={28} r={999} /><Hueso w={96} h={28} r={999} /></div>
            </div>
            <div className="row"><Hueso w={128} h={40} r={10} /><Hueso w={100} h={40} r={10} /></div>
          </div>
          <div className="esq-vistazo-columnas">
            {[[84, 120], [60, 110], [180, 90]].map(([a, b], i) => (
              <div key={i} className="esq-vistazo-col"><Hueso w="34%" h={12} /><Hueso h={a} r={12} /><Hueso h={b} r={12} /></div>
            ))}
          </div>
        </div>
      )}
      {cuerpo === "hoja" && <Hueso className="esq-hoja" r="6px 16px 16px 6px" />}
    </Esqueleto>
  );
}

/** The houses of the Universe's town in silhouette: where each is (%) and how tall. */
const casas: [number, number, number][] = [[5, 10, 22], [17, 7, 34], [27, 12, 18], [42, 9, 40], [54, 11, 24], [68, 8, 30], [79, 12, 20], [91, 6, 28]];

/** (d) The Universe: the town in silhouette on its ground, the floating controls where the real ones sit, and the side panel. */
export function EsqPueblo({ codigo }: { codigo?: boolean }) {
  return (
    <Esqueleto codigo={codigo} className="stack">
      <div className="u-head"><Hueso className="esq-titulo" w={180} /><Hueso className="esq-renglon" w={260} /></div>
      <div className="universe">
        <div className="esq-pueblo">
          {casas.map(([x, w, h], i) => <Hueso key={i} className="esq-casa" style={{ left: `${x}%`, width: `${w}%`, height: `${h}%` }} />)}
          <Hueso className="esq-suelo" r={0} />
          <Hueso className="esq-u-izq" r={14} />
          <Hueso className="esq-u-der" r={14} />
          <div className="esq-u-zoom">{Array.from({ length: 3 }, (_, i) => <Hueso key={i} w={36} h={36} r={10} />)}</div>
          <Hueso className="esq-u-donde" r={999} />
        </div>
        <div className="esq-u-lado">
          <Hueso className="esq-titulo" w="60%" />
          <Hueso className="esq-renglon" w="90%" />
          <Hueso className="esq-renglon" w="72%" />
          <Hueso h={90} r={12} />
          <Hueso h={140} r={12} />
        </div>
      </div>
    </Esqueleto>
  );
}

/** A few lines of text, for a small panel whose data is on its way (the Ruta's index). */
export function EsqRenglones({ n = 5, className }: { n?: number; className?: string }) {
  return (
    <Esqueleto className={className ? `esq-renglones ${className}` : "esq-renglones"}>
      {Array.from({ length: n }, (_, i) => <Hueso key={i} className="esq-renglon" w={`${92 - (i % 3) * 16}%`} />)}
    </Esqueleto>
  );
}

/**
 * (e) A table's rows (the Bitácora, the Log, the señales received, the AI's calls): a line for its head and its rows,
 * "alto" pixels each, in as many columns as the real one. Under the page's own title and filters.
 */
export function EsqTabla({ filas = 8, columnas = 5, alto = 34 }: { filas?: number; columnas?: number; alto?: number }) {
  const rejilla = { gridTemplateColumns: `repeat(${columnas}, minmax(0, 1fr))` };
  return (
    <Esqueleto className="esq-tabla">
      <div className="esq-tabla-fila" style={rejilla}>{Array.from({ length: columnas }, (_, i) => <Hueso key={i} h={10} w="55%" />)}</div>
      {Array.from({ length: filas }, (_, f) => (
        <div key={f} className="esq-tabla-fila" style={{ ...rejilla, height: alto }}>
          {Array.from({ length: columnas }, (_, i) => <Hueso key={i} h={12} w={`${88 - ((f + i) % 3) * 18}%`} />)}
        </div>
      ))}
    </Esqueleto>
  );
}

/** (f) Cards one under the other, of the heights given: an app's tab (Reliability, Visits), a panel whose data is on its way. */
export function EsqBloques({ alturas = [150, 230], className }: { alturas?: number[]; className?: string }) {
  return (
    <Esqueleto className={className ? `esq-bloques ${className}` : "esq-bloques"}>
      {alturas.map((h, i) => <Hueso key={i} h={h} r={14} />)}
    </Esqueleto>
  );
}

/** (g) The Ruta's front page: its rows for the person, one under the other, and the counts of what goes on meanwhile. Under its real grana head. */
export function EsqPortada({ codigo }: { codigo?: boolean }) {
  return (
    <Esqueleto className="esq-portada" codigo={codigo}>
      {codigo && <Hueso className="esq-pu-cabeza" r="0 0 28px 28px" />}
      <Hueso className="esq-titulo" w={140} />
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="esq-pu-fila"><Hueso w={44} h={44} r={14} /><div className="esq-cabeza-texto"><Hueso className="esq-renglon" w="62%" /><Hueso className="esq-renglon" w="40%" /></div><Hueso w={88} h={44} r={999} /></div>
      ))}
      <div className="esq-pu-cuentas">{[0, 1, 2, 3].map((i) => <Hueso key={i} h={84} r={18} />)}</div>
    </Esqueleto>
  );
}

/** (h) The encargos' cards, while they come: a head line, the title, the difficulty and the actions. */
export function EsqTarjetas({ n = 3 }: { n?: number }) {
  return (
    <Esqueleto className="esq-tarjetas">
      {Array.from({ length: n }, (_, i) => (
        <div key={i} className="esq-et">
          <div className="row"><Hueso w={44} h={44} r={10} /><div className="esq-cabeza-texto"><Hueso w={120} h={20} r={999} /><Hueso className="esq-renglon" w="78%" /></div></div>
          <div className="row"><Hueso w={42} h={42} r={12} /><Hueso className="esq-renglon" w="44%" /></div>
          <div className="row"><Hueso h={46} r={999} style={{ flex: 1 }} /><Hueso w={110} h={46} r={999} /><Hueso w={46} h={46} r={999} /></div>
        </div>
      ))}
    </Esqueleto>
  );
}

/** (i) The coding agent's queue: what it does now, and the line of what follows. */
export function EsqCola() {
  return (
    <Esqueleto className="esq-cola">
      <Hueso className="esq-titulo" w={160} />
      <Hueso h={170} r={20} />
      <Hueso className="esq-titulo" w={110} />
      {[0, 1, 2].map((i) => <div key={i} className="esq-pu-fila"><Hueso w={32} h={32} r={999} /><div className="esq-cabeza-texto"><Hueso className="esq-renglon" w="70%" /><Hueso className="esq-renglon" w="36%" /></div><Hueso w={96} h={44} r={12} /></div>)}
    </Esqueleto>
  );
}

/** (j) The notebook: its search and its colored cards. */
export function EsqCuaderno() {
  return (
    <Esqueleto className="esq-cuaderno">
      <Hueso h={48} r={999} />
      <div className="esq-cu-tarjetas">{Array.from({ length: 6 }, (_, i) => <Hueso key={i} h={96} r={18} />)}</div>
    </Esqueleto>
  );
}
