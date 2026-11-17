import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, dollars, duration, session, subscribe, timeAgo, type App, type Diagnosis, type Run, type Step } from "../api";
import { RunBadge } from "../components/ui";
import { Falla, NoExiste } from "../components/falla";
import { lang, t, tn } from "../i18n";
import { TextoConFormato } from "../components/texto";
import { Rama } from "../components/lo-ultimo";
import { Pasos } from "../components/pasos";
import { EsqCosa } from "../components/esqueleto";
import { anotar } from "../uso";

/** A run's page: why it failed, its steps with when each ran, live as they go, and the log of the step chosen. */
export function RunPage() {
  const { name = "", id = "" } = useParams();
  const runId = Number(id);
  const [run, setRun] = useState<Run>();
  const [error, setError] = useState<unknown>();
  const [vuelta, setVuelta] = useState(0); // bumped to load the run again after rendimiento did not answer
  const [selected, setSelected] = useState<string>();
  const [logs, setLogs] = useState<Record<string, string>>({});
  const [diagnosis, setDiagnosis] = useState<Diagnosis | null>();
  const [app, setApp] = useState<App>();
  const logRef = useRef<HTMLDivElement>(null);
  const follow = useRef(true);

  // Initial state, then live updates over SSE. The stream can miss events (the server drops them for a page that is
  // behind, and a phone drops the stream of a tab in the background), so the run is fetched again when the stream
  // reopens, when the page is seen again, and every few seconds while it goes. Only the run is replaced: the step chosen
  // and the log's scroll stay as they are.
  const [fresco, setFresco] = useState(0); // bumped when the stream or the page come back: the chosen step's log too
  const pedidos = useRef(0);
  const recargar = useCallback((log = false) => {
    const n = ++pedidos.current;
    api.run(runId).then((r) => {
      if (n !== pedidos.current) return; // a later fetch is on its way
      setRun((cur) => (cur ? { ...cur, ...r } : r));
      setSelected((cur) => cur ?? primero(r));
      if (log) setFresco((x) => x + 1);
    }, () => {});
  }, [runId]);
  useEffect(() => {
    api.run(runId).then((r) => {
      setRun(r);
      setSelected((cur) => cur ?? primero(r));
    }, setError);
    return subscribe(`/api/runs/${runId}/events`, {
      // The run, steps and all; what only the page's fetch says (stumbles, limits) is kept.
      run: (r: Run) => {
        setRun((cur) => (cur ? { ...cur, ...r } : r));
        setSelected((cur) => cur ?? primero(r));
      },
      step: (e: PasoEnVivo) => setRun((cur) => cur && { ...cur, steps: cur.steps?.map((x) => (x.id === e.id ? enVivo(x, e) : x)) }),
      log: (l: { step: string; text: string }) => setLogs((cur) => ({ ...cur, [l.step]: (cur[l.step] ?? "") + l.text })),
    }, () => recargar(true));
  }, [runId, recargar, vuelta]);
  const anda = run?.status === "queued" || run?.status === "running";
  useEffect(() => {
    const visto = () => { if (document.visibilityState === "visible") recargar(true); };
    document.addEventListener("visibilitychange", visto);
    return () => document.removeEventListener("visibilitychange", visto);
  }, [recargar]);
  useEffect(() => {
    if (!anda) return;
    const cada = setInterval(() => { if (document.visibilityState !== "hidden") recargar(); }, 10000);
    return () => clearInterval(cada);
  }, [anda, recargar]);

  // How long each step took in the last run of the same kind (deploying or not) that passed: a faint mark in its lane.
  const [antes, setAntes] = useState<Run>();
  const deploy = run?.deploy;
  useEffect(() => {
    if (deploy === undefined) return;
    let vivo = true;
    api.runs(name).then((rs) => {
      const otra = rs.find((r) => r.id < runId && r.status === "succeeded" && r.deploy === deploy);
      return otra && api.run(otra.id).then((r) => { if (vivo) setAntes(r); });
    }).catch(() => {});
    return () => { vivo = false; };
  }, [name, runId, deploy]);

  // The app says whether rendimiento merges, and with whose yes.
  useEffect(() => {
    api.app(name).then(setApp, () => {});
  }, [name]);

  // Stored log for the selected step (covers finished steps and reconnects).
  useEffect(() => {
    if (!selected) return;
    api.stepLog(runId, selected).then((text) => setLogs((cur) => (text.length >= (cur[selected]?.length ?? 0) ? { ...cur, [selected]: text } : cur)), () => {});
  }, [runId, selected, run?.steps?.find((s) => s.id === selected)?.status, fresco]);

  useEffect(() => {
    const el = logRef.current;
    if (el && follow.current) el.scrollTop = el.scrollHeight;
  }, [logs, selected]);

  // The page is about the run: one that is not there, or not for this account, is a page of its own.
  if (!Number.isInteger(runId) || runId <= 0) return <NoExiste />;
  if (error) return <Falla error={error} reintentar={() => { setError(undefined); setVuelta((v) => v + 1); }} />;
  if (!run) return <EsqCosa volver acciones cuerpo="pasos" />;
  const step = run.steps?.find((s) => s.id === selected);

  return (
    <>
      <p className="small"><Link to={`/apps/${name}/runs`}>{t("← {name} runs", { name })}</Link></p>
      <div className="row between">
        <div className="row">
          <h1 data-listo="">{t("Run #{n}", { n: run.id })}</h1>
          <RunBadge status={run.status} />
        </div>
        {(run.status === "running" || run.status === "queued") && (
          <button className="danger" onClick={() => { anotar("cancel_run"); api.cancelRun(run.id).catch((e) => alert(e.message)); }}>{t("Cancel")}</button>
        )}
        {run.status === "failed" && (
          <div className="row" style={{ gap: 8 }}>
            <Retry run={run} app={app} onRetried={(r) => setRun((cur) => ({ ...cur, ...r }))} />
            <AskAgent run={run} app={name} diagnosis={diagnosis?.status === "done" ? diagnosis.text : undefined} />
          </div>
        )}
      </div>
      <p className="sub">
        <span className="mono">{run.sha.slice(0, 7)}</span> {t("on")} <strong>{app ? <Rama repo={app.repo} branch={run.branch} className="" /> : run.branch}</strong> · {t(run.event)}
        {run.deploy ? ` · ${t("deploys on success")}` : ` · ${t("build only")}`} · {timeAgo(run.createdAt)}
        {run.startedAt && ` · ${duration(run.startedAt, run.finishedAt)}`}
        {run.message && ` · ${run.message}`}
        {!!run.retries && ` · ${tn(run.retries, "retried once", "retried {n} times")}`}
      </p>

      {run.status === "failed" && <WhyFailed run={run} d={diagnosis} setD={setDiagnosis} />}
      {run.merge === "conflict" && <Conflict run={run} app={name} />}
      {app && <Approve run={run} app={app} onApproved={setRun} />}

      {run.steps?.length ? (
        <div className="ejecucion">
          <Pasos run={run} antes={antes} selected={selected} onSelect={(id) => { anotar("select_step"); setSelected(id); }} />
          {step && (
            <div className="ejecucion-registro">
              <div className="row">
                <strong>{step.id}</strong>
                <RunBadge status={step.status} />
                <span className="small muted">{duration(step.startedAt, step.finishedAt)}</span>
                {(step.tries ?? 1) > 1 && <span className="badge">{t("try {n}", { n: step.tries! })}</span>}
                {(run.stumbles?.[step.id] ?? 0) >= (run.flakyAfter ?? 3) && (
                  <span className="badge warn" title={t("It failed and then passed on the same commit {n} times in 30 days: retrying hides it, fixing it does not.", { n: run.stumbles![step.id] })}>{t("flaky")}</span>
                )}
                {step.digest && <span className="mono small muted" title={step.digest}>{step.digest.slice(0, 19)}…</span>}
                <span style={{ flex: 1 }} />
                {firstError(logs[step.id]) >= 0 && (
                  <button className="small" onClick={() => { follow.current = false; logRef.current?.querySelector(".err")?.scrollIntoView({ block: "center", behavior: "smooth" }); }}>{t("Jump to the error")}</button>
                )}
              </div>
              {step.message && step.status !== "succeeded" && step.status !== "reused" && <div className="alert">{step.message}</div>}
              <Registro text={logs[step.id]} innerRef={logRef} onScroll={(near) => { follow.current = near; }}
                empty={step.status === "pending" ? t("Waiting to start…") : step.status === "skipped" ? t("Skipped.") : step.status === "reused" ? t("Nothing in this service changed, so it was not rebuilt: ") + (step.message ?? "") : t("No output yet.")} />
            </div>
          )}
        </div>
      ) : run.status === "failed" && run.message ? (
        <div className="alert" role="alert">
          <strong>{t("Nothing was built.")}</strong> {run.message}
          <div className="small" style={{ marginTop: 6 }}>{t("Fix the file and push again; the app keeps running its current release.")}</div>
        </div>
      ) : (
        <p className="muted">{t("Waiting to start…")}</p>
      )}
    </>
  );
}

