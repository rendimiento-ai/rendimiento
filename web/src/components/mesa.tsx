import { useState } from "react";
import { Link } from "react-router-dom";
import { api, session, timeAgo, type App, type MesaGroup, type MesaLine } from "../api";
import { ErrorBox, usePoll } from "./ui";
import { t, tn } from "../i18n";
import { VolverAlVistazo } from "./vistazo";
import { Rama } from "./lo-ultimo";
import { forjaOf, newPullURL, pullURL, repoURL } from "../forja";

const grupos: { id: MesaGroup; title: string; hint: string }[] = [
  { id: "you", title: "Your turn", hint: "merge, review, or look at what failed" },
  { id: "working", title: "Working", hint: "a run or an agent is on it" },
  { id: "spare", title: "Spare: can be closed", hint: "a copy of another, or forgotten" },
  { id: "today", title: "Merged today", hint: "" },
];

/**
 * The Mesa tab: each line of work of the app (a branch with its pull request, its last run and the encargos about it)
 * grouped by who it waits for, so what is open is in one place instead of five.
 */
export function MesaTab({ app }: { app: App }) {
  const { data, error, reload } = usePoll(() => api.mesa(app.name), [app.name], 30000);
  return (
    <>
      <VolverAlVistazo />
      {session.role !== "guest" && <Pedir app={app} onAsked={reload} />}
      <ErrorBox error={error} />
      {data && !data.github && <p className="small muted">{t("GitHub did not answer: the pull requests are missing, the runs and encargos are here.")}</p>}
      {data && data.lines.length === 0 && <div className="empty">{t("Nothing open: every branch is merged or closed.")}</div>}
      {data && data.lines.length > 0 && (
        <div className="mesa">
          {grupos.map((g) => {
            const lines = data.lines.filter((l) => l.group === g.id);
            if (lines.length === 0) return null;
            return (
              <section key={g.id} className={`mesa-grupo mesa-${g.id}`} aria-label={t(g.title)}>
                <h3>{t(g.title)} <span className="muted small">· {lines.length}{g.hint && ` · ${t(g.hint)}`}</span></h3>
                {lines.map((l) => <Fila key={(l.branch || "") + (l.encargos?.[0]?.id ?? "")} app={app} l={l} onClosed={reload} />)}
              </section>
            );
          })}
        </div>
      )}
    </>
  );
}

const ideas = ["Add tests to it", "Make it load faster", "Review its security", "Explain to me what it does"];

/**
 * Asking for a change without leaving the app: what is written becomes an encargo about this app, and an agent takes it
 * from there. Its first line is the encargo's title; the ideas fill the box with a common ask.
 */
function Pedir({ app, onAsked }: { app: App; onAsked: () => void }) {
  const [texto, setTexto] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const [hecho, setHecho] = useState<number>();
  const pedir = () => {
    const limpio = texto.trim();
    if (!limpio) return;
    const primera = limpio.split("\n")[0];
    const title = primera.length > 120 ? primera.slice(0, 117) + "…" : primera;
    setBusy(true); setErr(undefined);
    api.createEncargo({ title, body: limpio === primera ? "" : limpio, app: app.name }).then((e) => {
      setBusy(false); setTexto(""); setHecho(e.id); onAsked();
    }, (e) => { setBusy(false); setErr(e); });
  };
  return (
    <div className="mesa-pedir">
      <label htmlFor="mesa-pedido"><b>{t("What shall we change in {app}?", { app: app.name })}</b></label>
      <textarea id="mesa-pedido" rows={2} value={texto} maxLength={20000} onChange={(e) => { setTexto(e.target.value); setHecho(undefined); }}
        placeholder={t("e.g. That the page says hello in Spanish and in English")} />
      <div className="mesa-ideas">
        {ideas.map((i) => <button key={i} type="button" className="small" onClick={() => setTexto(t(i))}>{t(i)}</button>)}
      </div>
      <ErrorBox error={err} />
      <div className="row">
        <button className="primary" disabled={busy || !texto.trim()} onClick={pedir}>{t("Ask an agent")}</button>
        {hecho && <span className="small">{t("Asked:")} <Link to={`/ruta?tab=encargos#encargo-${hecho}`}>{t("encargo {n}", { n: hecho })}</Link></span>}
      </div>
    </div>
  );
}

function Fila({ app, l, onClosed }: { app: App; l: MesaLine; onClosed: () => void }) {
  const repo = repoURL(app);
  const encargos = l.encargos ?? [];
  return (
    <div className="mesa-fila">
      <div className="mesa-que">
        <b className="mono">{l.branch ? <Rama repo={app.repo} forja={app.forja} branch={l.branch} className="" /> : encargos[0]?.title}</b>
        {l.pull ? <> · <a href={pullURL(app, l.pull)} target="_blank" rel="noreferrer">{forjaOf(app) === "gitlab" ? "!" : "#"}{l.pull}</a></> : null}
        {l.pullTitle && <div className="small muted">{l.pullTitle}</div>}
      </div>
      <div className="mesa-como small">
        {porque(l, app)}
        {encargos.length > 0 && (
          <div className="muted">
            {encargos.map((e, i) => <span key={e.id}>{i > 0 && " · "}<Link to={`/ruta?tab=encargos#encargo-${e.id}`}>{t("encargo {n}", { n: e.id })}</Link></span>)}
          </div>
        )}
      </div>
      <div className="mesa-accion">{l.group === "spare" && session.role !== "guest" ? <Cerrar app={app} l={l} onClosed={onClosed} /> : accion(l, app, repo)}</div>
    </div>
  );
}

