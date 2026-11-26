// Poner al día (decision 191): the report on each dependency update (Renovate's, Dependabot's) on the app's Caminos
// tab — what would fail, what would change and how much, the risk and the cost — with «Put the app up to date», which
// opens the encargo for the agent the organization chose; and the «Put up to date» card of the AI page, where an
// administrator chooses who does it, when a report is made and whether a low-risk one starts by itself. In Cotija's
// one-line stroke, with a sun that rises over the road.
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, dollars, session, timeAgo, type AlDiaAjustes, type AlDiaInforme, type App } from "../api";
import { ErrorBox, Switch, usePoll } from "./ui";
import { t, tn } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "./conexion-aviso";
import { mayActOnCopies } from "./copias";

/** The drawings, in the same stroke as the agents' (permisos-dibujos.tsx). */
const trazos: Record<string, string> = {
  // The sun rising over the road: up to date.
  sol: "M3 18.5h18M6.5 18.5a5.5 5.5 0 0 1 11 0M12 6.5V4M6.3 9.8 4.6 8.1M17.7 9.8l1.7-1.7M3.5 14.5h2M18.5 14.5h2",
  // A cracked clay pot: what would fail.
  falla: "M7 8h10M8.2 8C5 9.6 4 12 4 14.5 4 18 7.6 20.5 12 20.5s8-2.5 8-6c0-2.5-1-4.9-4.2-6.5M9 8V6.3h6V8M12.5 10.5l-1.6 3 2 1.8-1 3.2",
  // An amate sheet with its plus and minus: what would change.
  cambia: "M6 3.5h9l3.5 3.5v13.5H6zM15 3.5V7h3.5M9.5 11.5h5M12 9v5M9.5 17h5",
  // The market's scale: the risk.
  riesgo: "M12 3v17M8 20.5h8M5 7h14M5 7l-2.5 6h5zM19 7l-2.5 6h5z",
  // A peso: what it would cost.
  costo: "M12 3a9 9 0 1 0 0 18a9 9 0 1 0 0-18M14.6 9.2c-.5-1-1.5-1.6-2.6-1.6-1.4 0-2.5.8-2.5 2s1.1 1.6 2.5 1.9 2.5.9 2.5 2.1-1.1 2-2.5 2c-1.1 0-2.1-.6-2.6-1.6M12 5.8v1.8M12 16.4v1.8",
  // A maize sprout: nothing to change, it grows by itself.
  sinCambios: "M12 21V10M12 14c-3 0-5-2-5-5 3 0 5 2 5 5zM12 11c0-3 2-5 5-5 0 3-2 5-5 5zM8 21h8",
  // The backstrap loom: the coding agent weaving it.
  telar: "M3 5h18M3 19h18M6 5v14M18 5v14M12 8l3 4-3 4-3-4zM9 5v3M15 5v3M9 16v3M15 16v3",
  // A key: the person's own agent.
  llave: "M11 9.5a4 4 0 1 0 8 0a4 4 0 1 0-8 0M12.2 12.3 4 20.5M6 18.5l2 2M8.6 15.9l2 2",
};

export function DibujoAlDia({ id, className = "" }: { id: keyof typeof trazos; className?: string }) {
  return (
    <svg className={`dibujo-al-dia ${className}`} viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
      <path d={trazos[id]} />
    </svg>
  );
}

/** A cempasúchil that turns while the report is written. */
function Flor({ c, d }: { c: string; d: string }) {
  return (
    <svg className="al-dia-flor" style={{ animationDelay: d }} width="16" height="16" viewBox="0 0 14 14" aria-hidden="true">
      <path d="M7 1c2 2 2 4 0 6-2-2-2-4 0-6zM13 7c-2 2-4 2-6 0 2-2 4-2 6 0zM7 13c-2-2-2-4 0-6 2 2 2 4 0 6zM1 7c2-2 4-2 6 0-2 2-4 2-6 0z" fill={c} />
    </svg>
  );
}

const riesgoName: Record<string, string> = { bajo: "low", medio: "medium", alto: "high" };
const quienName: Record<AlDiaAjustes["quien"], string> = { platform: "The platform's coding agent", local: "Your own agent (an encargo for your Claude)" };

