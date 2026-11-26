// What each agent may do (Ruta 184): the Permissions section of the AI page — the subjects (the platform's
// agents and each MCP key, by a stable id other pages link to: #permisos-<id>), each permission's mode
// (off, ask, always; actions with effect never always) and the apps it is limited to, and the grants that
// still hold; the card in «Para usted» where a person answers what an agent asks; and how a use, a request
// and an answer read in an encargo's history.
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api, session, timeAgo, type Permiso, type PermisoId, type PermisoModo, type PermisoPedido, type PermisoRespuesta, type PermisoSujeto, type PermisosView } from "../api";
import { ErrorBox, usePoll } from "./ui";
import { t } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "./conexion-aviso";
import { EsqBloques } from "./esqueleto";
import { DibujoAgente, DibujoPermiso, PicadoPermisos } from "./permisos-dibujos";

/** The platform's agents, by their stable ids (store.SujetosPlataforma). */
export const sujetoName: Record<string, string> = {
  coder: "The coding agent",
  diagnostician: "The diagnostician",
  estimator: "The estimator",
  vigia: "The lookout",
  clasificador: "The comment sorter",
  platica: "Conversations",
  resumen: "The summarizer",
  traductor: "The translator",
  aprendiz: "The learner",
};

/** A subject's name: a platform's agent's, or an MCP key's. */
export function nombreSujeto(s: { id: string; name?: string }) {
  if (s.id.startsWith("mcp:")) return t("Key {name} (MCP)", { name: s.name || s.id.slice(4) });
  return t(sujetoName[s.id] ?? s.id);
}

export const permisoName: Record<PermisoId, string> = {
  code_read: "Read the code and the Ruta",
  platform_read: "See runs, step logs and slow requests",
  infra_read: "See its app's infrastructure",
  sandbox: "Run commands in the sandbox",
  copies_read: "See its preview copies",
  effects: "Actions with effect",
};

const permisoDesc: Record<PermisoId, string> = {
  code_read: "Files, branches, history. The base: without it there is no agent.",
  platform_read: "runs, step_log, slow_requests, app_status",
  infra_read: "Pods, events, logs (the previous container's too), the rollout and the verification. Read-only, only its own namespace. Never secrets.",
  sandbox: "run: its branch, no secrets, no home network nor cluster.",
  copies_read: "The copy of its pull request, its health and its tests.",
  effects: "Restart a service of its app, run a branch again, make a copy again. Never on main, never another app's.",
};

const modoName: Record<PermisoModo, string> = { off: "Off", ask: "Ask", always: "Always" };
const modos: PermisoModo[] = ["off", "ask", "always"];

/** The permissions a subject's table shows: an MCP key has no sandbox here (it runs on its own computer). */
const filasDe = (s: PermisoSujeto) => s.permisos.filter((p) => !(s.kind === "mcp" && p.permiso === "sandbox"));

/** The Permissions section of the AI page. */
export function Permisos() {
  const { data, error, reload } = usePoll(api.permisos, [], 0);
  const [sujeto, setSujeto] = useState<string>(() => (location.hash.startsWith("#permisos-") ? decodeURIComponent(location.hash.slice(10)) : "coder"));
  return (
    <section className="card stack permisos-agentes" id="permisos" aria-labelledby="permisos-titulo">
      <PicadoPermisos />
      <span className="permisos-ruta">{t("AI → Permissions")}</span>
      <h2 id="permisos-titulo">{t("What the agents may do")}</h2>
      <p className="small muted" style={{ margin: 0 }}>{t("Always of the encargo's app, never another's. They never see a secret's value, nor merge, nor publish from main: that is a person's.")}</p>
      <ErrorBox error={error} />
      {!data && !error && <EsqBloques alturas={[44, 320, 120]} />}
      {data && <PermisosTabla v={data} sujeto={data.sujetos.some((s) => s.id === sujeto) ? sujeto : "coder"} setSujeto={setSujeto} onSaved={reload} />}
    </section>
  );
}