/** The step to show first: the one running or failed, else the first. */
function primero(r: Run): string | undefined {
  return (r.steps?.find((s) => s.status === "running" || s.status === "failed") ?? r.steps?.[0])?.id;
}

/** A step's live event: its status, and its times when the server says them (an older server does not). */
type PasoEnVivo = Pick<Step, "id" | "status"> & Partial<Pick<Step, "digest" | "message" | "startedAt" | "finishedAt">>;

/**
 * A step after its live event. The times the server stored win; without them, a step that starts starts now and one
 * that ends (having started) ends now, so its bar stops instead of growing with the clock until a reload.
 */
function enVivo(x: Step, e: PasoEnVivo): Step {
  const ahora = new Date().toISOString();
  const y: Step = { ...x, ...e };
  switch (e.status) {
    case "pending":
      y.startedAt = e.startedAt;
      y.finishedAt = e.finishedAt;
      break;
    case "running":
      // A retried step starts again: its old times are not this try's.
      y.startedAt = e.startedAt ?? (x.status === "running" ? x.startedAt : undefined) ?? ahora;
      y.finishedAt = undefined;
      break;
    default: // succeeded, failed, skipped, reused
      y.startedAt = e.startedAt ?? x.startedAt;
      y.finishedAt = e.finishedAt ?? (y.startedAt ? (x.status === "running" ? ahora : x.finishedAt ?? ahora) : undefined);
  }
  return y;
}

