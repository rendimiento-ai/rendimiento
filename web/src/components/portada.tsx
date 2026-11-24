// The Ruta's front page, «Para usted» (la Ruta nueva, C1 on a phone, C3 on a computer): how many things
// wait for the person's word, the coding agent live, first what the coding agent asks (answered there, or
// passed to the person's own agent), then one row for each of those things with ONE
// action (review a delivered encargo, decide, rate a known failure, do what only they can, read a
// comment nobody answered), the encargos nobody has yet (chosen right there, their app too), and
// what goes on meanwhile.
import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { api, dollars, session, timeAgo, type Cola, type Encargo, type Portada } from "../api";
import { ErrorBox, usePoll } from "./ui";
import { t, tn } from "../i18n";
import { AgenteTira, doingText } from "./cola";
import { BarraMandar, Dificultad, EscogerApp, porQueNo, routeLabel } from "./encargos";
import { EsqPortada } from "./esqueleto";

/** The greeting for the hour of the day. */
function saludo() {
  const h = new Date().getHours();
  return h < 12 ? t("good morning") : h < 19 ? t("good afternoon") : t("good evening");
}

/** The first line of a text, short. */
const primera = (s: string, n = 90) => {
  const l = (s || "").split("\n").map((x) => x.replace(/[#*_`>]/g, "").trim()).find(Boolean) ?? "";
  return l.length > n ? `${l.slice(0, n - 1)}…` : l;
};

/** One thing that needs the person: its sign, what, a line more, and its one action. */
function Fila({ tono, signo, que, mas, accion }: { tono: string; signo: string; que: ReactNode; mas?: ReactNode; accion: ReactNode }) {
  return (
    <article className={`pu-fila tono-${tono}`}>
      <span className="pu-signo" aria-hidden="true">{signo}</span>
      <span className="pu-texto"><strong>{que}</strong>{mas && <span>{mas}</span>}</span>
      {accion}
    </article>
  );
}

/** The encargos nobody has: choose which go to the coding agent, or their app when it is missing. */
function SinQuien({ list, cola, chosen, setChosen, onChange }: { list: Encargo[]; cola?: Cola; chosen: number[]; setChosen: (l: number[]) => void; onChange: () => void }) {
  const [app, setApp] = useState<number>();
  return (
    <section className="pu-grupo tono-naranja" aria-labelledby="pu-sin-quien">
      <div className="pu-grupo-cabeza">
        <span className="pu-signo" aria-hidden="true">$</span>
        <span className="pu-texto">
          <strong id="pu-sin-quien">{tn(list.length, "{n} encargo waits for someone to do it", "{n} encargos wait for someone to do them")}</strong>
          <span>{cola && cola.mode !== "off" ? t("Choose the ones you send to the agent") : t("Leave them to your agent, or turn on the coding agent")}</span>
        </span>
      </div>
      <ul className="pu-lista">
        {list.slice(0, 8).map((e) => {
          const motivo = porQueNo(e, cola);
          return (
            <li key={e.id} className={e.app ? "" : "falta"}>
              {e.app ? (
                <label className="et-escoger" title={motivo || t("Choose it")}>
                  <input type="checkbox" checked={chosen.includes(e.id)} disabled={!!motivo}
                    aria-label={motivo ? t("Encargo {n} cannot be chosen: {why}", { n: e.id, why: motivo }) : t("Choose encargo {n}", { n: e.id })}
                    onChange={(ev) => setChosen(ev.target.checked ? [...chosen, e.id] : chosen.filter((x) => x !== e.id))} />
                </label>
              ) : <span className="pu-falta" aria-hidden="true">!</span>}
              <span className="pu-texto">
                <strong><span className="muted">#{e.id}</span> {e.title}</strong>
                {e.app ? (
                  <span className="pu-meta">{e.app}{e.difficulty ? <> · <Dificultad d={e.difficulty} small /> · ~{dollars(e.estimateMicros ?? 0)}</> : null}
                    {e.route && <> · {t(routeLabel[e.route])}</>}</span>
                ) : <span className="pu-meta falta">{t("It does not say which app it is about")}</span>}
              </span>
              {e.app ? (
                <Link className="et-boton redondo" to={`/ruta?tab=encargos#encargo-${e.id}`} aria-label={t("More for encargo {n}", { n: e.id })} title={t("Open it")}>⋯</Link>
              ) : app === e.id ? <EscogerApp e={e} compacto onChange={() => { setApp(undefined); onChange(); }} />
                : <button type="button" className="et-boton" onClick={() => setApp(e.id)}>{t("Choose app")}</button>}
            </li>
          );
        })}
      </ul>
      <Link className="pu-ver-todos" to="/ruta?tab=encargos">{list.length > 8 ? tn(list.length, "See the {n} encargos", "See the {n} encargos") : t("See every encargo")}</Link>
    </section>
  );
}

/** A letter for an option: A, B, C, D. */
const letra = (i: number) => String.fromCharCode(65 + i);

/**
 * What the coding agent asks (Ruta 178): the question, its options and a line more, answered right here
 * («Answer and let it go on»: it resumes on its branch with the answer) or passed to the person's own agent.
 */
function Pregunta({ e, onDone }: { e: Encargo; onDone: (msg: string) => void }) {
  const q = e.question!;
  const [opcion, setOpcion] = useState<number>();
  const [mas, setMas] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const puede = session.role !== "guest";
  const respuesta = [opcion !== undefined ? `${letra(opcion)}. ${q.options[opcion]}` : "", mas.trim()].filter(Boolean).join("\n");
  const hacer = async (f: () => Promise<unknown>, msg: string) => {
    setBusy(true);
    setError(undefined);
    try {
      await f();
      onDone(msg);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="pu-grupo tono-anil pu-pregunta" aria-labelledby={`pu-pregunta-${e.id}`}>
      <div className="pu-grupo-cabeza">
        <span className="pu-signo" aria-hidden="true">?</span>
        <span className="pu-texto">
          <strong id={`pu-pregunta-${e.id}`}>{t("The agent asks you · encargo #{n}", { n: e.id })}</strong>
          <span>{e.title}{e.app ? ` · ${e.app}` : ""} · {timeAgo(q.askedAt)} · {t("it stopped to wait, spending nothing")}</span>
        </span>
      </div>
      <p className="pu-pregunta-texto">{q.question}</p>
      {q.options.length > 0 && (
        <div className="pu-opciones" role="radiogroup" aria-label={t("The options")}>
          {q.options.map((o, i) => (
            <label key={i} className={`pu-opcion${opcion === i ? " elegida" : ""}`}>
              <input type="radio" name={`pregunta-${e.id}`} checked={opcion === i} disabled={!puede} onChange={() => setOpcion(i)} />
              <span><strong>{letra(i)}.</strong> {o}</span>
            </label>
          ))}
        </div>
      )}
      {puede && (
        <label className="pu-mas">
          <span className="small muted">{q.options.length ? t("Something more (optional)") : t("Your answer")}</span>
          <textarea rows={2} value={mas} maxLength={4000} onChange={(ev) => setMas(ev.target.value)} />
        </label>
      )}
      <ErrorBox error={error} />
      {puede && (
        <div className="pu-acciones">
          <button type="button" className="et-boton principal anil" disabled={busy || !respuesta}
            onClick={() => hacer(() => api.answerEncargo(e.id, respuesta), t("Answered: the coding agent goes on with encargo #{n}.", { n: e.id }))}>
            {t("Answer and let it go on")}
          </button>
          <button type="button" className="et-boton" disabled={busy}
            onClick={() => hacer(() => api.passEncargo(e.id), t("Encargo #{n} went to your agent, with the question.", { n: e.id }))}>
            {t("Pass it to my agent")}
          </button>
        </div>
      )}
      <p className="small muted">{t("It spends nothing while it waits. If nobody answers in 3 days, it goes to your agent with the question. Answered, it goes on on the same branch, with what it had.")}</p>
    </section>
  );
}

/** A count of what goes on meanwhile, as a link to where it is. */
function Cuenta({ n, que, a, tono }: { n: number; que: string; a: string; tono: string }) {
  return <Link className={`pu-cuenta tono-${tono}`} to={a}><strong>{n}</strong><span>{que}</span></Link>;
}

/** The coding agent on a computer's right column: what it does now and what follows. */
function AgenteLado({ cola }: { cola?: Cola }) {
  if (!cola) return null;
  return (
    <section className="pu-lado-caja agente-lado" aria-labelledby="pu-agente">
      <h2 id="pu-agente">{t("The coding agent")}</h2>
      {cola.now.length ? cola.now.map((e) => (
        <div key={e.id} className="pu-agente-ahora">
          <span className="pu-etiqueta">{t("Doing now")}</span>
          <Link to="/ruta?tab=agente"><strong><span className="muted">#{e.id}</span> {e.title}</strong></Link>
          <span className="small muted">{doingText(e)} · {t("{spent} of about {estimate}", { spent: dollars(e.spentMicros ?? 0), estimate: dollars(e.estimateMicros ?? 0) })}</span>
        </div>
      )) : <p className="small muted">{cola.mode === "off" ? t("The coding agent is off") : t("Nothing right now.")}</p>}
      {cola.next.length > 0 && (
        <>
          <span className="pu-etiqueta">{t("Next in line")}</span>
          <ol className="pu-agente-siguen">{cola.next.slice(0, 4).map((e) => <li key={e.id}><span className="muted">#{e.id}</span> {e.title}</li>)}</ol>
        </>
      )}
      <Link className="et-boton" to="/ruta?tab=agente">{t("See the queue")}</Link>
    </section>
  );
}

/** «Para usted»: the Ruta's front page. */
export function ParaUsted({ me, cola, onCola }: { me: string; cola?: Cola; onCola: () => void }) {
  const { data, error, reload } = usePoll(api.portada, [], 20000);
  const { data: llaves } = usePoll(() => (session.role === "admin" ? api.agentKeys().catch(() => []) : Promise.resolve([])), [], 0);
  const [chosen, setChosen] = useState<number[]>([]);
  const [sent, setSent] = useState<string>();
  const change = () => { reload(); onCola(); };
  const p: Portada | undefined = data;
  const cuantas = p ? (p.asking?.length ?? 0) + p.review.length + p.decide.length + p.yours.length + p.rate.length + p.unanswered.length + p.unowned.length : 0;
  const activas = (llaves ?? []).filter((k) => !k.revokedAt && new Date(k.expiresAt).getTime() > Date.now()).length;
  const encargoDe = (id?: number) => `/ruta?tab=encargos#encargo-${id}`;
  return (
    <div className="portada">
      <section className="pu-cabeza">
        <svg className="pu-mariposa" width="54" height="46" viewBox="0 0 40 34" aria-hidden="true">
          <g className="pu-alas"><path d="M20 17 C10 2 0 6 4 16 C6 22 14 22 20 17Z" /><path d="M20 17 C30 2 40 6 36 16 C34 22 26 22 20 17Z" />
            <path className="pu-alas-bajas" d="M20 17 C14 26 8 30 10 24Z M20 17 C26 26 32 30 30 24Z" /></g>
          <path d="M20 10v16" className="pu-torso" />
        </svg>
        <span className="pu-saludo">{me ? t("La Ruta · {greeting}, {name}", { greeting: saludo(), name: me }) : t("La Ruta")}</span>
        <h1>{!p ? t("La Ruta") : cuantas ? tn(cuantas, "{n} thing waits for your word", "{n} things wait for your word") : t("Nothing waits for your word")}</h1>
        {p && <p className="pu-sub">{t("The rest goes on by itself.")} {p.counts.doneToday > 0 && tn(p.counts.doneToday, "Today {n} thing got done.", "Today {n} things got done.")}</p>}
        <AgenteTira cola={cola} className="en-cabeza" />
      </section>
      <ErrorBox error={error} />
      {!p && !error && <EsqPortada />}
      {p && (
        <div className="pu-cuerpo">
          <div className="pu-principal">
            <h2>{t("For you")}</h2>
            {sent && <p className="small" role="status">{sent}</p>}
            {(p.asking ?? []).map((e) => <Pregunta key={`q${e.id}`} e={e} onDone={(msg) => { setSent(msg); change(); }} />)}
            {cuantas === 0 && <p className="pu-nada">{t("Nothing needs you now. When an agent delivers something, or something waits for your word, it shows here.")}</p>}
            {p.review.map((e) => (
              <Fila key={`r${e.id}`} tono="verde" signo="✓" que={t("Review: encargo #{n} delivered", { n: e.id })}
                mas={<>{e.title}{e.spentMicros ? ` · ${dollars(e.spentMicros)}` : ""}</>}
                accion={<Link className="et-boton principal verde" to={encargoDe(e.id)}>{t("Review")}</Link>} />
            ))}
            {p.unowned.length > 0 && <SinQuien list={p.unowned} cola={cola} chosen={chosen} setChosen={setChosen} onChange={change} />}
            {p.decide.map((e) => (
              <Fila key={`d${e.id}`} tono="morado" signo="?" que={t("Decide: {title}", { title: e.title })} mas={primera(e.body) || timeAgo(e.updatedAt)}
                accion={<Link className="et-boton principal morado" to={`/ruta?tab=ruta&entrada=${e.id}`}>{t("Decide")}</Link>} />
            ))}
            {p.rate.map((m) => (
              <Fila key={`c${m.id}`} tono="rosa" signo="!" que={t("Rate: a failure of {app}", { app: m.app })} mas={t("The diagnostician is not sure · run #{n}", { n: m.runId })}
                accion={<Link className="et-boton principal rosa" to={`/apps/${m.app}/runs/${m.runId}`}>{t("Rate")}</Link>} />
            ))}
            {p.yours.map((e) => (
              <Fila key={`y${e.id}`} tono="anil" signo="✎" que={t("Do it yourself: {title}", { title: e.title })} mas={primera(e.body) || t("Only you can do it")}
                accion={<Link className="et-boton principal anil" to={`/ruta?tab=ruta&entrada=${e.id}`}>{t("Steps")}</Link>} />
            ))}
            {p.unanswered.map((c) => (
              <Fila key={`u${c.id}`} tono="teja" signo="“" que={t("Nobody answered: {about}", { about: c.about || t("a comment") })} mas={`${c.author}: ${primera(c.body, 70)}`}
                accion={<Link className="et-boton principal teja" to={c.encargoId ? encargoDe(c.encargoId) : "/ruta?tab=cargos"}>{t("Read")}</Link>} />
            ))}
          </div>
          <aside className="pu-lado">
            <AgenteLado cola={cola} />
            <section className="pu-lado-caja" aria-labelledby="pu-mientras">
              <h2 id="pu-mientras">{t("Meanwhile")}</h2>
              <div className="pu-cuentas">
                <Cuenta tono="anil" n={p.counts.withAgents} que={t("in agents' hands")} a="/ruta?tab=agente" />
                <Cuenta tono="grana" n={p.counts.pendientes} que={t("to-dos for later")} a="/ruta?tab=pendientes" />
                <Cuenta tono="verde" n={p.counts.doneToday} que={t("done today")} a="/ruta?tab=agente" />
                <Cuenta tono="rosa" n={p.counts.vividoNew} que={tn(p.counts.vividoNew, "new story in lo vivido", "new stories in lo vivido")} a="/ruta?tab=vivido" />
              </div>
            </section>
            <section className="pu-lado-caja solo-compu" aria-labelledby="pu-atajos">
              <h2 id="pu-atajos">{t("The journal")}</h2>
              <nav className="pu-atajos" aria-labelledby="pu-atajos">
                <Link to="/ruta?tab=decisiones">{t("Decisions and manuals")}</Link>
                <Link to="/ruta?tab=cargos">{t("Cargos")}</Link>
                {session.role === "admin" && <Link to="/ruta?tab=llaves">{t("Agent keys")} <span className="muted">{tn(activas, "{n} active key", "{n} active keys")}</span></Link>}
              </nav>
              {p.latest.length > 0 && (
                <>
                  <span className="pu-etiqueta">{t("The latest written")}</span>
                  <ul className="pu-ultimo">{p.latest.slice(0, 4).map((e) => <li key={e.id}><Link to={`/ruta?tab=ruta&entrada=${e.id}`}>{e.title}</Link> <span className="small muted">{timeAgo(e.updatedAt)}</span></li>)}</ul>
                </>
              )}
            </section>
          </aside>
        </div>
      )}
      <BarraMandar chosen={(p?.unowned ?? []).filter((e) => chosen.includes(e.id))} onClear={() => setChosen([])} onSent={(n) => {
        setChosen([]); setSent(tn(n, "Sent {n} encargo to the coding agent.", "Sent {n} encargos to the coding agent.")); change();
      }} />
    </div>
  );
}