function PermisosTabla({ v, sujeto, setSujeto, onSaved }: { v: PermisosView; sujeto: string; setSujeto: (s: string) => void; onSaved: () => void }) {
  const conexion = useConexion();
  const admin = session.role === "admin";
  const s = v.sujetos.find((x) => x.id === sujeto)!;
  const guardado = useMemo(() => Object.fromEntries(s.permisos.map((p) => [p.permiso, p])) as Record<PermisoId, Permiso>, [s]);
  const [cambios, setCambios] = useState<Partial<Record<PermisoId, Pick<Permiso, "modo" | "apps">>>>({});
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [err, setErr] = useState<unknown>();
  const [escoger, setEscoger] = useState<PermisoId>();
  useEffect(() => { setCambios({}); setEscoger(undefined); }, [sujeto, JSON.stringify(s.permisos)]);
  const actual = (p: PermisoId) => ({ ...guardado[p], ...cambios[p] });
  const poner = (p: PermisoId, c: Partial<Pick<Permiso, "modo" | "apps">>) => {
    setSaved(false);
    const next = { modo: actual(p).modo, apps: actual(p).apps, ...c };
    const g = guardado[p];
    setCambios((old) => {
      const o = { ...old };
      if (next.modo === g.modo && [...next.apps].sort().join() === [...g.apps].sort().join()) delete o[p];
      else o[p] = next;
      return o;
    });
  };
  const lista = Object.entries(cambios) as [PermisoId, Pick<Permiso, "modo" | "apps">][];
  const appsText = (apps: string[]) => (apps.length ? apps.join(", ") : t("All"));
  const save = () => {
    setBusy(true); setErr(undefined);
    api.savePermisos(sujeto, lista.map(([permiso, c]) => ({ permiso, modo: c.modo, apps: c.apps })))
      .then(() => { setBusy(false); setSaved(true); onSaved(); }, (e) => { setBusy(false); setErr(e); });
  };
  const dados = v.dados.filter((d) => d.sujeto === sujeto);
  return (
    <>
      <div className="permisos-sujetos" role="radiogroup" aria-label={t("For whom")}>
        {v.sujetos.map((x) => (
          <button key={x.id} type="button" role="radio" aria-checked={x.id === sujeto} id={`permisos-${x.id}`}
            className={`permisos-sujeto${x.id === sujeto ? " on" : ""}${x.kind === "mcp" ? " llave" : ""}`}
            onClick={() => { setSujeto(x.id); history.replaceState(null, "", `#permisos-${x.id}`); }}>
            <DibujoAgente id={x.id} />
            <span>{nombreSujeto(x)}</span>
          </button>
        ))}
      </div>
      <div className="permisos-filas" role="table" aria-label={t("Permissions of {who}", { who: nombreSujeto(s) })}>
        <div className="permisos-fila cabeza" role="row">
          <span role="columnheader">{t("Permission")}</span><span role="columnheader">{t("Mode")}</span><span role="columnheader">{t("Apps")}</span>
        </div>
        {filasDe(s).map((g) => {
          const p = actual(g.permiso);
          const fijo = g.permiso === "code_read";
          return (
            <div key={g.permiso} role="row" className={`permisos-fila${cambios[g.permiso] ? " cambiada" : ""}${g.permiso === "effects" ? " efecto" : ""}`}>
              <span role="cell" className="permisos-que">
                <span className="permisos-titulo"><DibujoPermiso id={g.permiso} /><strong>{t(permisoName[g.permiso])}</strong></span>
                {g.permiso === "infra_read" && <span className="permisos-marca nuevo">{t("new")}</span>}
                {g.permiso === "effects" && <span className="permisos-marca cuidado">{t("careful")}</span>}
                <span className="small muted">{g.permiso === "platform_read" || g.permiso === "sandbox" ? <code>{t(permisoDesc[g.permiso])}</code> : t(permisoDesc[g.permiso])}</span>
                {g.alcance === "steps" && <span className="small">{t("Always means only its failed run's step pods, their events and the nodes' pressure; the app's own namespace asks.")}</span>}
              </span>
              <span role="cell">
                {fijo ? <span className="badge ok">{t("Always")}</span> : (
                  <span className="permisos-modo" role="radiogroup" aria-label={t("Mode: {what}", { what: t(permisoName[g.permiso]) })}>
                    {modos.map((m) => {
                      const no = m === "always" && g.permiso === "effects";
                      return (
                        <button key={m} type="button" role="radio" aria-checked={p.modo === m} disabled={!admin || no}
                          title={no ? t("An action with effect always asks: each use is a person's yes") : undefined}
                          className={`modo-${m}${p.modo === m ? " on" : ""}`} onClick={() => poner(g.permiso, { modo: m })}>{t(modoName[m])}</button>
                      );
                    })}
                  </span>
                )}
              </span>
              <span role="cell" className="permisos-apps small">
                {fijo ? t("All") : (
                  <>
                    {appsText(p.apps)}
                    {admin && v.apps.length > 0 && <> · <button type="button" className="linkish" aria-expanded={escoger === g.permiso} onClick={() => setEscoger(escoger === g.permiso ? undefined : g.permiso)}>{t("choose")}</button></>}
                    {escoger === g.permiso && (
                      <span className="permisos-escoger">
                        {v.apps.map((a) => (
                          <label key={a}><input type="checkbox" checked={p.apps.includes(a)}
                            onChange={(e) => poner(g.permiso, { apps: e.target.checked ? [...p.apps, a] : p.apps.filter((x) => x !== a) })} /> {a}</label>
                        ))}
                        <span className="small muted">{t("None chosen: all of them.")}</span>
                      </span>
                    )}
                  </>
                )}
              </span>
            </div>
          );
        })}
      </div>
      <div className="permisos-tarjetas">
        <div className="permisos-tarjeta modo-off"><strong>{t("Off")}</strong><span className="small">{t("The tool does not exist for the agent. If it needs it, it says so in its delivery.")}</span></div>
        <div className="permisos-tarjeta modo-ask"><strong>{t("Ask")}</strong><span className="small">{t("The agent asks what it wants to see and why. You give it only this time, for the encargo, or always for that app.")}</span></div>
        <div className="permisos-tarjeta modo-always"><strong>{t("Always")}</strong><span className="small">{t("It uses it without asking. Each use stays in the encargo's history and in the bitácora.")}</span></div>
      </div>
      {dados.length > 0 && (
        <div className="stack" style={{ gap: 6 }}>
          <strong>{t("Permissions given")}</strong>
          <ul className="permisos-dados">
            {dados.map((d) => (
              <li key={d.id}>
                <span><strong>{t(permisoName[d.permiso])}</strong> · {d.app} · {t(alcanceName[d.alcance])}{d.encargoId ? ` · ${t("encargo #{n}", { n: d.encargoId })}` : ""}
                  <span className="small muted"> · {t("{who} gave it, {when}", { who: d.grantedBy, when: timeAgo(d.grantedAt) })}</span></span>
                {admin && <button type="button" className="small" disabled={busy} onClick={() => api.quitarPermiso(d.id).then(onSaved, setErr)}>{t("Take it back")}</button>}
              </li>
            ))}
          </ul>
        </div>
      )}
      <ErrorBox error={err} />
      {!admin ? <p className="small muted" style={{ margin: 0 }}>{t("Only administrators change this.")}</p> : lista.length ? (
        <div className="ia-savebar">
          <div className="small">
            <strong>{t("Not saved yet:")}</strong>
            <ul>{lista.map(([p, c]) => (
              <li key={p}>{t(permisoName[p])}: <span className="muted">{t(modoName[guardado[p].modo])} · {appsText(guardado[p].apps)}</span> → <strong>{t(modoName[c.modo])} · {appsText(c.apps)}</strong></li>
            ))}</ul>
          </div>
          <div className="row" style={{ gap: 8 }}>
            <button className="primary" {...apagado(conexion, busy)} onClick={save}>{busy ? t("Saving…") : t("Save")}</button>
            <button disabled={busy} onClick={() => setCambios({})}>{t("Discard")}</button>
          </div>
        </div>
      ) : saved ? <span className="badge ok">{t("Saved")}</span> : null}
    </>
  );
}