/**
 * Retry what failed: runs again, in this same run, the steps that failed or were skipped; those that passed keep their
 * result and their image. A run of the main branch deploys, so only the app's administrators retry it.
 */
function Retry({ run, app, onRetried }: { run: Run; app?: App; onRetried: (r: Run) => void }) {
  const [busy, setBusy] = useState(false);
  if (session.role === "guest" || (run.retries ?? 0) >= (run.maxRetries ?? 2)) return null;
  if (run.deploy && app?.role !== "admin") return null;
  const go = () => {
    setBusy(true);
    anotar("retry");
    api.retryRun(run.id).then((r) => { setBusy(false); onRetried(r); }, (e) => { setBusy(false); alert(e.message); });
  };
  return (
    <button disabled={busy} onClick={go} title={t("Runs again only what did not pass; what passed keeps its result and its image.")}>
      {busy ? t("Retrying…") : t("Retry what failed")}
    </button>
  );
}

const errLine = /\b(error|errors|failed|fail|failure|panic|fatal)\b|^--- FAIL|^FAIL\b/i;
const notErr = /\b(0|no) (errors|failures)\b/i;
const isErr = (l: string) => errLine.test(l) && !notErr.test(l);

/** The line of the first error in a log, or -1. */
function firstError(text?: string) {
  return formatLog(text).split("\n").findIndex(isErr);
}