/** What became of the encargo that puts it up to date, in words. */
function encargoText(status?: string): string {
  switch (status) {
    case "pending": return "Waiting for its agent";
    case "taken": case "waiting": return "The agent is putting it up to date";
    case "delivered": return "Delivered: review it and merge";
    case "reviewed": return "Done";
    case "cancelled": return "Cancelled";
    default: return "Sent";
  }
}

/** The reports on the app's dependency updates, on its Caminos tab (none: nothing shows). */
export function AlDiaInformes({ app }: { app: App }) {
  const { data, error, reload } = usePoll(() => api.alDia(app.name), [app.name], 30000);
  if (error) return <ErrorBox error={error} />;
  if (!data || data.informes.length === 0) return null;
  const may = mayActOnCopies(app);
  return (
    <section className="al-dia" id="al-dia" aria-labelledby="al-dia-titulo">
      <div className="al-dia-cabeza">
        <span className="al-dia-sol"><DibujoAlDia id="sol" /></span>
        <div>
          <h2 id="al-dia-titulo">{t("Up to date")}</h2>
          <p className="small muted" style={{ margin: 0 }}>
            {t("Each dependency update, read before it is merged: what would fail, what would change and how much.")}{" "}
            <Link to="/ia#al-dia">{t("Who puts it up to date: {who}", { who: t(quienName[data.ajustes.quien]) })}</Link>
          </p>
        </div>
      </div>
      {data.coderOff && data.ajustes.quien === "platform" && (
        <div className="alert info">{t("The platform's coding agent is off: an update sent to it waits until it is turned on (AI page).")}</div>
      )}
      <div className="al-dia-lista">
        {data.informes.map((r, i) => <Informe key={r.id} app={app.name} r={r} i={i} may={may} quien={data.ajustes.quien} onChange={reload} />)}
      </div>
    </section>
  );
}

