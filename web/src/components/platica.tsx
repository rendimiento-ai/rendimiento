// Platicar una entrada (D1 phone, D3 computer): a conversation about one Ruta entry, kept with it, between the
// people who see it and the AI they choose (one of the AI page's, each with what a message costs, or their own
// agent, which answers over MCP). Every message is signed. The AI keeps «La idea hasta ahora», which people may
// correct; it reads the entry, the related ones and the book, and says what it read. When the idea is ready,
// «Ya está: hacer encargos» (D2) has it proposed as encargos, which a person reviews and creates. Every target is
// at least 44 px; the dots and the glow stop under reduced motion.
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import {
  ApiError, api, dollars, session, timeAgo, type PlaticaEncargosView, type PlaticaIA, type PlaticaIAId, type PlaticaMensaje,
  type PlaticaView, type PropuestaEncargo, type PropuestaView, type RutaEntry,
} from "../api";
import { ErrorBox, usePoll } from "./ui";
import { t, tn } from "../i18n";
import { TextoConFormato } from "./texto";
import { Dificultad, routeLabel } from "./encargos";
import { Esqueleto, Hueso } from "./esqueleto";
import { useConexion } from "../conexion";
import { apagado, PorQueApagado } from "./conexion-aviso";
import { anotar } from "../uso";

/** The conversation of an entry, polled: every 4 seconds while it is open (so two people see each other), else every 30. */
export function usePlatica(id: number, abierta: boolean) {
  return usePoll(() => api.platica(id), [id, abierta], abierta ? 4000 : 30000);
}

/** An AI of the selector in words: who it is and what a message costs. */
function iaNombre(x: PlaticaIA): string {
  switch (x.id) {
    case "agente":
      return t("My agent (answers over MCP)");
    case "local":
      return t("Local model · {model} · free", { model: x.model ?? "" });
    case "own":
      return x.mensajeMicros > 0 ? t("Your key · {model} · about {cost} a message", { model: x.model ?? "", cost: dollars(x.mensajeMicros) }) : t("Your key · {model}", { model: x.model ?? "" });
  }
  return x.mensajeMicros > 0 ? t("The platform's AI · {model} · about {cost} a message", { model: x.model ?? "", cost: dollars(x.mensajeMicros) }) : t("The platform's AI · {model}", { model: x.model ?? "" });
}

/** Who answers, short, for the line under the box. */
function iaCorta(id: PlaticaIAId): string {
  return { agente: t("with your own agent"), local: t("with the local model"), own: t("with your own key"), platform: t("with the platform's AI") }[id];
}

/** What the AI read, in words: «Leí: Ruta 92, 73 y el libro, «AI»». */
function Leido({ refs }: { refs: string[] }) {
  if (refs.length === 0) return null;
  const ruta = refs.filter((r) => r.startsWith("ruta:")).map((r) => r.slice(5));
  const libro = refs.filter((r) => r.startsWith("libro:")).map((r) => r.slice(6).split("|"));
  return (
    <span className="pl-leido">
      {t("Read:")}{" "}
      {ruta.length > 0 && t("Ruta {list}", { list: ruta.join(", ") })}
      {libro.length > 0 && <>{ruta.length > 0 ? t(" and the book, ") : t("the book, ")}{libro.map(([path, title], i) => (
        <span key={path}>{i > 0 && ", "}<a href={`https://github.com/rendimiento-ai/rendimiento/blob/main/docs/content/${path.replace(/\/$/, "")}.md`} target="_blank" rel="noreferrer">«{title ?? path}»</a></span>
      ))}</>}
    </span>
  );
}

/** A robot's face, for the AI's and an agent's messages. */
function Cara() {
  return (
    <span className="pl-cara" aria-hidden="true">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" strokeWidth="2.4"><rect x="5" y="8" width="14" height="11" rx="3" /><path d="M12 8V4" /></svg>
    </span>
  );
}

