// La Ruta: what the platform's people and agents know, kept here rather than
// in any one agent's session. Agents read and write it over MCP (/mcp) with a
// key made on the Keys tab; lo vivido (what people lived) only people write.
import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, session, timeAgo, type AgentKey, type AgentRole, type CreatedAgentKey, type RutaEntry, type RutaInput, type RutaKind } from "../api";
import { Encargos } from "../components/encargos";
import { Agente, useCola } from "../components/cola";
import { ParaUsted } from "../components/portada";
import { Comentarios, porTrabajo } from "../components/comentarios";
import { CopyButton, ErrorBox, usePoll } from "../components/ui";
import { t, tn } from "../i18n";
import { useConexion } from "../conexion";
import { apagado, PorQueApagado } from "../components/conexion-aviso";
import { BorradorGuardado, borradorDe, guardarBorrador, soltarBorrador } from "../components/borrador";
import { TextoConFormato } from "../components/texto";
import { EsqCuaderno, EsqRenglones } from "../components/esqueleto";
import { Platica, usePlatica } from "../components/platica";

/**
 * The Ruta's views (?tab=): the front page «Para usted» (the default), the encargos, the coding agent's
 * queue and the notebook, whose sections open as their own pages: the index of every entry ("ruta",
 * the old default, where ?entrada=N opens one), the to-dos, decisions and manuals alone, lo vivido,
 * the cargos and the agents' keys.
 */
type Vista = "para-usted" | "encargos" | "agente" | "cuaderno" | "ruta" | "pendientes" | "decisiones" | "manuales" | "vivido" | "cargos" | "llaves";
const vistas: Vista[] = ["para-usted", "encargos", "agente", "cuaderno", "ruta", "pendientes", "decisiones", "manuales", "vivido", "cargos", "llaves"];
/** The notebook's sections (the bottom bar marks «Cuaderno» on them). */
const delCuaderno: Vista[] = ["cuaderno", "ruta", "pendientes", "decisiones", "manuales", "vivido", "cargos", "llaves"];
/** The kind of entries each section of one kind shows. */
const kindOf: Partial<Record<Vista, RutaKind>> = { pendientes: "pendiente", decisiones: "decision", manuales: "manual" };

const kinds: { id: RutaKind; label: string }[] = [
  { id: "decision", label: "Decisions" },
  { id: "manual", label: "Manuals" },
  { id: "pendiente", label: "To do" },
  { id: "nota", label: "Notes" },
];

const kindLabel: Record<RutaKind, string> = {
  decision: "Decision", manual: "Manual", pendiente: "To do", nota: "Note", vivido: "Lived",
};

/** A monarch, which no single one of makes the whole journey. */
function Monarca({ size = 34 }: { size?: number }) {
  return (
    <svg className="monarca" width={size} height={size} viewBox="0 0 64 64" aria-hidden="true">
      <g stroke="#1b1b1b" strokeWidth="2.2" strokeLinejoin="round">
        <path d="M31 30 C22 12 6 10 5 20 C4 30 16 33 30 33 Z" fill="#E8890C" />
        <path d="M33 30 C42 12 58 10 59 20 C60 30 48 33 34 33 Z" fill="#E8890C" />
        <path d="M30 34 C18 36 10 46 16 52 C22 57 29 46 31 36 Z" fill="#D9760A" />
        <path d="M34 34 C46 36 54 46 48 52 C42 57 35 46 33 36 Z" fill="#D9760A" />
        <path d="M12 18 L28 31 M52 18 L36 31 M17 47 L30 36 M47 47 L34 36" fill="none" strokeWidth="1.4" />
      </g>
      <ellipse cx="32" cy="34" rx="2.4" ry="13" fill="#1b1b1b" />
      <path d="M31 22 C29 15 26 12 23 11 M33 22 C35 15 38 12 41 11" stroke="#1b1b1b" strokeWidth="1.5" fill="none" />
      <g fill="#fff">
        <circle cx="8" cy="20" r="1.3" /><circle cx="11" cy="26" r="1.1" /><circle cx="56" cy="20" r="1.3" /><circle cx="53" cy="26" r="1.1" />
        <circle cx="16" cy="50" r="1.1" /><circle cx="48" cy="50" r="1.1" />
      </g>
    </svg>
  );
}

/** An entry's tags, as small labels. */
function Tags({ tags }: { tags: string[] }) {
  if (tags.length === 0) return null;
  return <span className="ruta-tags">{tags.map((g) => <span key={g} className="ruta-tag">{g}</span>)}</span>;
}

/** Who wrote an entry (marked when it was an agent), when it changed, and in which cargo. */
function Author({ e }: { e: RutaEntry }) {
  return (
    <span className="small muted">
      {e.byAgent ? <span className="badge ruta-agent" title={t("Written by an agent")}>{t("agent")}</span> : null} {e.author}
      {" · "}{t("updated {when}", { when: timeAgo(e.updatedAt) })}
      {e.cargoId ? <> · {t("cargo #{n}", { n: e.cargoId })}</> : null}
    </span>
  );
}

