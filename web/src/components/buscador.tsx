// The search in the top bar: apps by their name, runs by their number or
// branch, and the pages by their name, as you type. "/" opens it from any
// page (not while writing in a box); Enter opens the first result, the
// arrows move between them. On a phone it is a button that opens the box
// over the top of the screen.
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { api, type App, type RunNow } from "../api";
import { t } from "../i18n";
import { statusText } from "./ui";

/** A page the search can open: its address and its name, in the reader's language. */
export interface PaginaBuscable { to: string; nombre: string }

interface Resultado { id: string; grupo: "apps" | "runs" | "paginas"; a: string; titulo: ReactNode; detalle?: string }

/** Lowercase and without accents: "bitacora" finds Bitácora. */
const llano = (s: string) => s.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();

/** Whether a key press is someone writing (in a box, a list or an editable text). */
export function escribiendo(e: KeyboardEvent): boolean {
  const el = e.target;
  return el instanceof HTMLElement && (/^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName) || el.isContentEditable);
}

export function Buscador({ paginas, telefono }: { paginas: PaginaBuscable[]; telefono?: boolean }) {
  const nav = useNavigate();
  const { pathname } = useLocation();
  const [texto, setTexto] = useState("");
  const [q, setQ] = useState("");
  const [abierto, setAbierto] = useState(false);
  const [activo, setActivo] = useState(0);
  const [apps, setApps] = useState<App[]>();
  const [runs, setRuns] = useState<RunNow[]>([]);
  const [porNumero, setPorNumero] = useState<RunNow>();
  const caja = useRef<HTMLDivElement>(null);
  const entrada = useRef<HTMLInputElement>(null);

  // What was typed, a moment after the last key.
  useEffect(() => {
    const id = setTimeout(() => setQ(texto.trim()), 120);
    return () => clearTimeout(id);
  }, [texto]);
  useEffect(() => setActivo(0), [q]);

  // The apps and the runs, read when the search opens: nothing while it is closed.
  useEffect(() => {
    if (!abierto) return;
    api.apps().then(setApps, () => {});
    api.corriendo().then((c) => setRuns([...c.ahora, ...c.recientes]), () => {});
  }, [abierto]);

  // A run's number that is not among the last ones: asked for, once the typing stops.
  const numero = /^#?(\d+)$/.exec(q)?.[1];
  useEffect(() => {
    setPorNumero(undefined);
    if (!numero || !apps || runs.some((r) => String(r.id) === numero)) return;
    let vivo = true;
    api.run(Number(numero)).then((r) => {
      const app = apps.find((a) => a.id === r.appId);
      if (vivo && app) setPorNumero({ ...r, app: app.name });
    }, () => {});
    return () => { vivo = false; };
  }, [numero, apps, runs]);

  // "/" opens it from anywhere, unless one is writing.
  useEffect(() => {
    const tecla = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey || escribiendo(e)) return;
      e.preventDefault();
      setAbierto(true);
      entrada.current?.focus();
    };
    window.addEventListener("keydown", tecla);
    return () => window.removeEventListener("keydown", tecla);
  }, []);
  // A phone's box appears when opened: the focus goes into it then.
  useEffect(() => { if (abierto && telefono) entrada.current?.focus(); }, [abierto, telefono]);

  // A click outside or going to another page closes it.
  useEffect(() => {
    if (!abierto) return;
    const fuera = (e: MouseEvent) => { if (caja.current && !caja.current.contains(e.target as Node)) setAbierto(false); };
    document.addEventListener("mousedown", fuera);
    return () => document.removeEventListener("mousedown", fuera);
  }, [abierto]);
  useEffect(() => { setAbierto(false); setTexto(""); }, [pathname]);

  const resultados = useMemo<Resultado[]>(() => {
    const s = llano(q);
    if (!s) return [];
    const primero = (x: string) => (llano(x).startsWith(s) ? 0 : 1);
    const deApps = (apps ?? []).filter((a) => llano(a.name).includes(s) || llano(a.repo).includes(s))
      .sort((x, y) => primero(x.name) - primero(y.name) || x.name.localeCompare(y.name)).slice(0, 6)
      .map((a): Resultado => ({ id: `app-${a.id}`, grupo: "apps", a: `/apps/${a.name}`, titulo: a.name, detalle: a.repo }));
    const n = /^#?(\d+)$/.exec(s)?.[1];
    const todas = porNumero ? [porNumero, ...runs] : runs;
    const deRuns = todas.filter((r) => (n ? String(r.id).startsWith(n) : llano(r.branch).includes(s)))
      .sort((x, y) => (n ? Number(String(y.id) === n) - Number(String(x.id) === n) : 0) || y.id - x.id).slice(0, 5)
      .map((r): Resultado => ({ id: `run-${r.id}`, grupo: "runs", a: `/apps/${r.app}/runs/${r.id}`,
        titulo: t("Run {n} · {app}", { n: r.id, app: r.app }), detalle: `${r.branch} · ${statusText(r.status)}` }));
    const dePaginas = paginas.filter((p) => llano(p.nombre).includes(s))
      .sort((x, y) => primero(x.nombre) - primero(y.nombre)).slice(0, 5)
      .map((p): Resultado => ({ id: `pagina-${p.to}`, grupo: "paginas", a: p.to, titulo: p.nombre }));
    return [...deApps, ...deRuns, ...dePaginas];
  }, [q, apps, runs, porNumero, paginas]);

  const ir = (r: Resultado) => {
    setAbierto(false);
    setTexto("");
    entrada.current?.blur();
    nav(r.a);
  };
  const tecla = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      if (!resultados.length) return;
      e.preventDefault();
      setActivo((i) => (i + (e.key === "ArrowDown" ? 1 : resultados.length - 1)) % resultados.length);
    } else if (e.key === "Enter") {
      const r = resultados[activo] ?? resultados[0];
      if (r) { e.preventDefault(); ir(r); }
    } else if (e.key === "Escape") {
      e.preventDefault();
      if (texto) setTexto("");
      else { setAbierto(false); entrada.current?.blur(); }
    }
  };

  const grupos: { id: Resultado["grupo"]; nombre: string }[] = [
    { id: "apps", nombre: t("Apps") }, { id: "runs", nombre: t("Runs") }, { id: "paginas", nombre: t("Pages") },
  ];
  const lista = abierto && texto.trim() !== "";
  const etiqueta = t("Search apps, runs or pages");

  if (telefono && !abierto) {
    return (
      <button type="button" className="icon-btn buscador-abrir" title={etiqueta} aria-label={etiqueta} onClick={() => setAbierto(true)}>
        <Lupa />
      </button>
    );
  }
  return (
    <div className={`buscador${telefono ? " flotante" : ""}`} ref={caja} role="search">
      <label className="buscador-caja">
        <Lupa />
        <input ref={entrada} type="search" value={texto} placeholder={etiqueta} aria-label={etiqueta}
          role="combobox" aria-autocomplete="list" aria-expanded={lista} aria-controls="buscador-lista"
          aria-activedescendant={lista && resultados[activo] ? `buscador-${resultados[activo].id}` : undefined}
          onChange={(e) => { setTexto(e.target.value); setAbierto(true); }} onFocus={() => setAbierto(true)} onKeyDown={tecla} />
        {!telefono && <kbd aria-hidden="true">/</kbd>}
      </label>
      {telefono && <button type="button" className="buscador-cerrar" onClick={() => setAbierto(false)}>{t("Close")}</button>}
      {lista && (
        <div className="buscador-panel">
          {resultados.length === 0
            ? <p className="buscador-nada">{q === texto.trim() ? t("Nothing matches “{q}”.", { q: texto.trim() }) : t("Searching…")}</p>
            : (
              <ul id="buscador-lista" role="listbox" aria-label={etiqueta}>
                {grupos.filter((g) => resultados.some((r) => r.grupo === g.id)).map((g) => (
                  <li key={g.id} role="presentation">
                    <p className="buscador-grupo" aria-hidden="true">{g.nombre}</p>
                    <ul role="group" aria-label={g.nombre}>
                      {resultados.filter((r) => r.grupo === g.id).map((r) => {
                        const i = resultados.indexOf(r);
                        return (
                          <li key={r.id} id={`buscador-${r.id}`} role="option" aria-selected={i === activo} className="buscador-opcion"
                            onMouseDown={(e) => e.preventDefault()} onMouseEnter={() => setActivo(i)} onClick={() => ir(r)}>
                            <span>{r.titulo}</span>
                            {r.detalle && <small>{r.detalle}</small>}
                          </li>
                        );
                      })}
                    </ul>
                  </li>
                ))}
              </ul>
            )}
        </div>
      )}
    </div>
  );
}

function Lupa() {
  return <svg className="lupa" width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5" /><path d="M15.5 15.5 21 21" /></svg>;
}