/** One message, signed: the person's own on the right, the others' on the left with who wrote it. */
function Mensaje({ m, me }: { m: PlaticaMensaje; me: string }) {
  if (m.tipo === "aviso") return <li className="pl-aviso" role="note">{m.body}</li>;
  const mio = m.tipo === "persona" && m.author === me;
  const firma = m.tipo === "ia"
    ? t("AI · {model}", { model: m.model || m.author }) + (m.provider === "local" ? ` · ${t("local, free")}` : m.costMicros > 0 ? ` · ${dollars(m.costMicros)}` : "")
    : m.tipo === "agente" ? t("{name} · their agent", { name: m.author }) : m.author;
  return (
    <li className={`pl-msj ${mio ? "mio" : m.tipo === "persona" ? "persona" : "ia"}`}>
      {!mio && m.tipo !== "persona" && <Cara />}
      <div className="pl-burbuja">
        {!mio && <span className="pl-firma">{firma}</span>}
        <TextoConFormato text={m.body} className="pl-texto" />
        {m.tipo === "ia" && <Leido refs={m.read} />}
        {m.note && <span className="pl-leido">{m.note}</span>}
        <span className="pl-cuando">{timeAgo(m.createdAt)}</span>
      </div>
    </li>
  );
}

/** «La idea hasta ahora»: the AI keeps it after each answer; a person may correct it. */
function Idea({ e, v, onChange }: { e: RutaEntry; v: PlaticaView; onChange: (v: PlaticaView) => void }) {
  const [editando, setEditando] = useState(false);
  const [texto, setTexto] = useState(v.platica.idea);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const conexion = useConexion();
  useEffect(() => { if (!editando) setTexto(v.platica.idea); }, [v.platica.idea, editando]);
  const lineas = v.platica.idea.split("\n").map((l) => l.replace(/^\s*[-*•]\s*/, "").trim()).filter(Boolean);
  const falta = (l: string) => !!v.faltaDecidir && (l.startsWith(v.faltaDecidir) || l.startsWith("Falta decidir") || l.startsWith("Still to decide"));
  return (
    <section className={`pl-idea${v.platica.thinking ? " brilla" : ""}`} aria-labelledby={`pl-idea-${e.id}`}>
      <h2 id={`pl-idea-${e.id}`}>{t("The idea so far")}</h2>
      {editando ? (
        <>
          <label className="sr-only" htmlFor={`pl-idea-texto-${e.id}`}>{t("The idea so far")}</label>
          <textarea id={`pl-idea-texto-${e.id}`} rows={6} value={texto} onChange={(x) => setTexto(x.target.value)} />
          <ErrorBox error={err} />
          <div className="et-acciones">
            <button type="button" className="et-boton principal" {...apagado(conexion, busy)} onClick={() => {
              setBusy(true); setErr(undefined);
              api.platicaIdea(e.id, texto).then((nv) => { setBusy(false); setEditando(false); onChange(nv); }, (x) => { setBusy(false); setErr(x); });
            }}>{t("Save")}</button>
            <button type="button" className="et-boton" onClick={() => setEditando(false)}>{t("Cancel")}</button>
          </div>
        </>
      ) : (
        <>
          {lineas.length > 0
            ? <ul>{lineas.map((l, i) => <li key={i} className={falta(l) ? "falta" : undefined}>{l}</li>)}</ul>
            : <p className="pl-nota">{t("Nothing yet: it fills in as you talk.")}</p>}
          <div className="pl-idea-pie">
            <span className="pl-nota">{v.platica.ideaBy ? t("Last written by {who}; you can correct it.", { who: v.platica.ideaBy }) : t("The AI writes it as you talk; you can correct it.")}</span>
            <button type="button" className="et-boton chico" {...apagado(conexion)} onClick={() => setEditando(true)}>{t("Correct")}</button>
          </div>
        </>
      )}
    </section>
  );
}

/** The three dots while the AI writes. */
function Escribiendo() {
  return (
    <li className="pl-msj ia" aria-live="polite">
      <Cara />
      <div className="pl-burbuja pl-puntos" role="status" aria-label={t("The AI is writing")}><i /><i /><i /></div>
    </li>
  );
}

