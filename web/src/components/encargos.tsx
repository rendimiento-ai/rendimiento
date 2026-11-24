// Encargos: work asked of agents (la Ruta nueva, A1). Each card says what was asked, of which app, how hard
// it is and what it would cost, with one main action (send it to the coding agent, leave it to your own
// agent, review what came back), «Comentar» and a «⋯» menu for the rest. A card that cannot be chosen
// says why, with what fixes it right there (its app). What is chosen goes in one go from the bar at
// the bottom. Every target is at least 44 px.
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { api, dollars, timeAgo, type Cola, type Comentario, type Encargo, type EncargoStatus } from "../api";
import { ErrorBox, usePoll } from "./ui";
import { t, tn } from "../i18n";
import { TextoConFormato } from "./texto";
import { Comentarios, porTrabajo } from "./comentarios";
import { EstimateBar } from "./boleta";
import { EsqTarjetas } from "./esqueleto";
import { useConexion } from "../conexion";
import { apagado } from "./conexion-aviso";

export const statusLabel: Record<EncargoStatus, string> = {
  pending: "Pending", taken: "Taken", waiting: "Waiting", delivered: "Delivered", reviewed: "Reviewed", cancelled: "Dropped",
};
export const statusClass: Record<EncargoStatus, string> = {
  pending: "", taken: "live", waiting: "warn live", delivered: "warn", reviewed: "ok", cancelled: "",
};
export const eventLabel: Record<string, string> = {
  created: "created", taken: "taken", waiting: "waiting", resumed: "done waiting", delivered: "delivered",
  reviewed: "reviewed", routed: "routed", estimate: "asked to estimate it again", sent: "sent it to the coding agent", returned: "sent back", released: "let go", cancelled: "dropped",
  noted: "added to it", fixed: "ran a fix", ran: "ran a command", app: "chose its app", stop: "asked the coding agent to stop",
};

export const routeLabel = { platform: "For the platform's AI", local: "For your own agent" };

/** A difficulty's color, from green (1, cheap) to red (10, expensive), in either theme. */
export const difficultyColor = (d: number) => `color-mix(in oklab, var(--bad) ${Math.round(((d - 1) / 9) * 100)}%, var(--ok))`;

/** A difficulty in words: easy, medium, hard. */
export const difficultyWord = (d: number) => (d <= 3 ? t("Easy") : d <= 7 ? t("Medium") : t("Hard"));

/** A difficulty as a colored square with its number. */
export function Dificultad({ d, small }: { d: number; small?: boolean }) {
  return (
    <span className={`dif-cuadro${small ? " chico" : ""}`} style={{ ["--c" as string]: difficultyColor(d) }} title={t("Difficulty {d} of 10", { d })}>
      {d}<span className="sr-only"> {t("of 10")}</span>
    </span>
  );
}

/** Whether the coding agent will take it by itself: in its queue (the queue's page lists them). */
export const enFila = (e: Encargo, cola?: Cola) => !!cola?.next.some((x) => x.id === e.id);

/**
 * Why an encargo cannot be chosen to send to the coding agent, or "" when it can. The checkbox only
 * shows for pending ones; when it is off, this is said beside it.
 */
export function porQueNo(e: Encargo, cola?: Cola): string {
  if (e.status !== "pending") return e.status === "taken" || e.status === "waiting" ? t("{agent} has it", { agent: e.keyName || t("an agent") }) : "";
  if (!e.app) return t("its app is missing");
  if (enFila(e, cola)) return t("it is in the queue already");
  if (!cola || cola.mode === "off") return t("the coding agent is off");
  return "";
}

/** The apps of the organization, for the selects that choose one. */
export function useApps() {
  return usePoll(() => api.apps().catch(() => []), [], 0).data ?? [];
}