/** A step's log with line numbers, its error lines marked and its groups as headings. */
function Registro({ text, empty, innerRef, onScroll }: { text?: string; empty: string; innerRef: React.RefObject<HTMLDivElement | null>; onScroll: (nearBottom: boolean) => void }) {
  const lines = formatLog(text).replace(/\n$/, "").split("\n");
  return (
    <div className="reg" role="log" aria-label={t("Step log")} ref={innerRef}
      onScroll={(e) => { const el = e.currentTarget; onScroll(el.scrollHeight - el.scrollTop - el.clientHeight < 40); }}>
      {!text ? <div className="reg-vacio">{empty}</div> : lines.map((l, i) => (
        <div key={i} className={isErr(l) ? "err" : l.startsWith("── ") ? "grp" : undefined}><span className="n">{i + 1}</span>{l}</div>
      ))}
    </div>
  );
}

/** Drops the ::group:: markers the executor uses to separate clone and step output. */
function formatLog(s?: string) {
  if (!s) return "";
  return s.replace(/^::group::(.*)$/gm, "── $1 ──").replace(/^::endgroup::$\n?/gm, "");
}

/**
 * On a branch's run, in an app that merges by itself: the person's yes to
 * merging this commit, and who gave it. rendimiento opens the pull request
 * when the branch has none, and merges once the run has passed.
 */
function Approve({ run, app, onApproved }: { run: Run; app: App; onApproved: (r: Run) => void }) {
  const [busy, setBusy] = useState(false);
  const alive = run.status === "queued" || run.status === "running" || run.status === "succeeded";
  if (!app.autoMerge || run.deploy || run.branch === app.defaultBranch || !alive || run.merge === "merged" || run.merge === "conflict") return null;
  if (run.approvedBy) {
    return (
      <div className="card">
        <span className="small" id="merge-approved">
          {run.status === "succeeded"
            ? t("{who} approved merging it: rendimiento merges it into {branch} in a moment, or says here what GitHub asks for first.", { who: run.approvedBy, branch: app.defaultBranch })
            : t("{who} approved merging it: rendimiento merges it into {branch} when this run passes.", { who: run.approvedBy, branch: app.defaultBranch })}
        </span>
      </div>
    );
  }
  if (session.role === "guest") return null;
  return (
    <div className="card row">
      <button id="merge-approve" disabled={busy} onClick={() => {
        setBusy(true);
        anotar("merge");
        api.approveMerge(run.id).then((r) => { setBusy(false); onApproved({ ...run, ...r, steps: run.steps }); }, (e) => { setBusy(false); alert(e.message); });
      }}>{t("Merge this branch")}</button>
      <span className="small muted">
        {run.status === "succeeded"
          ? t("Your yes to this commit: rendimiento opens the pull request into {branch} if there is none, merges it and deletes the branch.", { branch: app.defaultBranch })
          : t("Your yes to this commit: rendimiento opens the pull request into {branch} if there is none, and merges it when this run passes.", { branch: app.defaultBranch })}
      </span>
    </div>
  );
}

/**
 * On a run that passed but whose pull request conflicts: why rendimiento
 * did not merge it, and handing the conflict to agents as an encargo, which
 * is estimated (how hard, what the platform's AI would spend) before
 * anyone does it.
 */