function Informe({ app, r, i, may, quien, onChange }: { app: string; r: AlDiaInforme; i: number; may: boolean; quien: AlDiaAjustes["quien"]; onChange: () => void }) {
  const conexion = useConexion();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const lineas = r.cambia.reduce((n, c) => n + c.mas + c.menos, 0);
  const poner = async () => {
    setBusy(true);
    setError(undefined);
    try {
      await api.ponerAlDia(app, r.id);
      onChange();
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <article className={`al-dia-informe estado-${r.estado}`} style={{ animationDelay: `${i * 0.08}s` }} aria-labelledby={`al-dia-${r.id}`}>
      <header className="al-dia-informe-cabeza">
        <div className="al-dia-titulos">
          <h3 id={`al-dia-${r.id}`}>{r.titulo || t("Pull request #{n}", { n: r.pull })}</h3>
          <span className="small muted">
            #{r.pull} · <code>{r.branch}</code> · <Link to={`/apps/${app}/runs/${r.runId}`}>{t("run {n}", { n: r.runId })}</Link>{" "}
            <span className={`badge ${r.runFailed ? "bad" : "ok"}`}>{r.runFailed ? t("failed") : t("passed")}</span> · {timeAgo(r.updatedAt)}
          </span>
        </div>
      </header>

      {r.estado === "pendiente" && (
        <p className="al-dia-pensando" role="status">
          <Flor c="#E8890C" d="0s" /><Flor c="#D6246E" d=".2s" /><Flor c="#23408E" d=".4s" />
          {t("Reading the release notes and the run…")}
        </p>
      )}
      {r.estado === "fallo" && <p className="small muted" style={{ margin: 0 }}>{t("The report could not be written: {why}", { why: r.nota ?? "" })}</p>}
      {r.estado === "sin_cambios" && (
        <p className="al-dia-sin"><DibujoAlDia id="sinCambios" />{t("No changes: the update needs nothing of the app. It can be merged as it is.")}</p>
      )}

      {r.estado === "listo" && (
        <>
          <div className="al-dia-cifras">
            <div className="al-dia-cifra falla">
              <DibujoAlDia id="falla" />
              <span className="al-dia-cifra-que">{t("What would fail")}</span>
              <strong>{tn(r.falla.length, "{n} thing", "{n} things")}</strong>
            </div>
            <div className="al-dia-cifra cambia">
              <DibujoAlDia id="cambia" />
              <span className="al-dia-cifra-que">{t("What would change")}</span>
              <strong>{tn(r.cambia.length, "{n} file", "{n} files")}</strong>
              <span className="small muted">{tn(lineas, "about {n} line", "about {n} lines")}</span>
            </div>
            <div className={`al-dia-cifra riesgo-${r.riesgo ?? "medio"}`}>
              <DibujoAlDia id="riesgo" />
              <span className="al-dia-cifra-que">{t("Risk and cost")}</span>
              <strong>{t(riesgoName[r.riesgo ?? "medio"])} · {dollars(r.costoMicros)}</strong>
            </div>
          </div>
          {r.resumen && <p className="al-dia-resumen">{r.resumen}</p>}
          {(r.falla.length > 0 || r.cambia.length > 0) && (
            <details className="al-dia-detalle">
              <summary>{t("Why, and which files")}</summary>
              <div className="al-dia-detalle-cols">
                {r.falla.length > 0 && <ul>{r.falla.map((f, k) => <li key={k}>{f}</li>)}</ul>}
                {r.cambia.length > 0 && (
                  <ul className="al-dia-archivos">
                    {r.cambia.map((c) => <li key={c.archivo}><span className="al-dia-mas">+{c.mas} −{c.menos}</span> {c.archivo}</li>)}
                  </ul>
                )}
              </div>
            </details>
          )}
          {r.encargoId ? (
            <p className="al-dia-trabaja">
              <DibujoAlDia id={r.encargoRoute === "local" ? "llave" : "telar"} className={r.encargoStatus === "taken" || r.encargoStatus === "waiting" ? "tejiendo" : ""} />
              <span>{t(encargoText(r.encargoStatus))}{r.puestoPor ? ` · ${r.puestoPor}` : ""}</span>
              <Link to={`/ruta?tab=encargos#encargo-${r.encargoId}`}>{t("Encargo {n}", { n: r.encargoId })}</Link>
            </p>
          ) : may ? (
            <div className="al-dia-poner">
              <span className="small">{t("{who} adapts the app on this same branch and tests it; you merge it when it passes.", { who: t(quienName[quien]) })}</span>
              <button type="button" className="al-dia-boton" {...apagado(conexion, busy)} onClick={poner}>
                <DibujoAlDia id="sol" />{busy ? t("Sending…") : t("Put the app up to date · ~{cost}", { cost: dollars(r.costoMicros) })}
              </button>
            </div>
          ) : null}
          <ErrorBox error={error} />
        </>
      )}
      {r.provider && r.estado !== "pendiente" && (
        <span className="al-dia-quien small muted">{t("Report by {model}", { model: r.model || r.provider })}{r.informeMicros > 0 ? ` · ${dollars(r.informeMicros)}` : ""}</span>
      )}
    </article>
  );
}

/** The «Put up to date» card of the AI page. */
export function AlDiaTarjeta() {
  const { data, error, reload } = usePoll(api.alDiaAjustes, [], 0);
  const conexion = useConexion();
  const admin = session.role === "admin";
  const [v, setV] = useState<AlDiaAjustes>();
  const [tope, setTope] = useState("");
  const [saving, setSaving] = useState(false);
  const [failed, setFailed] = useState<unknown>();
  const [saved, setSaved] = useState(false);
  useEffect(() => { if (data) { setV(data); setTope((data.topeMicros / 1e6).toFixed(2)); } }, [data]);
  const cambia = (p: Partial<AlDiaAjustes>) => { setSaved(false); setV((x) => x && { ...x, ...p }); };
  const guardar = async () => {
    if (!v) return;
    setSaving(true);
    setFailed(undefined);
    try {
      await api.putAlDiaAjustes({ ...v, topeMicros: Math.round((parseFloat(tope) || 0) * 1e6) });
      setSaved(true);
      reload();
    } catch (e) {
      setFailed(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <section className="card stack al-dia-tarjeta" id="al-dia" aria-labelledby="al-dia-tarjeta-titulo">
      <div className="al-dia-cabeza">
        <span className="al-dia-sol"><DibujoAlDia id="sol" /></span>
        <div style={{ flex: 1, minWidth: 0 }}>
          <span className="permisos-ruta">{t("AI → Agents")}</span>
          <h2 id="al-dia-tarjeta-titulo">{t("Put up to date")}</h2>
          <p className="small muted" style={{ margin: 0 }}>{t("With each update from Renovate or Dependabot it writes a report: what would fail, what would change and how much. When you say yes, or the rule below allows it, an agent adapts the app to the new version.")}</p>
        </div>
        {v && <Switch checked={v.encendido} disabled={!admin} label={t("Reports on dependency updates")} onChange={(on) => cambia({ encendido: on })} />}
      </div>
      <ErrorBox error={error} />
      {v && (
        <>
          <div className="al-dia-opciones">
            <fieldset disabled={!admin || !v.encendido}>
              <legend><DibujoAlDia id="telar" />{t("Who does it")}</legend>
              {(["platform", "local"] as const).map((q) => (
                <label key={q}><input type="radio" name="al-dia-quien" checked={v.quien === q} onChange={() => cambia({ quien: q })} /> {t(quienName[q])}</label>
              ))}
              <span className="small muted">{t("As with the other agents. The report is always the diagnostician's, with your local model when there is one.")}</span>
            </fieldset>
            <fieldset disabled={!admin || !v.encendido}>
              <legend><DibujoAlDia id="sol" />{t("When")}</legend>
              <label><input type="radio" name="al-dia-cuando" checked={v.cuando === "toda"} onChange={() => cambia({ cuando: "toda" })} /> {t("With every update")}</label>
              <label><input type="radio" name="al-dia-cuando" checked={v.cuando === "falla"} onChange={() => cambia({ cuando: "falla" })} /> {t("Only when its run fails")}</label>
              <span className="small muted">{t("«Every» reads the ones that pass too: a test that does not exist cannot fail.")}</span>
            </fieldset>
            <fieldset disabled={!admin || !v.encendido}>
              <legend><DibujoAlDia id="costo" />{t("Without asking")}</legend>
              <label><input type="radio" name="al-dia-sola" checked={!v.sola} onChange={() => cambia({ sola: false })} /> {t("Always ask me")}</label>
              <label><input type="radio" name="al-dia-sola" checked={v.sola} onChange={() => cambia({ sola: true })} /> {t("By itself when the risk is low and it costs at most")}</label>
              {v.sola && (
                <label className="al-dia-tope">
                  <span>$</span>
                  <input type="number" min="0" max="5" step="0.05" inputMode="decimal" value={tope} aria-label={t("The most it may cost without asking, in dollars")} onChange={(e) => { setSaved(false); setTope(e.target.value); }} />
                </label>
              )}
              <span className="small muted">{t("Merging stays yours, except in the apps that merge by themselves (the app's Settings).")}</span>
            </fieldset>
          </div>
          <div className="al-dia-permisos">
            <strong className="small">{t("Its permissions")}</strong>
            <div className="al-dia-chips">
              <span className="al-dia-chip si">{t("Read the code and the logs")}</span>
              <span className="al-dia-chip si">{t("Write on the update's branch")}</span>
              <span className="al-dia-chip pregunta"><a href="#permisos-coder">{t("Commands in the sandbox: as the coding agent's")}</a></span>
              <span className="al-dia-chip no">{t("Other branches or main: never")}</span>
            </div>
          </div>
          {admin && (
            <div className="row">
              <button type="button" className="primary" {...apagado(conexion, saving)} onClick={guardar}>{saving ? t("Saving…") : t("Save")}</button>
              {saved && <span className="small al-dia-guardado">{t("Saved ✓")}</span>}
              {data?.updatedBy && <span className="small muted">{t("Last changed by {who}", { who: data.updatedBy })}</span>}
            </div>
          )}
          <ErrorBox error={failed} />
        </>
      )}
    </section>
  );
}
