// The coding agent's queue (la Ruta nueva, A2): what it does now (what it reads, what it spent against
// the estimate, to see or stop it), what it does next in order (reordered by dragging on a computer or
// with the arrows on a phone, with when each was asked and the old ones marked), what it finished for
// the person to review, and what it gave back to them. Also the live strip of it that the front page
// and the Encargos view show.
import { useEffect, useState, type DragEvent } from "react";
import { Link } from "react-router-dom";
import { api, dollars, timeAgo, type Cola, type Encargo } from "../api";
import { ErrorBox, usePoll } from "./ui";
import { locale, t, tn } from "../i18n";
import { EstimateBar } from "./boleta";
import { Confirmar, DevolverForm, Dificultad, eventLabel, Reenviar } from "./encargos";
import { EsqCola } from "./esqueleto";
import { TextoConFormato } from "./texto";
import { useConexion } from "../conexion";
import { apagado } from "./conexion-aviso";

/** Past this many days in the queue, an encargo is marked old and can be set aside. */
export const VIEJO_DIAS = 14;

/** The coding agent's queue, refreshed every 10 seconds. */
export function useCola() {
  return usePoll(api.cola, [], 10000);
}

/** What the coding agent does now on an encargo ("tool:what", internal/agentes/codigo.go), in words. */
export function doingText(e: Encargo): string {
  if (e.status === "waiting" && e.waitingRun) return t("Waiting on run #{n}", { n: e.waitingRun });
  const i = (e.doing ?? "").indexOf(":");
  const tool = i < 0 ? e.doing ?? "" : e.doing!.slice(0, i);
  const what = i < 0 ? "" : e.doing!.slice(i + 1);
  switch (tool) {
    case "think": return t("Thinking about the next step");
    case "list_files": return what && what !== "." ? t("Looking at the files in {x}", { x: what }) : t("Looking at the repository's files");
    case "read_file": return t("Reading {x}", { x: what });
    case "search": return t("Searching for «{x}»", { x: what });
    case "write_file": case "edit_file": return t("Changing {x}", { x: what });
    case "delete_file": return t("Deleting {x}", { x: what });
    case "merge_default_branch": return t("Merging the default branch into its own");
    case "commit_and_test": return t("Testing its change with the platform's CI");
    case "run_fix": return t("Running the fix {x}", { x: what });
    case "run": return t("Running «{x}» in its sandbox", { x: what });
    case "runs": return t("Looking at the app's runs");
    case "step_log": return t("Reading a step's log");
    case "slow_requests": return t("Looking at the app's slow requests");
    case "app_status": return t("Looking at the app's status");
    case "ai_retry": return t("The AI's connection dropped: trying again");
    case "ask": return t("Asking you something");
    case "deliver": return t("Handing it over");
    case "give_up": return t("Giving it back");
  }
  return t("Getting its branch ready");
}

/** How far an encargo's spending is: what it spent over what was estimated (the brake is at twice that). */
const avance = (e: Encargo) => (e.estimateMicros ? Math.min(100, Math.round(((e.spentMicros ?? 0) / e.estimateMicros) * 100)) : 0);

/** The coding agent at a glance, as a link to its queue: what it does, how many wait, and how far it is. */
export function AgenteTira({ cola, className = "" }: { cola?: Cola; className?: string }) {
  if (!cola) return <span className={`agente-tira esperando ${className}`} aria-hidden="true" />;
  const now = cola.now[0];
  const fila = cola.next.length;
  return (
    <Link to="/ruta?tab=agente" className={`agente-tira ${className}`}>
      <span className="agente-tira-linea">
        {now ? <i className="girando" aria-hidden="true" /> : <i className="quieto" aria-hidden="true" />}
        <span className="agente-tira-texto">
          {cola.mode === "off" ? <strong>{t("The coding agent is off")}</strong>
            : now ? <><strong>{t("The agent does #{n}", { n: now.id })}</strong>{cola.now.length > 1 && <> {tn(cola.now.length - 1, "and {n} more", "and {n} more")}</>}</>
            : <strong>{t("The agent is resting")}</strong>}
          {cola.mode !== "off" && <> · {tn(fila, "{n} in line", "{n} in line")}</>}
          {now && <span className="agente-tira-que"> · {doingText(now)}</span>}
        </span>
        <span aria-hidden="true" className="agente-tira-flecha">›</span>
      </span>
      {now && (
        <span className="agente-avance" role="progressbar" aria-label={t("Spent of the estimate")} aria-valuemin={0} aria-valuemax={100} aria-valuenow={avance(now)}>
          <span style={{ width: `${Math.max(4, avance(now))}%` }} />
        </span>
      )}
    </Link>
  );
}