/** The conversation's skeleton, while it is on its way. */
function EsqPlatica() {
  return (
    <Esqueleto className="pl-esq">
      <Hueso h={96} r={18} />
      <Hueso h={48} w="70%" r={18} style={{ alignSelf: "flex-end" }} />
      <Hueso h={72} w="80%" r={18} />
      <Hueso h={48} w="60%" r={18} style={{ alignSelf: "flex-end" }} />
      <Hueso h={50} r={999} />
    </Esqueleto>
  );
}

/**
 * The tab «Platicarla»: the idea, the messages, the box to write in (with the AI to choose and what it spent),
 * and «Ya está: hacer encargos». On a computer the idea and «¿Ya está la idea?» go in a column on the right.
 */
export function Platica({ e, me, v, error, reload, setV }: {
  e: RutaEntry; me: string; v?: PlaticaView; error: unknown; reload: () => void; setV: (v: PlaticaView) => void;
}) {
  const [texto, setTexto] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const [tope, setTope] = useState(false); // the cap was reached: ask before going on
  const [convertir, setConvertir] = useState<"encargos" | "decision" | undefined>();
  const conexion = useConexion();
  const fin = useRef<HTMLLIElement>(null);
  const cuantos = v?.mensajes.length ?? 0;
  useEffect(() => { if (cuantos > 0) fin.current?.scrollIntoView?.({ block: "nearest" }); }, [cuantos, v?.platica.thinking]);

  if (convertir && v) {
    return <Convertir e={e} v={v} soloDecision={convertir === "decision"} onBack={() => { setConvertir(undefined); reload(); }} />;
  }
  if (!v) return <>{<ErrorBox error={error} />}{!error && <EsqPlatica />}</>;

  const enviar = (continuar = false) => {
    const msg = texto.trim();
    if (!msg) return;
    setBusy(true); setErr(undefined);
    api.platicar(e.id, msg, continuar).then((nv) => {
      setBusy(false); setTexto(""); setTope(false); setV(nv); anotar("talk_entry");
    }, (x) => {
      setBusy(false);
      if (x instanceof ApiError && x.status === 402) setTope(true);
      else setErr(x);
    });
  };
  const elegir = (id: PlaticaIAId) => api.platicaIA(e.id, id).then(setV, setErr);
  const hay = v.mensajes.some((m) => m.tipo === "persona");
  const otros = v.participantes.filter((p) => p !== me && v.mensajes.some((m) => m.tipo === "persona" && m.author === p));
  const leyo = [...v.mensajes].reverse().find((m) => m.tipo === "ia")?.read ?? [];
  const esperaAgente = v.ia === "agente" && v.mensajes.length > 0 && v.mensajes[v.mensajes.length - 1].tipo === "persona";

  return (
    <div className="pl">
      <div className="pl-charla">
        <div className="pl-idea-movil"><Idea e={e} v={v} onChange={setV} /></div>
        <ol className="pl-mensajes" aria-label={t("The conversation")} aria-live="polite">
          {v.mensajes.length === 0 && <li className="pl-vacio">{t("Ask, propose, doubt: the AI answers with what the Ruta and the book say, and keeps the idea. Others who can see this entry can join in.")}</li>}
          {v.mensajes.map((m) => <Mensaje key={m.id} m={m} me={me} />)}
          {v.platica.thinking && <Escribiendo />}
          {esperaAgente && <li className="pl-aviso" role="note">{t("Waiting for your own agent: it answers with the platicas_esperando and platica_responder tools.")}</li>}
          <li ref={fin} aria-hidden="true" className="pl-fin" />
        </ol>
        <form className="pl-escribir" onSubmit={(x) => { x.preventDefault(); enviar(); }}>
          {tope && (
            <div className="alert info stack" role="alertdialog" aria-labelledby={`pl-tope-${e.id}`}>
              <strong id={`pl-tope-${e.id}`}>{t("This conversation reached its cap of {cap}.", { cap: dollars(v.topeMicros) })}</strong>
              <span className="small">{t("It has spent {spent}. Go on, for up to {cap} more?", { spent: dollars(v.platica.spentMicros), cap: dollars(v.topeMicros) })}</span>
              <div className="et-acciones">
                <button type="button" className="et-boton principal" {...apagado(conexion, busy)} onClick={() => enviar(true)}>{t("Go on")}</button>
                <button type="button" className="et-boton" onClick={() => setTope(false)}>{t("Not now")}</button>
              </div>
            </div>
          )}
          <label className="pl-quien">
            <span>{t("Who answers")}</span>
            <select className="et-select" value={v.ia} {...apagado(conexion)} onChange={(x) => elegir(x.target.value as PlaticaIAId)}>
              {v.ias.map((x) => <option key={x.id} value={x.id}>{iaNombre(x)}</option>)}
            </select>
          </label>
          <div className="pl-caja">
            <label className="sr-only" htmlFor={`pl-msj-${e.id}`}>{t("Your message")}</label>
            <textarea id={`pl-msj-${e.id}`} rows={1} value={texto} maxLength={8000} placeholder={t("Write or ask…")}
              onChange={(x) => setTexto(x.target.value)}
              onKeyDown={(x) => { if (x.key === "Enter" && !x.shiftKey) { x.preventDefault(); enviar(); } }} />
            <button type="submit" className="et-boton principal redondo" aria-label={t("Send")} title={t("Send")} {...apagado(conexion, busy || !texto.trim())}>↑</button>
          </div>
          <ErrorBox error={err} />
          <PorQueApagado />
          <div className="pl-pie">
            <span className="pl-nota">
              {t("Spent {spent} of its {cap} · {who}", { spent: dollars(v.platica.spentMicros), cap: dollars(v.topeMicros + v.platica.allowedMicros), who: iaCorta(v.ia) })}
              {otros.length > 0 && <> · {t("also here: {who}", { who: otros.join(", ") })}</>}
            </span>
            <button type="button" className="et-boton principal naranja pl-ya-movil" {...apagado(conexion, !hay)} onClick={() => setConvertir("encargos")}>{t("Done: make encargos")}</button>
          </div>
        </form>
      </div>
      <aside className="pl-lado">
        <Idea e={e} v={v} onChange={setV} />
        <section className="pl-listo">
          <strong>{t("Is the idea ready?")}</strong>
          <span>{t("The AI splits it into encargos, each with its app, difficulty and cost. You review them before they are made.")}</span>
          <button type="button" className="et-boton principal naranja" {...apagado(conexion, !hay)} onClick={() => setConvertir("encargos")}>{t("Make encargos")}</button>
          <button type="button" className="et-boton pl-solo" {...apagado(conexion, !hay)} onClick={() => setConvertir("decision")}>{t("Only save the decision")}</button>
        </section>
        {leyo.length > 0 && <p className="pl-nota"><Leido refs={leyo} /></p>}
        {v.platica.encargos.length > 0 && (
          <p className="pl-nota">
            {tn(v.platica.encargos.length, "Made of it: encargo {list}", "Made of it: encargos {list}", { list: v.platica.encargos.map((n) => `#${n}`).join(", ") })}
            {" · "}<Link to="/ruta?tab=encargos">{t("see them")}</Link>
          </p>
        )}
      </aside>
    </div>
  );
}