/** Choosing the app of an encargo asked without one, right on its card (or its line on the front page). */
export function EscogerApp({ e, onChange, compacto }: { e: Encargo; onChange: () => void; compacto?: boolean }) {
  const apps = useApps();
  const [err, setErr] = useState<unknown>();
  const conexion = useConexion();
  const id = `app-de-${e.id}${compacto ? "-p" : ""}`;
  return (
    <div className={compacto ? "et-app compacto" : "et-falta"}>
      {!compacto && <span><strong>{t("It does not say which app it is about.")}</strong> {t("The agent works in an app's repository; without one it cannot be sent.")}</span>}
      <label htmlFor={id} className={compacto ? "sr-only" : "et-etiqueta"}>{compacto ? t("App of encargo {n}", { n: e.id }) : t("App")}</label>
      <select id={id} className="et-select" value="" {...apagado(conexion)} onChange={(ev) => {
        if (!ev.target.value) return;
        setErr(undefined);
        api.setEncargoApp(e.id, ev.target.value).then(onChange, setErr);
      }}>
        <option value="">{compacto ? t("Choose its app…") : t("Choose one…")}</option>
        {apps.map((a) => <option key={a.name} value={a.name}>{a.name}</option>)}
      </select>
      <ErrorBox error={err} />
    </div>
  );
}

/** What some encargos would cost: the sum of their estimates. */
export const suma = (l: Encargo[]) => dollars(l.reduce((n, e) => n + (e.estimateMicros ?? 0), 0));

/** The bar at the bottom while encargos are chosen: how many, about what they would cost, and sending them in one go. */
export function BarraMandar({ chosen, onClear, onSent }: { chosen: Encargo[]; onClear: () => void; onSent: (n: number) => void }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const conexion = useConexion();
  if (!chosen.length) return null;
  const send = () => {
    setBusy(true); setErr(undefined);
    api.doEncargos(chosen.map((e) => e.id)).then((r) => { setBusy(false); onSent(r.sent); }, (x) => { setBusy(false); setErr(x); });
  };
  return (
    <div className="barra-mandar" role="region" aria-label={t("Chosen encargos")}>
      <span className="barra-mandar-texto">
        <strong>{tn(chosen.length, "{n} encargo chosen", "{n} encargos chosen")}</strong>
        <span>{t("about {cost}, at most twice that", { cost: suma(chosen) })}</span>
        <ErrorBox error={err} />
      </span>
      <button type="button" className="barra-quitar" onClick={onClear}>{t("Clear")}</button>
      <button type="button" className="barra-enviar" {...apagado(conexion, busy)} onClick={send}>
        {busy ? t("Sending…") : tn(chosen.length, "Send it", "Send the {n}")}
      </button>
    </div>
  );
}

/** A choice that waits for a yes on the page itself (never the browser's confirm()): what it does, and yes or no. */
export function Confirmar({ pregunta, si, motivo, onSi, onNo }: { pregunta: string; si: string; motivo?: string; onSi: (motivo: string) => void; onNo: () => void }) {
  const [texto, setTexto] = useState("");
  return (
    <div className="et-confirma" role="alertdialog" aria-label={pregunta}>
      <strong>{pregunta}</strong>
      {motivo && (
        <label className="field"><span>{motivo}</span>
          <textarea rows={2} value={texto} autoFocus onChange={(e) => setTexto(e.target.value)} />
        </label>
      )}
      <div className="et-acciones">
        <button type="button" className="et-boton peligro" onClick={() => onSi(texto.trim())}>{si}</button>
        <button type="button" className="et-boton" autoFocus={!motivo} onClick={onNo}>{t("No, leave it")}</button>
      </div>
    </div>
  );
}