/** When an encargo was asked, as a date and an age. */
function Pedido({ e }: { e: Encargo }) {
  const dias = (Date.now() - new Date(e.createdAt).getTime()) / 86400000;
  return (
    <span className="small muted">
      {t("Asked on {date}", { date: new Date(e.createdAt).toLocaleDateString(locale, { day: "numeric", month: "short" }) })} · {timeAgo(e.createdAt)}
      {dias > VIEJO_DIAS && <> <span className="et-insignia viejo">{t("old")}</span></>}
    </span>
  );
}

/** One encargo the agent does now: what it does, what it spent, its history and the stop. */
function Ahora({ e, onChange }: { e: Encargo; onChange: () => void }) {
  const [ver, setVer] = useState(false);
  const [detener, setDetener] = useState(false);
  const [full, setFull] = useState<Encargo>();
  const [err, setErr] = useState<unknown>();
  // Its history: when it took it, and what happened since.
  useEffect(() => { api.encargo(e.id).then(setFull, () => undefined); }, [e.id, e.status, ver]);
  const tomado = full?.log?.filter((l) => l.what === "taken").pop();
  return (
    <article className="cola-ahora">
      <div className="et-insignias">
        {e.app && <Link className="et-insignia app" to={`/apps/${e.app}`}>{e.app}</Link>}
        {e.stopAsked && <span className="badge warn live">{t("Stopping…")}</span>}
      </div>
      <strong className="et-nombre"><Link to={`/ruta?tab=encargos#encargo-${e.id}`}><span className="muted">#{e.id}</span> {e.title}</Link></strong>
      <span className="cola-que"><i className="girando" aria-hidden="true" /> {doingText(e)}</span>
      <EstimateBar estimate={e.estimateMicros ?? 0} spent={e.spentMicros ?? 0} />
      {(tomado || e.waitingRun) && (
        <span className="small muted">
          {tomado && t("started {when}", { when: timeAgo(tomado.at) })}
          {e.waitingRun && e.app && <>{tomado && " · "}<Link to={`/apps/${e.app}/runs/${e.waitingRun}`}>{t("run #{n}", { n: e.waitingRun })}</Link></>}
        </span>
      )}
      <ErrorBox error={err} />
      {detener ? (
        <Confirmar pregunta={t("Stop the agent on #{n}? It gives it back to you with what it did.", { n: e.id })} si={t("Stop it")}
          onNo={() => setDetener(false)} onSi={() => { setDetener(false); api.stopEncargo(e.id).then(onChange, setErr); }} />
      ) : (
        <div className="et-acciones">
          <button type="button" className="et-boton principal anil" aria-expanded={ver} onClick={() => setVer(!ver)}>{ver ? t("Hide what it does") : t("See what it does")}</button>
          {!e.stopAsked && <button type="button" className="et-boton peligro" onClick={() => setDetener(true)}>{t("Stop")}</button>}
        </div>
      )}
      {ver && (
        <div className="small encargo-log">
          {full?.log?.slice(-12).map((l, i) => (
            <div key={i}><span className="muted">{new Date(l.at).toLocaleTimeString(locale)}</span> · {l.who} · {t(eventLabel[l.what] ?? l.what)}{l.note && ` · ${l.note}`}</div>
          ))}
        </div>
      )}
    </article>
  );
}