/** One encargo of the proposal, as the person changes it. */
interface Borrador extends PropuestaEncargo { crear: boolean }

/**
 * «De idea a encargos» (D2): what was decided (to save as a decision entry, linked to this one) and the
 * encargos the AI proposed, each to keep or not, with its title, app (required), difficulty, cost, who does
 * it and what it comes after; «+ Agregar otro»; «Crearlos» or «Crear y mandar» (to the coding agent).
 */
function Convertir({ e, v, soloDecision, onBack }: { e: RutaEntry; v: PlaticaView; soloDecision: boolean; onBack: () => void }) {
  const [prop, setProp] = useState<PropuestaView>();
  const [items, setItems] = useState<Borrador[]>([]);
  const [decidido, setDecidido] = useState("");
  const [guardar, setGuardar] = useState(true);
  const [err, setErr] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const [hecho, setHecho] = useState<PlaticaEncargosView>();
  const [tope, setTope] = useState(false);
  const conexion = useConexion();
  const pedir = () => {
    setErr(undefined); setTope(false); setProp(undefined);
    api.proponerEncargos(e.id).then((p) => {
      setProp(p); setDecidido(p.decidido);
      setItems(soloDecision ? [] : p.encargos.map((x) => ({ ...x, crear: true })));
    }, (x) => { if (x instanceof ApiError && x.status === 402) setTope(true); else setErr(x); });
  };
  useEffect(pedir, [e.id]);

  const precio = (d: number) => prop?.precios[d - 1] ?? 0;
  const cambiar = (i: number, c: Partial<Borrador>) => setItems(items.map((x, j) => {
    if (j !== i) return x;
    const n = { ...x, ...c };
    if (c.dificultad) {
      n.costoMicros = precio(c.dificultad);
      n.ruta = n.costoMicros <= (prop?.encargoAutoMicros ?? 0) ? "platform" : "local";
    }
    return n;
  }));
  const elegidos = items.filter((x) => x.crear);
  const total = elegidos.reduce((a, x) => a + (x.ruta === "platform" ? x.costoMicros : 0), 0);
  const faltaApp = elegidos.some((x) => !x.app || !x.titulo.trim());
  const crear = (mandar: boolean) => {
    // The numbers of what is kept change when some are left out: «after» follows them.
    const nuevo = new Map<number, number>();
    items.forEach((x, i) => { if (x.crear) nuevo.set(i + 1, nuevo.size + 1); });
    setBusy(true); setErr(undefined);
    api.encargosDePlatica(e.id, {
      encargos: elegidos.map((x) => ({ titulo: x.titulo.trim(), app: x.app, dificultad: x.dificultad, detalle: x.detalle, ruta: x.ruta,
        despuesDe: x.despuesDe.map((d) => nuevo.get(d)).filter((d): d is number => d !== undefined) })),
      decision: { guardar, texto: decidido.trim() },
      mandar,
    }).then((h) => { setBusy(false); setHecho(h); anotar("make_encargos"); }, (x) => { setBusy(false); setErr(x); });
  };

  if (hecho) {
    return (
      <div className="pl-convertir">
        <div className="pl-conv-cabeza"><h2>{t("Made")}</h2></div>
        {hecho.decision && <p>{t("The decision is in the Ruta:")} <Link to={`/ruta?tab=ruta&entrada=${hecho.decision.id}`}>{hecho.decision.title}</Link></p>}
        {hecho.encargos.length > 0 && (
          <ul className="pl-hechos">{hecho.encargos.map((x) => (
            <li key={x.id}><strong>#{x.id}</strong> {x.title} <span className="pl-nota">· {x.app} · {t(routeLabel[x.route === "platform" ? "platform" : "local"])}</span></li>
          ))}</ul>
        )}
        {hecho.enviados > 0 && <p className="pl-nota">{tn(hecho.enviados, "One went to the coding agent.", "{n} went to the coding agent.")}</p>}
        {hecho.note && <p className="pl-nota">{t("Not sent: {why}", { why: hecho.note })}</p>}
        <div className="et-acciones">
          <Link className="et-boton principal" to="/ruta?tab=encargos">{t("See the encargos")}</Link>
          <button type="button" className="et-boton" onClick={onBack}>{t("← Keep talking")}</button>
        </div>
      </div>
    );
  }

  return (
    <div className="pl-convertir">
      <button type="button" className="et-boton pl-volver" onClick={onBack}>{t("← Keep talking")}</button>
      <div className="pl-conv-cabeza">
        <svg width="54" height="54" viewBox="0 0 54 54" aria-hidden="true" className="pl-conv-icono">
          <circle cx="27" cy="27" r="26" className="fondo" /><path d="M17 18h20v22H17z" className="hoja" />
          <path d="M21 25h12M21 30h12M21 35h8" className="raya" /><circle cx="38" cy="16" r="7" className="listo" /><path d="M35 16l2 2 4-4" className="palomita" />
        </svg>
        <div>
          <h2>{soloDecision ? t("Save the decision") : t("From idea to encargos")}</h2>
          {prop && !soloDecision && <span className="pl-nota">{tn(prop.encargos.length, "The AI made it one encargo. Change what you like before making it.", "The AI split it into {n}. Change what you like before making them.")}</span>}
        </div>
      </div>
      {tope && (
        <div className="alert info stack" role="alert">
          <span>{t("This conversation reached its cap of {cap}. To go on, write once more and say yes.", { cap: dollars(v.topeMicros) })}</span>
          <div><button type="button" className="et-boton" onClick={onBack}>{t("← Keep talking")}</button></div>
        </div>
      )}
      <ErrorBox error={err} />
      {!prop && !err && !tope && (
        <Esqueleto className="pl-esq">
          <span className="pl-nota">{t("The AI is reading the conversation…")}</span>
          <Hueso h={90} r={18} /><Hueso h={110} r={18} /><Hueso h={110} r={18} />
        </Esqueleto>
      )}
      {!!err && <div><button type="button" className="et-boton" onClick={pedir}>{t("Ask again")}</button></div>}
      {prop && (
        <>
          <section className="pl-decidido">
            <label className="field"><span><strong>{t("What was decided")}</strong></span>
              <textarea rows={3} value={decidido} maxLength={4000} onChange={(x) => setDecidido(x.target.value)} />
            </label>
            <label className="pl-check">
              <input type="checkbox" checked={guardar} onChange={(x) => setGuardar(x.target.checked)} />
              {t("Save it as a decision in the Ruta, linked to entry {n}", { n: e.id })}
            </label>
          </section>
          {!soloDecision && (
            <ol className="pl-propuesta">
              {items.map((x, i) => {
                const n = i + 1;
                return (
                  <li key={i} className={x.crear ? undefined : "fuera"}>
                    <div className="pl-prop-fila">
                      <input type="checkbox" checked={x.crear} aria-label={t("Make number {n}", { n })} onChange={(c) => cambiar(i, { crear: c.target.checked })} />
                      <label className="field pl-prop-titulo"><span>{t("Title of number {n}", { n })}</span>
                        <input value={x.titulo} maxLength={200} onChange={(c) => cambiar(i, { titulo: c.target.value })} />
                      </label>
                    </div>
                    <div className="pl-prop-datos">
                      <label className="field"><span>{t("App")}</span>
                        <select className="et-select" value={x.app} required onChange={(c) => cambiar(i, { app: c.target.value })}>
                          <option value="">{t("Choose one…")}</option>
                          {prop.apps.map((a) => <option key={a} value={a}>{a}</option>)}
                        </select>
                      </label>
                      <label className="field"><span>{t("Difficulty")}</span>
                        <span className="row" style={{ gap: 6 }}>
                          <Dificultad d={x.dificultad} small />
                          <select className="et-select" value={x.dificultad} onChange={(c) => cambiar(i, { dificultad: Number(c.target.value) })}>
                            {Array.from({ length: 10 }, (_, d) => <option key={d + 1} value={d + 1}>{d + 1}</option>)}
                          </select>
                        </span>
                      </label>
                      <label className="field"><span>{t("Who does it")}</span>
                        <select className="et-select" value={x.ruta} onChange={(c) => cambiar(i, { ruta: c.target.value as "platform" | "local" })}>
                          <option value="platform">{t("The platform's AI · about {cost}", { cost: dollars(x.costoMicros) })}</option>
                          <option value="local">{t("Your own agent")}</option>
                        </select>
                      </label>
                      {n > 1 && (
                        <label className="field"><span>{t("After")}</span>
                          <select className="et-select" value={x.despuesDe[0] ?? 0} onChange={(c) => cambiar(i, { despuesDe: Number(c.target.value) ? [Number(c.target.value)] : [] })}>
                            <option value={0}>{t("any time")}</option>
                            {Array.from({ length: n - 1 }, (_, d) => <option key={d + 1} value={d + 1}>{t("after number {n}", { n: d + 1 })}</option>)}
                          </select>
                        </label>
                      )}
                    </div>
                    {!x.app && x.crear && <span className="pl-falta">{t("Choose its app: without it, no agent can take it.")}</span>}
                    {x.detalle && <details className="pl-detalle"><summary>{t("Detail")}</summary>
                      <textarea rows={4} value={x.detalle} maxLength={8000} aria-label={t("Detail")} onChange={(c) => cambiar(i, { detalle: c.target.value })} />
                    </details>}
                  </li>
                );
              })}
            </ol>
          )}
          {!soloDecision && (
            <button type="button" className="et-boton pl-otro" onClick={() => setItems([...items, { titulo: "", app: prop.apps.length === 1 ? prop.apps[0] : "", dificultad: 3, detalle: "",
              despuesDe: [], costoMicros: precio(3), ruta: precio(3) <= prop.encargoAutoMicros ? "platform" : "local", crear: true }])}>{t("+ Add another")}</button>
          )}
          <div className="pl-crear">
            <span>
              {soloDecision || elegidos.length === 0
                ? <strong>{t("Save the decision")}</strong>
                : <><strong>{tn(elegidos.length, "Make {n} encargo", "Make {n} encargos")}</strong> · {t("about {cost} with the platform's AI", { cost: dollars(total) })}</>}
            </span>
            <div className="et-acciones">
              {soloDecision || elegidos.length === 0 ? (
                <button type="button" className="et-boton principal" {...apagado(conexion, busy || !guardar || !decidido.trim())} onClick={() => crear(false)}>{t("Save the decision")}</button>
              ) : (
                <>
                  <button type="button" className="et-boton" {...apagado(conexion, busy || faltaApp)} onClick={() => crear(false)}>{t("Make them")}</button>
                  <button type="button" className="et-boton principal" {...apagado(conexion, busy || faltaApp || prop.codeAgent === "off")}
                    title={prop.codeAgent === "off" ? t("the coding agent is off") : undefined} onClick={() => crear(true)}>{t("Make and send")}</button>
                </>
              )}
            </div>
            <span className="pl-nota">{t("Each encargo carries the conversation and the decision, so the agent knows why. The ones for your own agent wait for it.")}</span>
            {prop.costMicros > 0 && <span className="pl-nota">{t("The proposal cost {cost} ({model}).", { cost: dollars(prop.costMicros), model: prop.model })}</span>}
          </div>
        </>
      )}
    </div>
  );
}

