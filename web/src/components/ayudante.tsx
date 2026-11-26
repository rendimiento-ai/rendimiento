// El ayudante (decision 188): Tsïntsuni, the hummingbird — tsïntsuni in Purépecha, as in Tzintzuntzan,
// «the place of hummingbirds» — whom a person asks anything about rendimiento: where a setting is, what a
// page does, why a run failed. It answers with links to where to go, with the local model (free) unless
// the person asks the platform's AI. It only reads and points. A round button on every page opens it: a
// panel beside the page on a computer, a sheet from below on a phone. Each person's conversation is
// theirs alone and is kept 7 days.
import { useEffect, useRef, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { api, dollars, type AIProvider, type AyudanteEnlace, type AyudanteMensaje, type AyudanteView } from "../api";
import { t } from "../i18n";
import { ErrorBox } from "./ui";

/** The hummingbird, in the town's colors: its wings beat, it hums in place and blinks. */
export function Colibri({ size = 44 }: { size?: number }) {
  return (
    <svg className="colibri" width={size} height={size} viewBox="0 0 44 44" aria-hidden="true" fill="none" strokeLinecap="round" strokeLinejoin="round">
      <path className="colibri-ala" d="M21 16 7 9c3 7 7 10 12 11z" fill="#5BC0BE" stroke="#F2C14E" strokeWidth="1.6" />
      <path d="M16 22c0-5 4-8 9-8 4 0 7 3 7 6 0 6-6 10-13 11-3 .4-5 2-7 5 0-5 2-9 4-11z" fill="#3E9E6B" stroke="#F2C14E" strokeWidth="1.6" />
      <path d="M19 24c3 2 7 2 10-1" stroke="#D6246E" strokeWidth="3" />
      <path d="M31.5 18.5 41 16.5" stroke="#2B1B12" strokeWidth="1.8" />
      <g className="colibri-ojo"><circle cx="28" cy="18" r="2" fill="#FFFDF8" /><circle cx="28.4" cy="18" r="1" fill="#2B1B12" /></g>
      <path d="M12 36l-3 5M14 35.5l-1 5.5M10 34l-5 3" stroke="#8E1B2C" strokeWidth="1.8" />
    </svg>
  );
}

/** A cempasúchil flower: three of them turn while it thinks. */
function Flor({ color, d }: { color: string; d: string }) {
  return (
    <svg className="ay-flor" style={{ animationDelay: d }} width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
      <path d="M7 1c2 2 2 4 0 6-2-2-2-4 0-6zM13 7c-2 2-4 2-6 0 2-2 4-2 6 0zM7 13c-2-2-2-4 0-6 2 2 2 4 0 6zM1 7c2-2 4-2 6 0-2 2-4 2-6 0z" fill={color} />
    </svg>
  );
}

const rapidas = ["Where are the keys?", "What does this page do?", "How do I add an app?"];

/** One link the helper gave: a page of rendimiento opens here, a page of the book in a new tab. */
function Enlace({ e, i, libro, cerrar }: { e: AyudanteEnlace; i: number; libro: string; cerrar: () => void }) {
  const tono = `ay-enlace tono-${i % 4}`;
  if (e.tipo === "libro") {
    return <a className={`${tono} libro`} href={libro + e.href.slice("libro:".length)} target="_blank" rel="noreferrer" style={{ animationDelay: `${0.15 + i * 0.1}s` }}>
      <span className="ay-enlace-que">{t("The book")}</span>{e.texto}<span aria-hidden="true" className="ay-flecha">↗</span></a>;
  }
  return <Link className={tono} to={e.href} onClick={cerrar} style={{ animationDelay: `${0.15 + i * 0.1}s` }}>
    {e.texto}<span aria-hidden="true" className="ay-flecha">→</span></Link>;
}

/** What the helper read, as a short line. */
function leidoTexto(leido: string[]) {
  return leido.map((l) => l.startsWith("pagina:") ? t("this page") : l.startsWith("libro:") ? (l.split("|")[1] ?? l.slice(6)) : l).join(", ");
}

/** The round button on every page and the helper it opens. */
export function Ayudante() {
  const location = useLocation();
  const pagina = location.pathname + location.search + location.hash;
  const [abierto, setAbierto] = useState(false);
  const [view, setView] = useState<AyudanteView>();
  const [texto, setTexto] = useState("");
  const [pensando, setPensando] = useState(false);
  const [error, setError] = useState<unknown>();
  const caja = useRef<HTMLInputElement>(null);
  const lista = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!abierto) return;
    if (!view) api.ayudante().then(setView, setError);
    caja.current?.focus();
    const tecla = (ev: KeyboardEvent) => ev.key === "Escape" && setAbierto(false);
    document.addEventListener("keydown", tecla);
    return () => document.removeEventListener("keydown", tecla);
  }, [abierto]);
  useEffect(() => { lista.current?.scrollTo({ top: lista.current.scrollHeight, behavior: "smooth" }); }, [view?.mensajes.length, pensando]);

  const plataforma = view?.ias.find((x) => x.id === "platform");
  const preguntar = (q: string, ia?: AIProvider) => {
    q = q.trim();
    if (!q || pensando) return;
    setPensando(true); setError(undefined); setTexto("");
    // The question shows at once; the answer comes when the model has it.
    const provisional: AyudanteMensaje = { id: -Date.now(), kind: "persona", body: q, pagina, enlaces: [], leido: [], costMicros: 0, createdAt: new Date().toISOString() };
    setView((v) => v && { ...v, mensajes: [...v.mensajes, provisional] });
    api.preguntarAyudante(q, pagina, ia).then(
      (r) => setView((v) => v && { ...v, mensajes: [...v.mensajes.filter((m) => m.id !== provisional.id), r.pregunta, r.respuesta] }),
      (e) => { setError(e); setView((v) => v && { ...v, mensajes: v.mensajes.filter((m) => m.id !== provisional.id) }); setTexto(q); },
    ).finally(() => setPensando(false));
  };
  const mensajes = view?.mensajes ?? [];
  const ultima = mensajes[mensajes.length - 1];
  const ultimaPregunta = [...mensajes].reverse().find((m) => m.kind === "persona");
  const ofrecer = !pensando && plataforma && ultima?.kind === "ayudante" && ultima.provider === "local" && ultimaPregunta;
  const quien = view?.ia === "local" ? t("Your local model · free") : view?.ia ? t("The platform's AI") : "";
  const cerrarEnTelefono = () => { if (window.matchMedia("(max-width: 760px)").matches) setAbierto(false); };

  return (
    <>
      {!abierto && (
        <button type="button" className="ayudante-boton" aria-label={t("Ask the hummingbird")} title={t("Ask the hummingbird")} onClick={() => setAbierto(true)}>
          <Colibri size={38} />
        </button>
      )}
      {abierto && (
        <section className="ayudante" role="dialog" aria-labelledby="ayudante-titulo">
          <header className="ay-cabeza">
            <div className="ay-picado" aria-hidden="true">{["a", "b", "c", "d", "e", "f"].map((k, i) => <i key={k} style={{ animationDelay: `${i * -0.5}s` }} />)}</div>
            <span className="ay-ave"><Colibri /></span>
            <div className="ay-titulos">
              <h2 id="ayudante-titulo">{t("Tsïntsuni, the hummingbird")}</h2>
              <span>{quien}{quien && " · "}{t("on {page}", { page: location.pathname })}</span>
            </div>
            <button type="button" className="ay-cerrar" aria-label={t("Close the helper")} onClick={() => setAbierto(false)}>×</button>
          </header>
          <div className="ay-lista" ref={lista} aria-live="polite">
            {view && mensajes.length === 0 && (
              <p className="ay-hola">{t("Ask me anything about rendimiento: where a setting is, what a page does, why a run failed. I answer with where to go.")}</p>
            )}
            {mensajes.map((m) => m.kind === "persona" ? (
              <div key={m.id} className="ay-msg persona">{m.body}</div>
            ) : (
              <div key={m.id} className="ay-msg respuesta">
                <span>{m.body}</span>
                {m.enlaces.length > 0 && <div className="ay-enlaces">{m.enlaces.map((e, i) => <Enlace key={e.href} e={e} i={i} libro={view?.libro ?? ""} cerrar={cerrarEnTelefono} />)}</div>}
                {m.leido.length > 0 && <span className="ay-leido">{t("Read:")} {leidoTexto(m.leido)}</span>}
                {m.provider === "platform" && m.costMicros > 0 && <span className="ay-leido">{t("The platform's AI · {cost}", { cost: dollars(m.costMicros) })}</span>}
              </div>
            ))}
            {pensando && <div className="ay-msg respuesta ay-pensando" aria-label={t("The hummingbird is thinking")}><Flor color="#E8890C" d="0s" /><Flor color="#D6246E" d=".2s" /><Flor color="#23408E" d=".4s" /></div>}
            {ofrecer && (
              <div className="ay-ofrece">
                <span>{plataforma!.mensajeMicros > 0 ? t("Not convinced? I can ask the platform's AI (about {cost}).", { cost: dollars(plataforma!.mensajeMicros) }) : t("Not convinced? I can ask the platform's AI.")}</span>
                <button type="button" onClick={() => preguntar(ultimaPregunta!.body, "platform")}>{t("Ask the platform's AI")}</button>
              </div>
            )}
            <ErrorBox error={error} />
          </div>
          <div className="ay-rapidas">
            {rapidas.map((q, i) => <button key={q} type="button" className={`tono-${i}`} disabled={pensando} onClick={() => preguntar(t(q))}>{t(q)}</button>)}
          </div>
          <form className="ay-escribir" onSubmit={(ev) => { ev.preventDefault(); preguntar(texto); }}>
            <label className="sr-only" htmlFor="ayudante-pregunta">{t("Your question")}</label>
            <input id="ayudante-pregunta" ref={caja} value={texto} maxLength={2000} placeholder={t("Ask the hummingbird…")} onChange={(ev) => setTexto(ev.target.value)} />
            <button type="submit" className="ay-enviar" aria-label={t("Ask")} disabled={pensando || !texto.trim()}>↑</button>
          </form>
        </section>
      )}
    </>
  );
}