/** What comes next, in order: each with its place, when it was asked, moved with the arrows or by dragging. */
function Siguen({ next, onChange }: { next: Encargo[]; onChange: () => void }) {
  const [orden, setOrden] = useState(next.map((e) => e.id));
  const [arrastra, setArrastra] = useState<number>();
  const [dicho, setDicho] = useState("");
  const [descartar, setDescartar] = useState<number>();
  const [err, setErr] = useState<unknown>();
  const conexion = useConexion();
  // The server's order, when it changes (another person, a new encargo).
  const llave = next.map((e) => e.id).join(",");
  useEffect(() => setOrden(next.map((e) => e.id)), [llave]); // eslint-disable-line react-hooks/exhaustive-deps
  const porId = new Map(next.map((e) => [e.id, e]));
  const lista = orden.map((id) => porId.get(id)).filter((e): e is Encargo => !!e);
  const guardar = (ids: number[], movido: number) => {
    setOrden(ids);
    setDicho(t("#{n} is now number {pos}.", { n: movido, pos: ids.indexOf(movido) + 1 }));
    setErr(undefined);
    api.reorderCola(ids).then(onChange, (x) => { setErr(x); setOrden(next.map((e) => e.id)); });
  };
  const mover = (id: number, a: number) => {
    const ids = orden.filter((x) => x !== id);
    ids.splice(Math.max(0, Math.min(ids.length, a)), 0, id);
    guardar(ids, id);
  };
  const soltar = (ev: DragEvent, sobre: number) => {
    ev.preventDefault();
    if (arrastra === undefined || arrastra === sobre) return;
    mover(arrastra, orden.indexOf(sobre));
    setArrastra(undefined);
  };
  if (!lista.length) return <p className="muted">{t("Nothing in line. Send it encargos from the Encargos view.")}</p>;
  return (
    <>
      <ol className="cola-siguen">
        {lista.map((e, i) => (
          <li key={e.id} draggable onDragStart={(ev) => { setArrastra(e.id); ev.dataTransfer.effectAllowed = "move"; }}
            onDragOver={(ev) => ev.preventDefault()} onDrop={(ev) => soltar(ev, e.id)} onDragEnd={() => setArrastra(undefined)}
            className={arrastra === e.id ? "arrastrando" : undefined}>
            <span className="cola-lugar" aria-hidden="true">{i + 1}</span>
            <span className="cola-cosa">
              <strong><Link to={`/ruta?tab=encargos#encargo-${e.id}`}><span className="muted">#{e.id}</span> {e.title}</Link></strong>
              <span className="small muted cola-meta">
                {e.app}{e.difficulty ? <> · <Dificultad d={e.difficulty} small /> · ~{dollars(e.estimateMicros ?? 0)}</> : null}
              </span>
              <Pedido e={e} />
              {descartar === e.id && (
                <Confirmar pregunta={t("Set #{n} aside? It is dropped, with your reason.", { n: e.id })} si={t("Set it aside")} motivo={t("Why")}
                  onNo={() => setDescartar(undefined)} onSi={(m) => { setDescartar(undefined); api.cancelEncargo(e.id, m || t("Old: set aside from the queue")).then(onChange, setErr); }} />
              )}
            </span>
            <span className="cola-mover">
              <button type="button" className="et-boton redondo" {...apagado(conexion, i === 0)} aria-label={t("Move #{n} up", { n: e.id })} title={t("Move up")} onClick={() => mover(e.id, i - 1)}>↑</button>
              <button type="button" className="et-boton redondo" {...apagado(conexion, i === lista.length - 1)} aria-label={t("Move #{n} down", { n: e.id })} title={t("Move down")} onClick={() => mover(e.id, i + 1)}>↓</button>
              {(Date.now() - new Date(e.createdAt).getTime()) / 86400000 > VIEJO_DIAS && descartar !== e.id && (
                <button type="button" className="et-boton" onClick={() => setDescartar(e.id)}>{t("Set aside")}</button>
              )}
            </span>
          </li>
        ))}
      </ol>
      <p className="small muted cola-pista">{t("To change the order, drag them on a computer, or use the arrows.")}</p>
      <p className="sr-only" role="status" aria-live="polite">{dicho}</p>
      <ErrorBox error={err} />
    </>
  );
}