const alcanceName: Record<string, string> = { once: "only this time", encargo: "while the encargo lasts", cargo: "while its cargo lasts", always: "always" };

/**
 * «The agent asks for permission» in «Para usted»: what it wants, of which app, why, and the window; the
 * grant's reach (only this time, while the encargo — or the key's cargo — lasts, always for the app) and
 * «No». An action with effect only ever takes «Do it» or «No». Administrators give it; a collaborator may say no.
 */
export function PidePermiso({ p, onDone }: { p: PermisoPedido; onDone: (msg: string) => void }) {
  const efecto = p.permiso === "effects";
  const llave = p.sujeto.startsWith("mcp:");
  const opciones: PermisoRespuesta[] = efecto ? ["once"] : llave && !p.cargoId ? ["once", "always"] : ["once", "encargo", "always"];
  const [alcance, setAlcance] = useState<PermisoRespuesta>("once");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const admin = session.role === "admin";
  const puedeNo = session.role !== "guest";
  const quien = llave ? t("Key {name} (MCP)", { name: p.keyName || p.sujeto.slice(4) }) : t("Encargo {n}", { n: p.encargoId ?? 0 });
  const opcionName = (o: PermisoRespuesta) => o === "once" ? t("Only this time") : o === "always" ? t("Always, for {app}", { app: p.app })
    : llave ? t("While its cargo lasts") : t("While this encargo lasts");
  const responder = async (a: PermisoRespuesta) => {
    setBusy(true); setError(undefined);
    try {
      await api.answerPermiso(p.id, a);
      onDone(a === "no" ? t("Said no: the agent goes on without it.") : t("Permission given: the agent goes on."));
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className={`pu-grupo tono-anil pu-permiso${efecto ? " efecto" : ""}`} aria-labelledby={`pu-permiso-${p.id}`}>
      <div className="pu-grupo-cabeza">
        <span className="pu-signo pu-signo-dibujo" aria-hidden="true">{efecto ? <DibujoPermiso id="effects" /> : <DibujoAgente id={p.sujeto} />}</span>
        <span className="pu-texto">
          <strong id={`pu-permiso-${p.id}`}>{efecto ? t("It wants to do something with effect") : t("The agent asks for permission")}</strong>
          <span>{quien}{p.encargoTitle ? ` · ${p.encargoTitle}` : ""} · {p.app} · {timeAgo(p.askedAt)}{!llave && <> · {t("it waits, spending nothing")}</>}</span>
        </span>
        {efecto && <span className="permisos-marca cuidado">{t("always asks")}</span>}
      </div>
      <p className="pu-pregunta-texto"><strong>{t("It wants:")}</strong> {t(permisoName[p.permiso])} · {p.app}</p>
      {p.que && <p className="small" style={{ margin: 0 }}>{p.que}{p.ventana ? ` · ${p.ventana}` : ""}</p>}
      <p className="small" style={{ margin: 0 }}><strong>{t("Why:")}</strong> {p.porQue ? `«${p.porQue}»` : <span className="muted">{t("it did not say yet")}</span>}</p>
      {!efecto && (
        <div className="pu-opciones" role="radiogroup" aria-label={t("Give it permission")}>
          {opciones.map((o) => (
            <label key={o} className={`pu-opcion${alcance === o ? " elegida" : ""}`}>
              <input type="radio" name={`permiso-${p.id}`} checked={alcance === o} disabled={!admin} onChange={() => setAlcance(o)} />
              <span>{opcionName(o)}</span>
            </label>
          ))}
        </div>
      )}
      <ErrorBox error={error} />
      {puedeNo && (
        <div className="pu-acciones">
          {admin && <button type="button" className="et-boton principal anil" disabled={busy} onClick={() => responder(efecto ? "once" : alcance)}>{efecto ? t("Do it") : t("Give permission")}</button>}
          <button type="button" className="et-boton" disabled={busy} onClick={() => responder("no")}>{t("No")}</button>
        </div>
      )}
      {!admin && puedeNo && <p className="small muted">{t("Only administrators give permissions; you can say no.")}</p>}
      <p className="small muted">{t("With «No», it goes on without it and says so in its delivery. It never sees secrets' values.")} {t("The permissions given «always» are taken back on")} <Link to="/ia#permisos">{t("AI → Permissions")}</Link>.</p>
    </section>
  );
}

/** How a use, a request, a grant and a no read in an encargo's history (the Uso mock). */
export const usoKind: Record<string, string> = {
  permiso_uso: "used",
  permiso_pedido: "asked for permission",
  permiso_dado: "gave permission",
  permiso_negado: "said no",
};

/** The short name of a permission as a line of the history says it. */
export const usoPermiso: Record<PermisoId, string> = {
  code_read: "the code",
  platform_read: "runs",
  infra_read: "infrastructure",
  sandbox: "sandbox",
  copies_read: "copies",
  effects: "with effect",
};

/** One permission line of an encargo's history. */
export function UsoLinea({ l }: { l: { at: string; who: string; what: string; note?: string; permiso?: PermisoId } }) {
  const hora = new Date(l.at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return (
    <div className={`uso-linea ${l.what}`}>
      <span className="uso-cuando">{hora} · {l.permiso && l.what === "permiso_uso" ? t(usoPermiso[l.permiso]) : t(usoKind[l.what] ?? l.what)}</span>
      <span className="uso-que">{l.what === "permiso_dado" || l.what === "permiso_negado" ? <>{l.who} · {t(usoKind[l.what])}{l.note ? ` · ${l.note}` : ""}</> : l.note}</span>
    </div>
  );
}