/** The «⋯» menu: the rest of what can be done with an encargo. Escape, a tap outside or a choice closes it. */
function Menu({ label, items }: { label: string; items: { label: string; onClick: () => void; peligro?: boolean }[] }) {
  const [open, setOpen] = useState(false);
  const caja = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const fuera = (ev: PointerEvent) => !caja.current?.contains(ev.target as Node) && setOpen(false);
    const tecla = (ev: KeyboardEvent) => ev.key === "Escape" && setOpen(false);
    document.addEventListener("pointerdown", fuera);
    document.addEventListener("keydown", tecla);
    caja.current?.querySelector<HTMLButtonElement>("[role=menuitem]")?.focus();
    return () => { document.removeEventListener("pointerdown", fuera); document.removeEventListener("keydown", tecla); };
  }, [open]);
  if (!items.length) return null;
  return (
    <div className="et-menu" ref={caja}>
      <button type="button" className="et-boton redondo" aria-haspopup="menu" aria-expanded={open} aria-label={label} title={label} onClick={() => setOpen(!open)}>⋯</button>
      {open && (
        <div role="menu" aria-label={label} className="et-menu-lista">
          {items.map((it) => (
            <button key={it.label} type="button" role="menuitem" className={it.peligro ? "peligro" : undefined} onClick={() => { setOpen(false); it.onClick(); }}>{it.label}</button>
          ))}
        </div>
      )}
    </div>
  );
}