/** What a line's state means, in words. */
export function porque(l: MesaLine, app: App): string {
  const at = l.at && !l.at.startsWith("0001") ? timeAgo(l.at) : "";
  switch (l.why) {
    case "passed": return t("passed: it can be merged");
    case "blocked": return forjaOf(app) === "gitlab" ? t("passed, but GitLab will not merge it (an approval or a required pipeline)") : t("passed, but GitHub will not merge it (a review or a required check)");
    case "conflict": return t("it conflicts with {branch}", { branch: app.defaultBranch });
    case "failed": return l.failed?.length ? t("failed in {steps}", { steps: l.failed.join(", ") }) : t("its run failed");
    case "cancelled": return t("its run was cancelled");
    case "untested": return forjaOf(app) === "gitlab" ? t("an open merge request with no run here") : t("an open pull request with no run here");
    case "review": return t("an agent delivered it: it waits for your review");
    case "running": return l.runStatus === "queued" ? t("run {n} is waiting its turn", { n: l.run ?? 0 }) : t("run {n} is running", { n: l.run ?? 0 });
    case "taken": return l.agent ? t("{agent} has it", { agent: l.agent }) : t("an agent has it");
    case "copy": return t("the same commit as #{n}: it repeats it", { n: l.copyOf ?? 0 });
    case "stale": return forjaOf(app) === "gitlab" ? t("failed {ago}, with no merge request", { ago: at }) : t("failed {ago}, with no pull request", { ago: at });
    case "merged": return l.release ? t("merged {ago} · release #{n}", { ago: at, n: l.release }) : t("merged {ago}", { ago: at });
    default: return l.why;
  }
}

/** The one thing to do about a line, as a link. */
function accion(l: MesaLine, app: App, repo: string) {
  const run = l.run ? `/apps/${app.name}/runs/${l.run}` : "";
  switch (l.why) {
    case "passed":
    case "blocked":
      if (l.pull) return <a className="btn small" href={pullURL(app, l.pull)} target="_blank" rel="noreferrer">{t("Merge")}</a>;
      return <a className="btn small" href={newPullURL(app, l.branch ?? "")} target="_blank" rel="noreferrer">{forjaOf(app) === "gitlab" ? t("Open the merge request") : t("Open the pull request")}</a>;
    case "review":
      return <Link className="btn small" to={`/ruta?tab=encargos${l.encargos?.length ? `#encargo-${l.encargos[0].id}` : ""}`}>{t("Review")}</Link>;
    case "copy":
      return <a className="btn small" href={pullURL(app, l.pull ?? 0)} target="_blank" rel="noreferrer">{forjaOf(app) === "gitlab" ? t("Close it on GitLab") : t("Close it on GitHub")}</a>;
    case "stale":
      return <a className="btn small" href={forjaOf(app) === "gitlab" ? `${repo}/-/branches` : `${repo}/branches`} target="_blank" rel="noreferrer">{t("See the branches")}</a>;
    case "untested":
      return <a className="btn small" href={pullURL(app, l.pull ?? 0)} target="_blank" rel="noreferrer">{t("See it")}</a>;
    case "taken":
      if (!run) return <Link className="btn small" to="/ruta?tab=encargos">{t("See it")}</Link>;
      return <Link className="btn small" to={run}>{t("See it")}</Link>;
    default:
      return run ? <Link className="btn small" to={run}>{l.why === "merged" ? t("See it") : t("See the run")}</Link> : null;
  }
}

/**
 * Closing a spare line: its pull request without merging, and its branch. A person's choice, as merging is; it asks
 * once more before it does it.
 */
function Cerrar({ app, l, onClosed }: { app: App; l: MesaLine; onClosed: () => void }) {
  const [busy, setBusy] = useState(false);
  const close = () => {
    const what = l.pull ? (forjaOf(app) === "gitlab" ? t("Close merge request !{n} without merging it, and delete the branch {branch}?", { n: l.pull, branch: l.branch ?? "" })
      : t("Close pull request #{n} without merging it, and delete the branch {branch}?", { n: l.pull, branch: l.branch ?? "" }))
      : t("Delete the branch {branch}?", { branch: l.branch ?? "" });
    if (!confirm(what)) return;
    setBusy(true);
    api.closeSpare(app.name, l.branch ?? "").then(onClosed, (e: Error) => alert(e.message)).finally(() => setBusy(false));
  };
  return <button className="btn small" disabled={busy} onClick={close}>{l.pull ? t("Close") : t("Delete the branch")}</button>;
}

/** On the home page: how many things wait for the person, per app, each a door to that app's board. */
export function MesaAviso() {
  const { data } = usePoll(() => api.mesaCounts().catch(() => []), [], 60000);
  if (!data || data.length === 0) return null;
  const total = data.reduce((n, c) => n + c.you, 0);
  return (
    <div className="card mesa-aviso" role="status">
      <strong>{tn(total, "{n} thing waits for you", "{n} things wait for you")}</strong>
      <span className="small">
        {data.map((c, i) => (
          <span key={c.app}>{i > 0 && " · "}<Link to={`/apps/${c.app}/mesa`}>{c.app} ({c.you})</Link></span>
        ))}
      </span>
    </div>
  );
}