/** On the AI page: what one conversation may spend on paid models before it asks whether to go on. */
export function TopePlatica() {
  const { data, error, reload } = usePoll(api.platicaTope, [], 0);
  const admin = session.role === "admin";
  const [dolares, setDolares] = useState<number>();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const [saved, setSaved] = useState(false);
  const conexion = useConexion();
  useEffect(() => { if (data) setDolares(data.capMicros / 1e6); }, [data]);
  return (
    <section className="card stack" style={{ marginTop: 16 }} aria-labelledby="ia-platica">
      <h2 id="ia-platica" style={{ margin: 0 }}>{t("Talking entries over")}</h2>
      <p className="small muted" style={{ margin: 0 }}>{t("Each entry of the Ruta can be talked over with an AI. This is what one conversation may spend on paid models before it asks whether to go on; the local model and your own agent cost nothing here.")}</p>
      <ErrorBox error={error ?? err} />
      {dolares === undefined ? <Hueso h={44} w={200} r={12} /> : (
        <div className="row" style={{ alignItems: "flex-end", flexWrap: "wrap" }}>
          <label className="field ia-narrow"><span>{t("Dollars per conversation")}</span>
            <input disabled={!admin} type="number" min={0.05} max={20} step={0.05} value={dolares} onChange={(x) => setDolares(Number(x.target.value))} />
          </label>
          {admin && (
            <button type="button" className="primary" {...apagado(conexion, busy || Math.round(dolares * 1e6) === data?.capMicros)} onClick={() => {
              setBusy(true); setErr(undefined);
              api.putPlaticaTope(dolares).then(() => { setBusy(false); setSaved(true); reload(); }, (x) => { setBusy(false); setErr(x); });
            }}>{t("Save")}</button>
          )}
          {saved && <span className="badge ok">{t("Saved")}</span>}
        </div>
      )}
    </section>
  );
}