/** One encargo as a card: who asked, its app and route, how hard, its one main action, comments and the «⋯» menu. */
export function EncargoCard({ e, cola, onChange, chosen, onChoose, comentarios }: {
  e: Encargo; cola?: Cola; onChange: () => void; chosen: boolean; onChoose: (on: boolean) => void; comentarios: Comentario[];
}) {
  const [historia, setHistoria] = useState(false);
  const [full, setFull] = useState<Encargo>();
  const [comentando, setComentando] = useState(false);
  const [confirma, setConfirma] = useState<"soltar" | "devolver">();
  const [err, setErr] = useState<unknown>();
  const conexion = useConexion();
  const act = (p: Promise<unknown>) => { setErr(undefined); p.then(onChange, setErr); };
  const verHistoria = () => {
    if (!historia) api.encargo(e.id).then(setFull, () => undefined);
    setHistoria(!historia);
  };
  // A link to this encargo (#encargo-N) brings it into view.
  const card = useRef<HTMLElement>(null);
  useEffect(() => {
    if (location.hash === `#encargo-${e.id}`) card.current?.scrollIntoView({ block: "center" });
  }, [e.id]);

  const pending = e.status === "pending";
  const open = e.status !== "reviewed" && e.status !== "cancelled";
  const motivo = porQueNo(e, cola);
  const choosable = pending && !motivo;
  const agentOn = !!cola && cola.mode !== "off";
  const delCoder = (e.status === "taken" || e.status === "waiting") && !!cola?.now.some((x) => x.id === e.id);
  const fila = pending && enFila(e, cola);

  // The one main action.
  let principal: ReactNode = null;
  if (e.status === "delivered") {
    principal = <button type="button" className="et-boton principal verde" {...apagado(conexion)} onClick={() => act(api.reviewEncargo(e.id, true))}>{t("Reviewed ✓")}</button>;
  } else if (delCoder) {
    principal = <Link className="et-boton principal" to="/ruta?tab=agente">{t("See what it does")}</Link>;
  } else if (fila) {
    principal = <Link className="et-boton principal suave" to="/ruta?tab=agente">{t("See the queue")}</Link>;
  } else if (pending && e.app && e.route === "local" && !e.routeChosen) {
    principal = <button type="button" className="et-boton principal anil" {...apagado(conexion)} onClick={() => act(api.routeEncargo(e.id, "local"))}>{t("Leave it to my agent")}</button>;
  } else if (pending && e.app && agentOn) {
    principal = <button type="button" className="et-boton principal" {...apagado(conexion)} onClick={() => act(api.doEncargos([e.id]))}>{t("Send to the agent")}</button>;
  }

  const menu: { label: string; onClick: () => void; peligro?: boolean }[] = [];
  if (open && e.status !== "delivered") menu.push({ label: t("Estimate again"), onClick: () => act(api.reestimateEncargo(e.id)) });
  if (pending) {
    menu.push(e.route === "platform"
      ? { label: t("Change to my agent"), onClick: () => act(api.routeEncargo(e.id, "local")) }
      : { label: t("Change to the platform's AI"), onClick: () => act(api.routeEncargo(e.id, "platform")) });
  }
  if (delCoder && !e.stopAsked) menu.push({ label: t("Stop the agent"), onClick: () => act(api.stopEncargo(e.id)) });
  menu.push({ label: historia ? t("Hide history") : t("History"), onClick: verHistoria });
  if (open) menu.push({ label: t("Drop it"), onClick: () => setConfirma("soltar"), peligro: true });

  const quien = e.keyName ? t("{agent} has it", { agent: e.keyName }) : t("Asked by {who}", { who: e.createdBy });
  return (
    <article className={`encargo-tarjeta${chosen ? " escogido" : ""}${e.status === "delivered" ? " entregado" : ""}`} id={`encargo-${e.id}`} ref={card}>
      <div className="et-cabeza">
        {pending && (
          <label className="et-escoger" title={motivo || t("Choose it")}>
            <input type="checkbox" checked={chosen} disabled={!choosable} onChange={(ev) => onChoose(ev.target.checked)}
              aria-label={choosable ? t("Choose encargo {n}", { n: e.id }) : t("Encargo {n} cannot be chosen: {why}", { n: e.id, why: motivo })} />
          </label>
        )}
        <div className="et-titulo">
          <div className="et-insignias">
            {e.app ? <Link className="et-insignia app" to={`/apps/${e.app}`}>{e.app}</Link> : <span className="et-insignia sin-app">{t("No app")}</span>}
            {pending && e.route && <span className={`et-insignia ruta-${e.route}`}>{t(routeLabel[e.route])}</span>}
            {!pending && <span className={`badge ${statusClass[e.status]}`}>{t(statusLabel[e.status])}</span>}
            {fila && <span className="et-insignia fila">{t("in the queue")}</span>}
          </div>
          <strong className="et-nombre"><span className="muted">#{e.id}</span> {e.title}</strong>
          {pending && motivo && e.app && (
            <span className="et-porque">{t("Cannot be chosen: {why}.", { why: motivo })}
              {cola?.mode === "off" && <> <Link to="/ia">{t("Turn the agent on")}</Link></>}</span>
          )}
        </div>
      </div>
      {pending && !e.app && <EscogerApp e={e} onChange={onChange} />}
      <div className="et-costo">
        {e.difficulty ? <Dificultad d={e.difficulty} /> : <span className="dif-cuadro vacio" aria-hidden="true">?</span>}
        <span className="et-costo-texto">
          <strong>{e.difficulty ? t("{how} · about {cost}", { how: difficultyWord(e.difficulty), cost: dollars(e.estimateMicros ?? 0) })
            : e.estimateReason ? t("No estimate") : t("Estimating…")}</strong>
          <span className="small muted">{quien} · {timeAgo(e.createdAt)}
            {e.route === "local" && !e.routeChosen && pending && e.difficulty ? <> · {t("over your amount: better with your agent")}</> : null}
            {e.waitingRun && e.app && <> · <Link to={`/apps/${e.app}/runs/${e.waitingRun}`}>{t("waiting on run #{n}", { n: e.waitingRun })}</Link></>}
            {e.runId && e.app && <> · <Link to={`/apps/${e.app}/runs/${e.runId}`}>{t("about run #{n}", { n: e.runId })}</Link></>}
          </span>
        </span>
      </div>
      {e.estimateReason && (
        <details className="et-porque-cuesta">
          <summary>{e.difficulty ? t("Why it costs that") : t("Why there is no estimate")}</summary>
          <p>{e.estimateReason}</p>
        </details>
      )}
      {!!e.spentMicros && <EstimateBar estimate={e.estimateMicros ?? 0} spent={e.spentMicros} />}
      {e.result && <div className="encargo-result"><TextoConFormato text={e.result} />
        {e.links.map((l) => <div key={l}><a className="small" href={l} target="_blank" rel="noreferrer">{l}</a></div>)}</div>}
      {e.comment && pending && <p className="small"><strong>{t("Sent back:")}</strong> {e.comment}</p>}
      <ErrorBox error={err} />
      {confirma === "soltar" ? (
        <Confirmar pregunta={t("Drop encargo #{n}?", { n: e.id })} si={t("Drop it")} motivo={t("Why (optional): whoever reads it later knows")}
          onNo={() => setConfirma(undefined)} onSi={(m) => { setConfirma(undefined); act(api.cancelEncargo(e.id, m)); }} />
      ) : confirma === "devolver" ? (
        <DevolverForm onNo={() => setConfirma(undefined)} onSi={(c) => { setConfirma(undefined); act(api.reviewEncargo(e.id, false, c)); }} />
      ) : (
        <div className="et-acciones">
          {principal}
          {e.status === "delivered" && <button type="button" className="et-boton" onClick={() => setConfirma("devolver")}>{t("Send back")}</button>}
          {(open || comentarios.length > 0) && <button type="button" className="et-boton" aria-expanded={comentando} onClick={() => setComentando(!comentando)}>{t("Comment")}</button>}
          <Menu label={t("More for encargo {n}", { n: e.id })} items={menu} />
        </div>
      )}
      {(comentarios.length > 0 || comentando) && <Comentarios list={comentarios} encargoId={e.id} onChange={onChange} abierto={comentando} poner={setComentando} />}
      {historia && (
        <div className="small encargo-log">
          {full?.body && <TextoConFormato text={full.body} />}
          {full?.log?.map((l, i) => (
            <div key={i}><span className="muted">{new Date(l.at).toLocaleString()}</span> · {l.who} · {t(eventLabel[l.what] ?? l.what)}{l.note && ` · ${l.what === "routed" ? t(routeLabel[l.note as "platform" | "local"] ?? l.note) : l.note}`}</div>
          ))}
        </div>
      )}
    </article>
  );
}