/** The form to write or change an entry: kind, title, text, tags and, for a to-do, done. */
function EntryForm({ initial, kind, onDone, onCancel }: { initial?: RutaEntry; kind: RutaKind; onDone: () => void; onCancel?: () => void }) {
  // What is being written is kept on this device as a draft until it is saved (components/borrador).
  const caja = `ruta-${initial ? initial.id : "nueva"}`;
  const [kept] = useState(() => {
    try {
      return JSON.parse(borradorDe(caja) ?? "null") as { title: string; body: string; tags: string } | null;
    } catch {
      return null;
    }
  });
  const [form, setForm] = useState<RutaInput>({
    kind: initial?.kind ?? kind, title: kept?.title ?? initial?.title ?? "", body: kept?.body ?? initial?.body ?? "", tags: initial?.tags ?? [], done: initial?.done ?? false,
  });
  const [tags, setTags] = useState(kept?.tags ?? (initial?.tags ?? []).join(", "));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const [guardado, setGuardado] = useState(!!kept);
  const conexion = useConexion();
  useEffect(() => {
    const cambiado = form.title !== (initial?.title ?? "") || form.body !== (initial?.body ?? "") || tags !== (initial?.tags ?? []).join(", ");
    setGuardado(guardarBorrador(caja, cambiado ? JSON.stringify({ title: form.title, body: form.body, tags }) : ""));
  }, [caja, form.title, form.body, tags, initial]);
  const vivido = form.kind === "vivido";
  const save = () => {
    setBusy(true);
    setErr(undefined);
    const body = { ...form, tags: tags.split(",").map((s) => s.trim()).filter(Boolean) };
    (initial ? api.updateRuta(initial.id, body) : api.addRuta(body)).then(
      () => { setBusy(false); soltarBorrador(caja); onDone(); },
      (e) => { setBusy(false); setErr(e); },
    );
  };
  return (
    <div className={`card stack ${vivido ? "vivido-form" : ""}`}>
      {!initial && !vivido && (
        <label className="field"><span>{t("Kind")}</span>
          <select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as RutaKind })}>
            {kinds.map((k) => <option key={k.id} value={k.id}>{t(kindLabel[k.id])}</option>)}
          </select>
        </label>
      )}
      <label className="field"><span>{t("Title")}</span>
        <input value={form.title} maxLength={200} onChange={(e) => setForm({ ...form, title: e.target.value })}
          placeholder={vivido ? t("e.g. Going to school in Cotija") : ""} />
      </label>
      <label className="field"><span>{vivido ? t("Tell it in your own words") : t("Text")}</span>
        <textarea rows={vivido ? 8 : 6} value={form.body} onChange={(e) => setForm({ ...form, body: e.target.value })} />
      </label>
      <label className="field"><span>{t("Tags (comma-separated)")}</span>
        <input value={tags} onChange={(e) => setTags(e.target.value)} placeholder={vivido ? "cotija, escuela" : "stockpulse, respaldos"} />
      </label>
      {form.kind === "pendiente" && (
        <label className="small row" style={{ gap: 6 }}>
          <input type="checkbox" style={{ width: "auto" }} checked={form.done} onChange={(e) => setForm({ ...form, done: e.target.checked })} /> {t("Done")}
        </label>
      )}
      <p className="small muted" style={{ margin: 0 }}>{t("Never write passwords, tokens or keys here; say where they live instead.")}</p>
      <ErrorBox error={err} />
      <div className="row">
        <button className="primary" {...apagado(conexion, busy || !form.title.trim())} onClick={save}>{initial ? t("Save") : vivido ? t("Keep this story") : t("Add")}</button>
        {onCancel && <button onClick={() => { soltarBorrador(caja); onCancel(); }}>{t("Cancel")}</button>}
        <BorradorGuardado si={guardado} />
        <PorQueApagado />
      </div>
    </div>
  );
}

const kindColor: Record<RutaKind, string> = {
  decision: "var(--anil)", manual: "var(--nopal)", pendiente: "var(--cempasuchil)", nota: "var(--muted)", vivido: "var(--rosa)",
};

/**
 * The Ruta tab, as a notebook's index: the entries by kind, each a line with its date at the end of a dotted leader
 * (to-dos with a box to tick), and a search; an entry opens as its own page.
 */