function Conflict({ run, app }: { run: Run; app: string }) {
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const body = [
    t("Pull request #{pull} of {app} ({branch}, {sha}) passed run #{n}, but it conflicts with the default branch, so rendimiento did not merge it.", { pull: run.pull ?? 0, app, branch: run.branch, sha: run.sha.slice(0, 7), n: run.id }),
    run.message,
    t("Resolve it on {branch} itself, keeping what both sides meant: merge the default branch into it (with git, or with merge_default_branch, rendimiento's coding agent's tool) and push; the new commit is tested, and merged if it passes. Say in the handover what you chose in each conflict.", { branch: run.branch }),
  ].join("\n");
  return (
    <div className="card why-failed">
      <h2 style={{ margin: 0 }}>{t("It passed, but it was not merged")}</h2>
      <p className="small" style={{ margin: 0 }}>{t("Pull request #{pull} conflicts with the default branch: both changed the same lines. Resolve it on {branch} and push; the new commit is tested and merged by itself.", { pull: run.pull ?? 0, branch: run.branch })}</p>
      {session.role !== "guest" && (
        <div className="row">
          <button disabled={busy} title={t("Creates an encargo for an agent to take")} onClick={() => {
            setBusy(true);
            // A conflict of code needs git: it goes to the person's own agent, not the platform's.
            api.createEncargo({ title: t("Resolve the conflict of {branch} in {app}", { branch: run.branch, app }), body, app, runId: run.id })
              .then((e) => api.routeEncargo(e.id, "local").catch(() => e))
              .then((e) => navigate(`/ruta?tab=encargos#encargo-${e.id}`), (e) => { setBusy(false); alert(e.message); });
          }}>{t("Ask an agent to resolve it")}</button>
          <span className="small muted">{t("It goes to your own agent, which has git: the platform's agent cannot merge branches.")}</span>
        </div>
      )}
    </div>
  );
}

/**
 * On a failed run: hand it to agents as an encargo, with the failed steps
 * and the run's message as its detail, then go to the encargos.
 */
function AskAgent({ run, app, diagnosis }: { run: Run; app: string; diagnosis?: string }) {
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const failed = run.steps?.filter((s) => s.status === "failed") ?? [];
  const body = [
    t("Run #{n} of {app} failed on {branch} ({sha}).", { n: run.id, app, branch: run.branch, sha: run.sha.slice(0, 7) }),
    ...failed.map((s) => `- ${s.id}: ${s.message || t("failed")}`),
    run.message && !failed.length ? run.message : "",
    diagnosis ? t("The diagnostician's explanation:") + "\n" + diagnosis : "",
    t("Find out why (ejecucion_leer, paso_registro), fix it on a branch, test it there and hand it over with the link."),
  ].filter(Boolean).join("\n");
  return (
    <button disabled={busy} title={t("Creates an encargo for an agent to take")} onClick={() => {
      setBusy(true);
      // A branch with an open encargo gets this one added to it, not a second encargo.
      api.createEncargo({ title: t("Fix run #{n} of {app}", { n: run.id, app }), body, app, runId: run.id })
        .then((e) => navigate(`/ruta?tab=encargos#encargo-${e.id}`), (e) => { setBusy(false); alert(e.message); });
    }}>{t("Ask an agent")}</button>
  );
}

/**
 * «Why did it fail?»: the diagnostician agent's explanation of a failed
 * run, kept fresh while it works; a collaborator asks for it (again).
 */