/** Sending a delivered encargo back: what is missing, on the page (the next agent reads it). */
export function DevolverForm({ onSi, onNo }: { onSi: (c: string) => void; onNo: () => void }) {
  const [c, setC] = useState("");
  return (
    <div className="et-confirma">
      <label className="field"><span>{t("What is missing? The next agent reads it.")}</span>
        <textarea rows={3} value={c} autoFocus onChange={(e) => setC(e.target.value)} />
      </label>
      <div className="et-acciones">
        <button type="button" className="et-boton principal" disabled={!c.trim()} onClick={() => onSi(c.trim())}>{t("Send back")}</button>
        <button type="button" className="et-boton" onClick={onNo}>{t("Cancel")}</button>
      </div>
    </div>
  );
}

/**
 * The form to ask agents for something: what, the app (asked here: without it the coding agent cannot
 * take it, though it can be chosen later on the card) and the detail.
 */
function NuevoEncargo({ onCreated, onClose }: { onCreated: () => void; onClose: () => void }) {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [app, setApp] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const apps = useApps();
  const conexion = useConexion();
  const later = app === "-";
  return (
    <form className="card stack nuevo-encargo" onSubmit={(ev) => {
      ev.preventDefault();
      setBusy(true); setErr(undefined);
      api.createEncargo({ title: title.trim(), body, app: later ? undefined : app }).then(() => {
        setBusy(false); setTitle(""); setBody(""); setApp(""); onCreated();
      }, (e) => { setBusy(false); setErr(e); });
    }}>
      <label className="field"><span>{t("What to do")}</span>
        <input value={title} maxLength={200} required onChange={(e) => setTitle(e.target.value)} placeholder={t("e.g. Find out why analizar-accion fails")} />
      </label>
      <label className="field"><span>{t("Which app is it about?")}</span>
        <select className="et-select" value={app} required onChange={(e) => setApp(e.target.value)}>
          <option value="">{t("Choose one…")}</option>
          {apps.map((a) => <option key={a.name} value={a.name}>{a.name}</option>)}
          <option value="-">{t("I do not know yet; I choose it later")}</option>
        </select>
        {later && <span className="small muted">{t("Without its app, the coding agent cannot take it; choose it later on its card.")}</span>}
      </label>
      <label className="field"><span>{t("Detail")}</span>
        <textarea rows={3} value={body} onChange={(e) => setBody(e.target.value)} placeholder={t("What you know, where to look, and how to tell it is done.")} />
      </label>
      <ErrorBox error={err} />
      <div className="et-acciones">
        <button type="submit" className="et-boton principal" {...apagado(conexion, busy || !title.trim() || !app)}>{t("Ask agents")}</button>
        <button type="button" className="et-boton" onClick={onClose}>{t("Cancel")}</button>
      </div>
    </form>
  );
}