function Entries({ kind, me = "" }: { kind?: RutaKind; me?: string }) {
  // ?entrada=N opens that entry (an app's libreta links to its requests so); ?nueva=1 starts one.
  const [params] = useSearchParams();
  const [q, setQ] = useState(() => params.get("q") ?? "");
  const [adding, setAdding] = useState(() => params.get("nueva") === "1");
  const [open, setOpen] = useState<number | undefined>(() => Number(params.get("entrada")) || undefined);
  const { data, error, reload } = usePoll(() => api.ruta({ q, kind }), [q, kind], 30000);
  const list = (data ?? []).filter((e) => e.kind !== "vivido");
  const shown = list.find((e) => e.id === open);
  if (shown) return <Pagina e={shown} me={me} onBack={() => setOpen(undefined)} onChange={reload} />;
  const toggleDone = (e: RutaEntry) => api.updateRuta(e.id, { ...e, done: !e.done }).then(reload, (x) => alert(x.message));
  return (
    <div className="hoja-vista">
      <div className="hoja-cabeza">
        <h2 className="mano">{kind ? t(kinds.find((k) => k.id === kind)?.label ?? "") : t("Index of the Ruta")}</h2>
        <div className="row">
          <input className="ruta-search" type="search" placeholder={t("Search the notebook…")} aria-label={t("Search the Ruta…")} value={q} onChange={(e) => setQ(e.target.value)} />
          <button className="primary" onClick={() => setAdding(!adding)}>{adding ? t("Close") : t("New entry")}</button>
        </div>
      </div>
      {adding && <EntryForm kind={kind ?? "nota"} onDone={() => { setAdding(false); reload(); }} onCancel={() => setAdding(false)} />}
      <ErrorBox error={error} />
      {!data && !error && <EsqRenglones n={8} />}
      {data && list.length === 0 && <p className="renglon-texto muted">{q ? t("Nothing matches.") : t("Nothing here yet. Agents add decisions, manuals and to-dos as they work; you can too.")}</p>}
      {kinds.map((k) => {
        const items = list.filter((e) => e.kind === k.id);
        if (items.length === 0) return null;
        return (
          <section key={k.id} aria-label={t(k.label)}>
            <div className="indice-grupo" style={{ color: kindColor[k.id] }}>{t(k.label)}</div>
            {items.map((e) => (
              <div key={e.id} className="indice-fila">
                {e.kind === "pendiente" ? (
                  <button className={`check${e.done ? " on" : ""}`} aria-pressed={e.done} aria-label={t("Done")} onClick={() => toggleDone(e)}>
                    <svg width="13" height="13" viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12 5 5 9-10" /></svg>
                  </button>
                ) : <span className="marca" style={{ ["--c" as string]: kindColor[e.kind] }} />}
                <button className="renglon" onClick={() => setOpen(e.id)}>
                  <strong className={e.done ? "tachado" : undefined}>{e.title}</strong>
                  <span className="puntos" />
                  <span className="small muted">{timeAgo(e.updatedAt)}</span>
                </button>
              </div>
            ))}
          </section>
        );
      })}
    </div>
  );
}

/**
 * One entry as a page of the notebook, in two tabs: «La entrada» (its text, who wrote it and when, to edit or
 * archive) and «Platicarla · N», the conversation about it (components/platica). ?platica=1 opens the second.
 * Lo vivido is never talked over with an AI.
 */