function WhyFailed({ run, d, setD }: { run: Run; d: Diagnosis | null | undefined; setD: (d: Diagnosis | null) => void }) {
  const [asking, setAsking] = useState(false);
  useEffect(() => {
    let live = true;
    const load = () => api.diagnosis(run.id, lang).then((x) => live && setD(x), () => live && setD(null));
    load();
    const every = setInterval(load, 8000);
    return () => { live = false; clearInterval(every); };
  }, [run.id, setD]);
  const ask = () => { anotar("ask_agent"); setAsking(true); api.askDiagnosis(run.id).then(() => { setAsking(false); setD({ runId: run.id, status: "pending", costMicros: 0, updatedAt: new Date().toISOString() }); }, (e) => { setAsking(false); alert(e.message); }); };
  const canAsk = session.role !== "guest";
  return (
    <div className="card why-failed">
      <div className="row between">
        <h2 style={{ margin: 0 }}>{t("Why did it fail?")}</h2>
        {canAsk && d?.status !== "pending" && <button className="link small" disabled={asking} onClick={ask}>{d ? t("Explain again") : t("Explain")}</button>}
      </div>
      {d === undefined && <p className="muted small">{t("Loading…")}</p>}
      {d === null && <p className="muted small">{t("No explanation yet. If automatic diagnosis is on (AI page), the diagnostician agent writes one within a minute of the failure.")}</p>}
      {d?.status === "pending" && <p className="muted small">{t("The diagnostician agent is reading the logs…")}</p>}
      {d?.status === "done" && d.text && <TextoConFormato text={d.text} className="why-text" />}
      {d?.status === "done" && d.translatedFrom && <p className="small muted" style={{ margin: 0 }}>{d.translatedFrom === "es" ? t("Translated by AI from Spanish.") : t("Translated by AI from English.")}</p>}
      {d?.status === "failed" && <p className="small">{t("It could not explain it: {why}", { why: d.note ?? "" })}</p>}
      {d?.status === "done" && <Judge run={run} d={d} setD={setD} />}
      {d && d.status !== "pending" && d.model && (
        <p className="small muted" style={{ margin: 0 }}>{t("By the diagnostician agent")} · {d.model} · {dollars(d.costMicros)} · {timeAgo(d.updatedAt)}{d.note && d.status === "done" && <> · {d.note}</>}</p>
      )}
    </div>
  );
}

/**
 * Did the explanation help? Either answer becomes a known failure in the
 * Ruta, which the diagnostician reads next time a failure looks alike: that
 * is how it learns.
 */
function Judge({ run, d, setD }: { run: Run; d: Diagnosis; setD: (d: Diagnosis) => void }) {
  const [wrong, setWrong] = useState(false);
  const [cause, setCause] = useState("");
  const [busy, setBusy] = useState(false);
  if (session.role === "guest") return null;
  const send = (useful: boolean) => {
    setBusy(true);
    api.judgeDiagnosis(run.id, useful, cause.trim()).then((r) => {
      setBusy(false); setWrong(false);
      setD({ ...d, feedback: useful ? "useful" : "wrong", feedbackBy: session.role, rutaEntry: r.rutaEntry });
    }, (e) => { setBusy(false); alert(e.message); });
  };
  if (d.feedback && !wrong) {
    return (
      <p className="small judge">
        {d.feedback === "useful" ? t("Marked useful.") : t("Corrected.")}{" "}
        {d.rutaEntry && <Link to="/ruta">{t("Kept in the Ruta as a known failure (#{n}): the diagnostician reads it next time.", { n: d.rutaEntry })}</Link>}{" "}
        <button className="link small" onClick={() => setWrong(true)}>{t("Change")}</button>
      </p>
    );
  }
  return (
    <div className="judge stack">
      {!wrong ? (
        <div className="row" style={{ gap: 8 }}>
          <span className="small muted">{t("Did it help?")}</span>
          <button className="small" disabled={busy} onClick={() => send(true)}>{t("Useful")}</button>
          <button className="small" disabled={busy} onClick={() => setWrong(true)}>{t("Not useful…")}</button>
        </div>
      ) : (
        <>
          <label className="field"><span>{t("What was the real cause, and the fix?")}</span>
            <textarea rows={3} value={cause} maxLength={4000} onChange={(e) => setCause(e.target.value)}
              placeholder={t("e.g. The test's etcd is slow on a busy node: it is transient, run it again. Fixed for good with unsafe-no-fsync.")} />
          </label>
          <div className="row" style={{ gap: 8 }}>
            <button className="primary small" disabled={busy || !cause.trim()} onClick={() => send(false)}>{t("Teach the diagnostician")}</button>
            <button className="link small" onClick={() => setWrong(false)}>{t("Cancel")}</button>
          </div>
        </>
      )}
    </div>
  );
}