/**
 * What the encargos would cost with the platform's AI, folded: the open ones (all, and only the easy
 * ones), what the done ones were worth and what the coding agent spent, with the open ones by
 * difficulty from green to red.
 */
function Cifras({ list }: { list: Encargo[] }) {
  const estimated = list.filter((e) => e.difficulty && e.status !== "cancelled");
  if (!estimated.length) return null;
  const open = estimated.filter((e) => e.status !== "delivered" && e.status !== "reviewed");
  const easy = open.filter((e) => e.route === "platform");
  const done = estimated.filter((e) => e.status === "delivered" || e.status === "reviewed");
  const waiting = list.filter((e) => !e.difficulty && e.status !== "cancelled").length;
  const byDifficulty = Array.from({ length: 10 }, (_, i) => open.filter((e) => e.difficulty === i + 1));
  const spent = list.reduce((n, e) => n + (e.spentMicros ?? 0), 0);
  return (
    <details className="card encargo-costos">
      <summary><strong>{t("What the encargos would cost with the platform's AI")}</strong></summary>
      <div className="encargo-cifras">
        <div><span className="cifra">{suma(open)}</span><span className="small muted">{tn(open.length, "to do the open one", "to do the {n} open ones")}</span></div>
        <div><span className="cifra">{suma(easy)}</span><span className="small muted">{tn(easy.length, "to do only the easy one (under your amount)", "to do only the {n} easy ones (under your amount)")}</span></div>
        <div><span className="cifra">{suma(done)}</span><span className="small muted">{tn(done.length, "what the done one was worth", "what the {n} done ones were worth")}</span></div>
        <div><span className="cifra">{dollars(spent)}</span><span className="small muted">{t("what the platform's coding agent spent on them; the rest was done by your own agent, paid by the month")}</span></div>
      </div>
      {open.length > 0 && (
        <div className="stack" style={{ gap: 4 }}>
          <div className="dificultad-barra" role="img" aria-label={t("Open encargos by difficulty")}>
            {byDifficulty.map((l, i) => l.length > 0 && (
              <span key={i} style={{ flexGrow: l.length, background: difficultyColor(i + 1) }} title={t("{n} of difficulty {d}", { n: l.length, d: i + 1 })}>{i + 1}</span>
            ))}
          </div>
          <div className="row between small muted"><span>{t("cheap")}</span><span>{t("expensive")}</span></div>
        </div>
      )}
      {waiting > 0 && <span className="small muted">{tn(waiting, "{n} still to estimate", "{n} still to estimate")}</span>}
    </details>
  );
}

const grupos: { id: string; label: string; de: (e: Encargo) => boolean }[] = [
  { id: "revisar", label: "To review", de: (e) => e.status === "delivered" },
  { id: "hacer", label: "Still to do", de: (e) => e.status === "pending" },
  { id: "agentes", label: "With agents", de: (e) => e.status === "taken" || e.status === "waiting" },
  { id: "cerrados", label: "Reviewed and dropped", de: (e) => e.status === "reviewed" || e.status === "cancelled" },
];

/**
 * The Encargos view: work asked of agents. A person (or an agent) asks; an agent takes it, may wait on
 * a run, and hands it over; the person reviews it or sends it back with a comment.
 */