function Pagina({ e, me, onBack, onChange }: { e: RutaEntry; me: string; onBack: () => void; onChange: () => void }) {
  const [editing, setEditing] = useState(false);
  const [params] = useSearchParams();
  const platicable = e.kind !== "vivido";
  const [tab, setTab] = useState<"entrada" | "platica">(() => (platicable && params.get("platica") === "1" ? "platica" : "entrada"));
  const { data: platica, error: platicaError, reload: reloadPlatica, setData: setPlatica } = usePlatica(e.id, tab === "platica");
  const n = platica?.mensajes.filter((m) => m.tipo !== "aviso").length ?? 0;
  const tabs = [{ id: "entrada" as const, label: t("The entry") }, { id: "platica" as const, label: t("Talk it over · {n}", { n }) }];
  return (
    <div className="hoja-vista">
      <div className="hoja-cabeza">
        <div className="row">
          <button onClick={onBack}>{t("← Index")}</button>
          <span className="indice-grupo" style={{ color: kindColor[e.kind] }}>{t(kindLabel[e.kind])}</span>
          {e.app && <span className="ruta-tag">{e.app}</span>}
        </div>
      </div>
      {editing ? <EntryForm initial={e} kind={e.kind} onDone={() => { setEditing(false); onChange(); }} onCancel={() => setEditing(false)} /> : (
        <>
          <h2 className="mano pagina-titulo">{e.title}</h2>
          {platicable && (
            <div className="pl-pestanas" role="tablist" aria-label={t("Views of the entry")}>
              {tabs.map((x) => (
                <button key={x.id} type="button" role="tab" id={`pl-tab-${x.id}-${e.id}`} aria-selected={tab === x.id} aria-controls={`pl-panel-${e.id}`}
                  className={tab === x.id ? "on" : undefined} onClick={() => setTab(x.id)}>{x.label}</button>
              ))}
            </div>
          )}
          <div id={`pl-panel-${e.id}`} role={platicable ? "tabpanel" : undefined} aria-labelledby={platicable ? `pl-tab-${tab}-${e.id}` : undefined}>
            {tab === "platica" ? (
              <Platica e={e} me={me} v={platica} error={platicaError} reload={reloadPlatica} setV={setPlatica} />
            ) : (
              <>
                {e.body && <TextoConFormato text={e.body} className="renglon-texto ruta-body" />}
                <div className="renglon-texto"><Tags tags={e.tags} /> <Author e={e} /></div>
                {e.app && (
                  <p className="renglon-texto small">
                    {t("About {app}:", { app: e.app })} <Link to={`/apps/${e.app}/libreta`}>{t("see it in its libreta")}</Link>
                    {e.discarded && <> · {t("set aside: {why}", { why: e.discarded })}</>}
                  </p>
                )}
                <div className="row">
                  <button onClick={() => setEditing(true)}>{t("Edit")}</button>
                  <button onClick={() => confirm(t("Archive “{title}”? It disappears from the list but is kept.", { title: e.title })) && api.archiveRuta(e.id).then(() => { onChange(); onBack(); }, (x) => alert(x.message))}>{t("Archive")}</button>
                </div>
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}

/** The "Lived" tab: what people wrote about working here, in their words (agents cannot write these). */
function Vivido() {
  const [adding, setAdding] = useState(false);
  const { data, error, reload } = usePoll(() => api.ruta({ kind: "vivido" }), [], 60000);
  const [editing, setEditing] = useState<number>();
  return (
    <>
      <p className="sub">{t("Stories people lived, in their own words. Agents read them, with respect, but cannot write or change them: what a person lived is not a technical fact, and is not anyone else's to rewrite.")}</p>
      <button className="primary" onClick={() => setAdding(!adding)}>{adding ? t("Close") : t("Tell a story")}</button>
      {adding && <div style={{ marginTop: 12 }}><EntryForm kind="vivido" onDone={() => { setAdding(false); reload(); }} onCancel={() => setAdding(false)} /></div>}
      <ErrorBox error={error} />
      {!data && !error && <EsqRenglones />}
      {data && data.length === 0 && !adding && <div className="card empty" style={{ marginTop: 12 }}><p>{t("No stories yet.")}</p></div>}
      <div className="stack" style={{ marginTop: 16 }}>
        {data?.map((e) => editing === e.id
          ? <EntryForm key={e.id} initial={e} kind="vivido" onDone={() => { setEditing(undefined); reload(); }} onCancel={() => setEditing(undefined)} />
          : (
            <figure key={e.id} className="vivido">
              <figcaption><strong>{e.title}</strong> <Tags tags={e.tags} /></figcaption>
              <blockquote>{e.body}</blockquote>
              <div className="row between">
                <span className="small muted">— {e.author}, {new Date(e.createdAt).toLocaleDateString()}</span>
                <span className="row">
                  <button onClick={() => setEditing(e.id)}>{t("Edit")}</button>
                  <button onClick={() => confirm(t("Archive “{title}”? It disappears from the list but is kept.", { title: e.title })) && api.archiveRuta(e.id).then(reload, (x) => alert(x.message))}>{t("Archive")}</button>
                </span>
              </div>
            </figure>
          ))}
      </div>
    </>
  );
}

/**
 * The Cargos tab: each agent's turns of work (purpose, handover, what is left) and, on request, every tool call.
 */
function Cargos() {
  const { data, error } = usePoll(api.rutaActivity, [], 20000);
  const { data: comentarios, reload } = usePoll(api.comentarios, [], 20000);
  const hilos = porTrabajo(comentarios, "cargoId");
  const [showActions, setShowActions] = useState(false);
  return (
    <>
      <p className="sub">{t("A cargo is an agent's turn of work: taken with a purpose, handed over with what it did and what is left, so the next one (another session, another model, or a person) knows where things stand.")}</p>
      <ErrorBox error={error} />
      {!data && !error && <EsqRenglones />}
      {data && data.cargos.length === 0 && <div className="card empty"><p>{t("No agent has taken a cargo yet.")}</p></div>}
      <ol className="ruta-cargos">
        {data?.cargos.map((c) => (
          <li key={c.id} className={c.endedAt ? "" : "open"}>
            <div className="card stack">
              <div className="row between" style={{ flexWrap: "wrap" }}>
                <strong>#{c.id} · {c.purpose}</strong>
                <span className={`badge ${c.endedAt ? "ok" : "warn"}`}>{c.endedAt ? t("handed over") : t("in progress")}</span>
              </div>
              <span className="small muted">{c.keyName} · {timeAgo(c.startedAt)}{c.endedAt ? ` → ${timeAgo(c.endedAt)}` : ""}</span>
              {c.entrega && <div><div className="small muted">{t("Handover")}</div><TextoConFormato text={c.entrega} className="ruta-body" /></div>}
              {c.pendientes && <div><div className="small muted">{t("Left to do")}</div><TextoConFormato text={c.pendientes} className="ruta-body" /></div>}
              <Comentarios list={hilos.get(c.id) ?? []} cargoId={c.id} onChange={reload} />
            </div>
          </li>
        ))}
      </ol>
      {data && data.actions.length > 0 && (
        <section style={{ marginTop: 20 }}>
          <a href="#" onClick={(e) => { e.preventDefault(); setShowActions(!showActions); }}>
            {showActions ? t("Hide what agents did") : tn(data.actions.length, "Show the latest agent action", "Show the latest {n} agent actions")}
          </a>
          {showActions && (
            <table className="ruta-actions small">
              <thead><tr><th>{t("When")}</th><th>{t("Key")}</th><th>{t("Tool")}</th><th>{t("Result")}</th></tr></thead>
              <tbody>
                {data.actions.map((a) => (
                  <tr key={a.id} title={a.args}>
                    <td>{timeAgo(a.at)}</td><td>{a.keyName}</td><td className="mono">{a.tool}</td>
                    <td>{a.ok ? "✓" : <span className="ruta-bad">✗ {a.error}</span>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      )}
    </>
  );
}

export const roleLabel: Record<AgentRole, string> = { reader: "Reader", collaborator: "Collaborator", admin: "Administrator" };

/** What a key may do and on which apps, as the keys table shows it. */
function access(k: AgentKey) {
  const apps = k.apps?.length ? k.apps.join(", ") : t("every app");
  return `${t(roleLabel[k.role] ?? "Reader")} · ${apps}`;
}

/**
 * The form to make an agent key: its name, what it may do (read, or also
 * write the Ruta and run CI), on which apps, and how many days it lasts.
 */
function NewKey({ onCreated }: { onCreated: (k: CreatedAgentKey) => void }) {
  const [name, setName] = useState("");
  const [role, setRole] = useState<AgentRole>("collaborator");
  const [apps, setApps] = useState<string[]>([]);
  const { data: allApps } = usePoll(api.apps, [], 0);
  const [days, setDays] = useState(30);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  return (
    <div className="card stack">
      <div className="form-grid">
        <label className="field"><span>{t("Name")}</span>
          <input value={name} maxLength={60} onChange={(e) => setName(e.target.value)} placeholder={t("e.g. claude on main")} />
        </label>
        <label className="field"><span>{t("Lasts")}</span>
          <select value={days} onChange={(e) => setDays(Number(e.target.value))}>
            {[7, 30, 90].map((d) => <option key={d} value={d}>{tn(d, "{n} day", "{n} days")}</option>)}
          </select>
        </label>
        <label className="field"><span>{t("Role")}</span>
          <select value={role} onChange={(e) => setRole(e.target.value as AgentRole)}>
            <option value="collaborator">{t("Collaborator: reads, writes the Ruta (with a cargo), runs and cancels CI")}</option>
            <option value="reader">{t("Reader: reads the Ruta, apps, runs, logs and traces")}</option>
          </select>
        </label>
        <label className="field"><span>{t("Apps")}</span>
          <select multiple value={apps} onChange={(e) => setApps(Array.from(e.target.selectedOptions, (o) => o.value))} size={Math.min(6, Math.max(2, allApps?.length ?? 2))}>
            {allApps?.map((a) => <option key={a.name} value={a.name}>{a.name}</option>)}
          </select>
          <span className="small muted">{apps.length ? tn(apps.length, "only {n} app", "only {n} apps") : t("none chosen: every app")}</span>
        </label>
      </div>
      <ErrorBox error={err} />
      <div><button className="primary" disabled={busy || !name.trim()} onClick={() => {
        setBusy(true); setErr(undefined);
        api.createAgentKey({ name: name.trim(), role, apps, days }).then((k) => { setBusy(false); setName(""); onCreated(k); }, (e) => { setBusy(false); setErr(e); });
      }}>{t("Create key")}</button></div>
    </div>
  );
}

/**
 * Shown once after making a key: the key itself and how to connect an agent with it (Claude Code's command, or a JSON config).
 */
function Connect({ k, onClose }: { k: CreatedAgentKey; onClose: () => void }) {
  const cli = `claude mcp add --transport http rendimiento ${k.endpoint} --header "Authorization: Bearer ${k.token}"`;
  const json = JSON.stringify({ mcpServers: { rendimiento: { type: "http", url: k.endpoint, headers: { Authorization: "Bearer ${RENDIMIENTO_MCP_TOKEN}" } } } }, null, 2);
  const codex = `export RENDIMIENTO_MCP_TOKEN=${k.token}\ncodex mcp add rendimiento --url ${k.endpoint} --bearer-token-env-var RENDIMIENTO_MCP_TOKEN`;
  return (
    <div className="alert info stack" role="status" style={{ position: "relative" }}>
      <button type="button" className="cerrar" onClick={onClose} aria-label={t("Close")} title={t("Close")}>✕</button>
      <strong>{t("Copy the key now: it is shown only this once.")}</strong>
      <div className="row"><code className="mono ruta-token">{k.token}</code><CopyButton text={k.token} /></div>
      <div className="small">{t("In Claude Code, one command:")}</div>
      <div className="row"><pre className="mono ruta-snippet">{cli}</pre><CopyButton text={cli} /></div>
      <div className="small">{t("In Codex: keep the key in an environment variable (in your shell's profile, so it stays), then add the server:")}</div>
      <div className="row"><pre className="mono ruta-snippet">{codex}</pre><CopyButton text={codex} /></div>
      <div className="small">{t("Or, to share it with anyone who opens a repository, commit this .mcp.json and keep the key in the RENDIMIENTO_MCP_TOKEN environment variable, never in git:")}</div>
      <div className="row"><pre className="mono ruta-snippet">{json}</pre><CopyButton text={json} /></div>
      <div><button onClick={onClose}>{t("Done, I saved it")}</button></div>
    </div>
  );
}

// What /encargo asks the agent to do: leave the work in the Ruta for an
// agent to take, instead of doing it there and then.
const encargoInstrucciones = `Use the rendimiento MCP tools to leave an encargo (a piece of work) for an agent to take later; do not do the work yourself.

1. If you have no cargo open, take one with cargo_tomar (purpose: leave an encargo).
2. Create it with encargo_crear: a short title of what needs doing; the app, when this repository or the request makes it clear; and in the body what is known, where to look, and how to know it is done.
3. Answer with the encargo's number, and hand your cargo over with cargo_entregar.

What to ask for:`;

/**
 * The /encargo shortcut for Claude Code and Codex: from a person's own
 * agent, one line leaves work in the Ruta, filled in as an encargo should be.
 */
function Atajo() {
  const claude = `---\ndescription: Leave an encargo in rendimiento's Ruta for an agent to take\nargument-hint: what needs doing\n---\n${encargoInstrucciones} $ARGUMENTS\n`;
  const codex = `---\nname: encargo\ndescription: Leave an encargo in rendimiento's Ruta for an agent to take, when the person asks to leave work for later or for another agent.\n---\n${encargoInstrucciones} what the person asked for.\n`;
  return (
    <details className="card" style={{ marginTop: 16 }}>
      <summary className="small"><strong>{t("The /encargo shortcut")}</strong> · {t("leave work for agents from your own Claude Code or Codex")}</summary>
      <div className="stack" style={{ marginTop: 8 }}>
        <div className="small">{t("Claude Code: save this as .claude/commands/encargo.md (in a repository) or ~/.claude/commands/encargo.md (everywhere), then type /encargo and what needs doing.")}</div>
        <div className="row"><pre className="mono ruta-snippet">{claude}</pre><CopyButton text={claude} /></div>
        <div className="small">{t("Codex: save this as ~/.codex/skills/encargo/SKILL.md, then ask it to leave an encargo.")}</div>
        <div className="row"><pre className="mono ruta-snippet">{codex}</pre><CopyButton text={codex} /></div>
        <div className="small muted">{t("The encargo then says who asked and through which agent; another agent takes it from the Ruta.")}</div>
      </div>
    </details>
  );
}

/**
 * The Keys tab: the agents' keys with their state (active, expired, revoked), and making or revoking one.
 */
function Keys() {
  const { data, error, reload } = usePoll(api.agentKeys, [], 30000);
  const [created, setCreated] = useState<CreatedAgentKey>();
  const now = Date.now();
  const state = (k: AgentKey) =>
    k.revokedAt ? t("revoked") : new Date(k.expiresAt).getTime() < now ? t("expired") : t("active");
  return (
    <>
      <p className="sub">{t("Agents connect to the Ruta over MCP with a key. A key is shown once, stored only as a hash, always expires, and can be revoked here at any time. Everything an agent does with it is recorded on the Cargos tab.")}</p>
      {created ? <Connect k={created} onClose={() => setCreated(undefined)} /> : <NewKey onCreated={(k) => { setCreated(k); reload(); }} />}
      <ErrorBox error={error} />
      <table className="ruta-actions small" style={{ marginTop: 16 }}>
        <thead><tr><th>{t("Name")}</th><th>{t("Access")}</th><th>{t("State")}</th><th>{t("Expires")}</th><th>{t("Last used")}</th><th /></tr></thead>
        <tbody>
          {data?.map((k) => (
            <tr key={k.id} className={k.revokedAt ? "muted" : ""}>
              <td>{k.name}</td>
              <td>{access(k)}</td>
              <td>{state(k)}</td>
              <td>{new Date(k.expiresAt).toLocaleDateString()}</td>
              <td>{k.lastUsedAt ? timeAgo(k.lastUsedAt) : t("never")}</td>
              <td>{!k.revokedAt && (
                <button onClick={() => confirm(t("Revoke “{name}”? Agents using it lose access at once.", { name: k.name })) && api.revokeAgentKey(k.id).then(reload, (e) => alert(e.message))}>{t("Revoke")}</button>
              )}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <Atajo />
    </>
  );
}

/** The four places of the Ruta: on a phone the bar at the bottom, on a computer the tabs on top. */
const lugares: { id: Vista; label: string; icono: string }[] = [
  { id: "para-usted", label: "For you", icono: "M4 13l3-8h10l3 8v6H4z M4 13h5l1 2h4l1-2h5" },
  { id: "encargos", label: "Encargos", icono: "M6 4h12v16H6z M9 9h6 M9 13h6" },
  { id: "agente", label: "The agent", icono: "M5 8h14v11H5z M12 8V4 M9 13h.01 M15 13h.01" },
  { id: "cuaderno", label: "Journal", icono: "M5 5h14v14H5z M5 10h14 M10 10v9" },
];

/** The notebook's sections as colored cards (C2), each with a line of what it holds. */
function Cuaderno({ ir }: { ir: (v: Vista) => void }) {
  const [q, setQ] = useState("");
  const { data: portada } = usePoll(api.portada, [], 60000);
  const { data: llaves } = usePoll(() => (session.role === "admin" ? api.agentKeys().catch(() => []) : Promise.resolve([])), [], 0);
  const { data: hallado } = usePoll(() => (q.trim() ? api.ruta({ q: q.trim() }) : Promise.resolve(undefined)), [q], 0);
  const activas = (llaves ?? []).filter((k) => !k.revokedAt && new Date(k.expiresAt).getTime() > Date.now()).length;
  const c = portada?.counts;
  const tarjetas: { id: Vista; titulo: string; linea: string; tono: string }[] = [
    { id: "pendientes", titulo: t("To-dos"), linea: c ? tn(c.pendientes, "{n} open to-do", "{n} open to-dos") : "", tono: "cempasuchil" },
    { id: "decisiones", titulo: t("Decisions"), linea: t("with their reasons"), tono: "anil" },
    { id: "manuales", titulo: t("Manuals"), linea: t("known failures and how they are fixed"), tono: "nopal" },
    { id: "vivido", titulo: t("Lived"), linea: c ? (c.vividoNew ? tn(c.vividoNew, "{n} new story", "{n} new stories") : t("stories people lived")) : "", tono: "rosa" },
    { id: "cargos", titulo: t("Cargos"), linea: t("who worked and what they handed over"), tono: "terciopelo" },
    ...(session.role === "admin" ? [{ id: "llaves" as Vista, titulo: t("Agent keys"), linea: llaves ? tn(activas, "{n} active key", "{n} active keys") : "", tono: "maiz" }] : []),
  ];
  return (
    <div className="vista-cuaderno">
      <div className="vista-cabeza"><h1 className="row" style={{ gap: 10 }}><Monarca /> {t("The journal")}</h1></div>
      <p className="sub">{t("No monarch makes the whole journey; it takes several generations, and still they find the same forests in Michoacán. Each agent session is one generation: the Ruta keeps the way. Decisions with their reasons, manuals, to-dos, and what each agent handed over, for the next one, whoever it is.")}</p>
      <input className="cu-buscar" type="search" placeholder={t("Search the notebook…")} aria-label={t("Search the Ruta…")} value={q} onChange={(e) => setQ(e.target.value)} />
      {q.trim() ? (
        <section aria-label={t("What was found")} className="cu-hallado">
          {!hallado && <EsqRenglones n={4} />}
          {hallado && !hallado.length && <p className="muted">{t("Nothing matches.")}</p>}
          <ul className="pu-ultimo">{hallado?.slice(0, 30).map((e) => (
            <li key={e.id}><span className="marca" style={{ ["--c" as string]: kindColor[e.kind] }} /> <Link to={`/ruta?tab=${e.kind === "vivido" ? "vivido" : "ruta"}&entrada=${e.id}`}>{e.title}</Link> <span className="small muted">{t(kindLabel[e.kind])} · {timeAgo(e.updatedAt)}</span></li>
          ))}</ul>
        </section>
      ) : (
        <>
          {!portada && <EsqCuaderno />}
          {portada && (
            <div className="cu-tarjetas">
              {tarjetas.map((x) => (
                <button key={x.id} type="button" className={`cu-tarjeta tono-${x.tono}`} onClick={() => ir(x.id)}>
                  <strong>{x.titulo}</strong><span>{x.linea}</span>
                </button>
              ))}
            </div>
          )}
          {portada && portada.latest.length > 0 && (
            <section className="cu-ultimo" aria-labelledby="cu-ultimo">
              <h2 id="cu-ultimo">{t("The latest written")}</h2>
              <ul className="pu-ultimo">{portada.latest.map((e) => (
                <li key={e.id}><span className="marca" style={{ ["--c" as string]: kindColor[e.kind] }} /> <Link to={`/ruta?tab=ruta&entrada=${e.id}`}>{e.title}</Link> <span className="small muted">{timeAgo(e.updatedAt)}</span></li>
              ))}</ul>
            </section>
          )}
          <Link className="et-boton principal" to="/ruta?tab=ruta&nueva=1">{t("+ Write in the Ruta")}</Link>
        </>
      )}
    </div>
  );
}

/**
 * The Ruta page: the platform's memory, kept by people and agents. It opens on «Para usted», what waits
 * for the person; the encargos, the coding agent and the notebook are the other three places.
 */
export function Ruta({ me = "" }: { me?: string }) {
  // ?tab= opens a view (a run's "Ask an agent" lands on encargos); ?entrada=N an entry of the index.
  const [params, setParams] = useSearchParams();
  const asked = params.get("tab") as Vista;
  const vista: Vista = vistas.includes(asked) ? asked : params.has("entrada") ? "ruta" : "para-usted";
  const ir = (id: Vista) => {
    setParams(id === "para-usted" ? {} : { tab: id });
    window.scrollTo({ top: 0 });
  };
  const { data: cola, error: colaError, reload: reloadCola } = useCola();
  const enCuaderno = delCuaderno.includes(vista);
  // The notebook's sections keep the sheet with its section tabs, as before.
  const separadores: { id: Vista; label: string; color: string }[] = [
    { id: "ruta", label: "The Ruta", color: "#F2A93B" }, { id: "pendientes", label: "To-dos", color: "#E8890C" }, { id: "decisiones", label: "Decisions", color: "#9DB4F2" },
    { id: "manuales", label: "Manuals", color: "#A8CFA0" }, { id: "vivido", label: "Lived", color: "#E58FB5" }, { id: "cargos", label: "Cargos", color: "#C9A2D8" },
    ...(session.role === "admin" ? [{ id: "llaves" as Vista, label: "Keys", color: "#E9B824" }] : []),
  ];
  return (
    <div className={`ruta-nueva vista-${vista}`}>
      <nav className="ruta-lugares" aria-label={t("Places of the Ruta")}>
        {lugares.map((l) => {
          const on = l.id === vista || (l.id === "cuaderno" && enCuaderno);
          const cuantos = l.id === "agente" && cola ? cola.now.length + cola.next.length : 0;
          return (
            <a key={l.id} href={l.id === "para-usted" ? "/ruta" : `/ruta?tab=${l.id}`} aria-current={on ? "page" : undefined} className={on ? "on" : undefined}
              onClick={(e) => { e.preventDefault(); ir(l.id); }}>
              <svg width="24" height="24" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d={l.icono} /></svg>
              <span>{t(l.label)}</span>
              {cuantos > 0 && <span className="nav-count">{cuantos}</span>}
            </a>
          );
        })}
      </nav>
      <div className="ruta-vista" key={vista}>
        {vista === "para-usted" && <ParaUsted me={me} cola={cola} onCola={reloadCola} />}
        {vista === "encargos" && <Encargos cola={cola} onCola={reloadCola} />}
        {vista === "agente" && <Agente cola={cola} error={colaError} onChange={reloadCola} />}
        {vista === "cuaderno" && <Cuaderno ir={ir} />}
        {enCuaderno && vista !== "cuaderno" && (
          <>
            <div className="vista-cabeza">
              <a className="cu-volver" href="/ruta?tab=cuaderno" onClick={(e) => { e.preventDefault(); ir("cuaderno"); }}>{t("← Journal")}</a>
              <h1 className="row" style={{ gap: 10 }}><Monarca /> {t("La Ruta")}</h1>
            </div>
            <div className="libro">
              <div className="hoja">
                <div className="anillos" aria-hidden="true">{Array.from({ length: 8 }, (_, i) => <span key={i} />)}</div>
                <div className="vuelta">
                  {(vista === "ruta" || kindOf[vista]) && <Entries key={vista} kind={kindOf[vista]} me={me} />}
                  {vista === "vivido" && <Vivido />}
                  {vista === "cargos" && <Cargos />}
                  {vista === "llaves" && <Keys />}
                </div>
              </div>
              <nav className="separadores" aria-label={t("Sections of the Ruta")}>
                {separadores.map((x) => (
                  <button key={x.id} className={`sep${vista === x.id ? " on" : ""}`} style={{ ["--c" as string]: x.color }} aria-current={vista === x.id ? "page" : undefined} onClick={() => ir(x.id)}>{t(x.label)}</button>
                ))}
              </nav>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