/** What it finished and waits for the person's review: accepting it, or sending it back. */
function Terminado({ e, onChange }: { e: Encargo; onChange: () => void }) {
  const [devolver, setDevolver] = useState(false);
  const [err, setErr] = useState<unknown>();
  const conexion = useConexion();
  return (
    <article className="cola-fin">
      <strong className="et-nombre"><Link to={`/ruta?tab=encargos#encargo-${e.id}`}><span className="muted">#{e.id}</span> {e.title}</Link></strong>
      <span className="small muted">{e.app} · {t("spent {spent} of about {estimate}", { spent: dollars(e.spentMicros ?? 0), estimate: dollars(e.estimateMicros ?? 0) })} · {timeAgo(e.updatedAt)}</span>
      {e.links.map((l) => <a key={l} className="small" href={l} target="_blank" rel="noreferrer">{l}</a>)}
      <ErrorBox error={err} />
      {devolver ? <DevolverForm onNo={() => setDevolver(false)} onSi={(c) => { setDevolver(false); api.reviewEncargo(e.id, false, c).then(onChange, setErr); }} /> : (
        <div className="et-acciones">
          <button type="button" className="et-boton principal verde" {...apagado(conexion)} onClick={() => api.reviewEncargo(e.id, true).then(onChange, setErr)}>{t("Reviewed ✓")}</button>
          <button type="button" className="et-boton" onClick={() => setDevolver(true)}>{t("Send back")}</button>
        </div>
      )}
    </article>
  );
}

/** What it gave back to the person: why (its last note), and leaving it to their own agent or sending it again, with what it should know. */
function Devuelto({ e, onChange }: { e: Encargo; onChange: () => void }) {
  const [full, setFull] = useState<Encargo>();
  const [err, setErr] = useState<unknown>();
  useEffect(() => { api.encargo(e.id).then(setFull, () => undefined); }, [e.id]);
  const porque = full?.log?.filter((l) => l.what === "released").pop()?.note;
  return (
    <article className="cola-fin devuelto">
      <strong className="et-nombre"><Link to={`/ruta?tab=encargos#encargo-${e.id}`}><span className="muted">#{e.id}</span> {e.title}</Link></strong>
      {porque ? <TextoConFormato text={porque} className="small" /> : null}
      <span className="small muted">{e.app} · {t("spent {spent}", { spent: dollars(e.spentMicros ?? 0) })} · {timeAgo(e.updatedAt)}</span>
      <ErrorBox error={err} />
      <Reenviar e={e} onChange={onChange} setErr={setErr} otraVez />
    </article>
  );
}

/** «El agente»: the coding agent's queue. */
export function Agente({ cola, error, onChange }: { cola?: Cola; error: unknown; onChange: () => void }) {
  return (
    <div className="vista-agente">
      <div className="vista-cabeza"><h1>{t("The coding agent")}</h1></div>
      {cola && (
        <p className="sub">
          {cola.concurrency > 1 ? t("It does up to {n} at once, in order, never two of the same app.", { n: cola.concurrency }) : t("It does one at a time, in order.")}{" "}
          {t("Today it has spent {spent}; the monthly cap is {cap}.", { spent: dollars(cola.spentTodayMicros), cap: dollars(cola.capMicros) })}
        </p>
      )}
      <ErrorBox error={error} />
      {!cola && !error && <EsqCola />}
      {cola?.mode === "off" && (
        <div className="alert info">{t("The coding agent is off: encargos are for your own agent.")} <Link to="/ia">{t("Turn it on on the AI page")}</Link></div>
      )}
      {cola && (
        <>
          <section className="cola-seccion" aria-labelledby="cola-ahora">
            <h2 id="cola-ahora">{t("Doing now")}</h2>
            {cola.now.length ? cola.now.map((e) => <Ahora key={e.id} e={e} onChange={onChange} />) : <p className="muted">{t("Nothing right now.")}</p>}
          </section>
          <section className="cola-seccion" aria-labelledby="cola-siguen">
            <h2 id="cola-siguen">{t("Next in line")} <span className="muted">{cola.next.length}</span></h2>
            <Siguen next={cola.next} onChange={onChange} />
          </section>
          {cola.delivered.length > 0 && (
            <section className="cola-seccion" aria-labelledby="cola-fin">
              <h2 id="cola-fin">{t("Finished, waiting for your review")}</h2>
              {cola.delivered.map((e) => <Terminado key={e.id} e={e} onChange={onChange} />)}
            </section>
          )}
          {cola.returned.length > 0 && (
            <section className="cola-seccion" aria-labelledby="cola-devuelto">
              <h2 id="cola-devuelto">{t("Given back to you")}</h2>
              {cola.returned.map((e) => <Devuelto key={e.id} e={e} onChange={onChange} />)}
            </section>
          )}
        </>
      )}
    </div>
  );
}