export function Encargos({ cola, onCola }: { cola?: Cola; onCola: () => void }) {
  const [all, setAll] = useState(false);
  const { data, error, reload: reloadList } = usePoll(() => api.encargos(all), [all], 15000);
  const { data: comentarios, reload: reloadComentarios } = usePoll(api.comentarios, [], 15000);
  const hilos = porTrabajo(comentarios, "encargoId");
  const reload = () => { reloadList(); reloadComentarios(); onCola(); };
  const [chosen, setChosen] = useState<number[]>([]);
  const [sent, setSent] = useState<string>();
  const [pidiendo, setPidiendo] = useState(false);
  // A link to a closed encargo (#encargo-N) shows the closed ones too.
  useEffect(() => {
    const m = /^#encargo-(\d+)$/.exec(location.hash);
    if (m && data && !all && !data.some((e) => e.id === Number(m[1]))) setAll(true);
  }, [data, all]);
  const list = data ?? [];
  const chosenList = list.filter((e) => chosen.includes(e.id));
  const choosable = list.filter((e) => e.status === "pending" && !porQueNo(e, cola));
  const easy = choosable.filter((e) => e.route === "platform");
  const hard = choosable.filter((e) => e.route !== "platform");
  const escoger = (l: Encargo[]) => setChosen([...new Set([...chosen, ...l.map((e) => e.id)])]);
  return (
    <div className="vista-encargos">
      <div className="vista-cabeza">
        <h1>{t("Encargos")}</h1>
        <button type="button" className="et-boton principal naranja" aria-expanded={pidiendo} onClick={() => setPidiendo(!pidiendo)}>{pidiendo ? t("Close") : t("+ Ask for something")}</button>
      </div>
      <p className="sub">{t("Work for agents. An agent connected over MCP sees the pending ones when it starts, takes one (nobody else can then), waits on its runs without asking over and over, and hands it over with what it did. You review it, or send it back with what is missing.")}</p>
      {pidiendo && <NuevoEncargo onCreated={() => { setPidiendo(false); reload(); }} onClose={() => setPidiendo(false)} />}
      {sent && <p className="small" role="status">{sent}</p>}
      {(easy.length > 0 || hard.length > 0) && (
        <div className="et-lotes" role="group" aria-label={t("Choose several")}>
          <span className="small muted">{t("Choose:")}</span>
          {easy.length > 0 && <button type="button" className="et-boton" onClick={() => escoger(easy)}>{t("the easy ones ({n} · {cost})", { n: easy.length, cost: suma(easy) })}</button>}
          {hard.length > 0 && <button type="button" className="et-boton" onClick={() => escoger(hard)}>{t("the hard ones ({n} · {cost})", { n: hard.length, cost: suma(hard) })}</button>}
        </div>
      )}
      {cola?.mode === "off" && <p className="small muted">{t("To have the platform's AI do them, an administrator turns on the coding agent on the AI page.")} <Link to="/ia">{t("The AI page")}</Link></p>}
      <ErrorBox error={error} />
      {!data && !error && <EsqTarjetas />}
      {data && !list.length && <p className="muted">{t("No encargos. Ask agents for something above, or from a failed run.")}</p>}
      {grupos.map((g) => {
        const items = list.filter(g.de);
        if (!items.length) return null;
        return (
          <section key={g.id} className="et-grupo" aria-label={t(g.label)}>
            <h2>{t(g.label)} <span className="muted">{items.length}</span></h2>
            {items.map((e) => (
              <EncargoCard key={e.id} e={e} cola={cola} onChange={reload} chosen={chosen.includes(e.id)} comentarios={hilos.get(e.id) ?? []}
                onChoose={(on) => setChosen(on ? [...chosen, e.id] : chosen.filter((id) => id !== e.id))} />
            ))}
          </section>
        );
      })}
      <label className="et-todos"><input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} /> {t("Show reviewed and dropped")}</label>
      <Cifras list={list} />
      <BarraMandar chosen={chosenList} onClear={() => setChosen([])} onSent={(n) => {
        setChosen([]); setSent(tn(n, "Sent {n} encargo to the coding agent.", "Sent {n} encargos to the coding agent.")); reload();
      }} />
    </div>
  );
}
