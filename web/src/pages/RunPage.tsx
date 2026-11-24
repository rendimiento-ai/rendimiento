import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, dollars, duration, session, subscribe, timeAgo, type App, type Diagnosis, type FixOffer, type FixRun, type ReleaseTask, type Run, type Step } from "../api";
import { RunBadge } from "../components/ui";
import { Falla, NoExiste } from "../components/falla";
import { lang, t, tn } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "../components/conexion-aviso";
import { TextoConFormato } from "../components/texto";
import { Rama } from "../components/lo-ultimo";
import { forjaOf } from "../forja";
import { Pasos } from "../components/pasos";
import { EsqCosa, EsqRenglones } from "../components/esqueleto";
import { anotar } from "../uso";
import { SoloAbrir } from "../components/solo-abrir";

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
          {run.status === "succeeded" && run.dry?.failed
            ? <span className="badge warn" id="paso-con-aviso">{t("Passed, with a warning")}</span>
            : <RunBadge status={run.status} />}
        </div>
        {(run.status === "running" || run.status === "queued") && (
          <Cancelar id={run.id} />
        )}
        {run.status === "failed" && (
          <div className="row" style={{ gap: 8 }}>
            <Retry run={run} app={app} onRetried={(r) => setRun((cur) => ({ ...cur, ...r }))} />
            <AskAgent run={run} app={name} diagnosis={diagnosis?.status === "done" ? diagnosis.text : undefined} />
          </div>
        )}
      </div>
      <p className="sub">
        <span className="mono">{run.sha.slice(0, 7)}</span> {t("on")} <strong>{app ? <Rama repo={app.repo} forja={app.forja} branch={run.branch} className="" /> : run.branch}</strong> · {t(run.event)}
        {run.deploy ? ` · ${t("deploys on success")}` : ` · ${t("build only")}`} · {timeAgo(run.createdAt)}
        {run.startedAt && ` · ${duration(run.startedAt, run.finishedAt)}`}
        {run.message && ` · ${run.message}`}
        {!!run.retries && ` · ${tn(run.retries, "retried once", "retried {n} times")}`}
      </p>

      {run.status === "failed" && <WhyFailed run={run} d={diagnosis} setD={setDiagnosis} />}
      {run.status === "failed" && !run.deploy && app && run.branch !== app.defaultBranch && <StaleLockfile run={run} app={name} />}
      {run.merge === "conflict" && <Conflict run={run} app={name} />}
      <Untested run={run} app={app} />
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
      <EnSeco run={run} />
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
  const conexion = useConexion();
  if (session.role === "guest" || (run.retries ?? 0) >= (run.maxRetries ?? 2)) return null;
  if (run.deploy && app?.role !== "admin") return null;
  const go = () => {
    setBusy(true);
    anotar("retry");
    api.retryRun(run.id).then((r) => { setBusy(false); onRetried(r); }, (e) => { setBusy(false); alert(e.message); });
  };
  return (
    <button {...apagado(conexion, busy, t("Runs again only what did not pass; what passed keeps its result and its image."))} onClick={go}>
      {busy ? t("Retrying…") : t("Retry what failed")}
    </button>
  );
}

/** Cancel the run (switched off without a connection). */
function Cancelar({ id }: { id: number }) {
  const conexion = useConexion();
  return <button className="danger" {...apagado(conexion)} onClick={() => { anotar("cancel_run"); api.cancelRun(id).catch((e) => alert(e.message)); }}>{t("Cancel")}</button>;
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
 * On a branch's run that passed: what of its change no step builds or
 * tests. rendimiento does not merge it by itself, and its GitHub check asks
 * for a person, so Renovate's automerge waits too.
 */
function Untested({ run, app }: { run: Run; app?: App }) {
  if (!run.untested?.length || run.status !== "succeeded" || run.merge === "merged") return null;
  return (
    <div className="card" id="untested" style={{ borderLeft: "4px solid var(--warn)", marginBottom: 16 }}>
      <h2 style={{ margin: 0 }}>{t("It passed, but no step tested all of it")}</h2>
      <p className="small" style={{ margin: 0 }}>
        {run.approvedBy
          ? t("No step builds or tests these paths; {who} approved merging it anyway:", { who: run.approvedBy })
          : t("No step builds or tests these paths, so a person must merge this: rendimiento does not merge it by itself, and its check in GitHub asks for a person, so Renovate's automerge waits too.")}
      </p>
      <ul className="small mono" style={{ margin: 0 }}>
        {run.untested.map((p) => <li key={p}>{p}</li>)}
      </ul>
      <p className="small muted" style={{ margin: 0 }}>
        {app?.autoMerge && !run.approvedBy
          ? t("Merge this branch below is your yes to that part too. To have it tested, add a step for it to rendimiento.yaml (a task with its path and when: always).")
          : t("To have it tested, add a step for it to rendimiento.yaml (a task with its path and when: always).")}
      </p>
    </div>
  );
}

/** The book's page on previews, where the "Turn on previews" link of a run en seco leads. */
const LIBRO_VISTAS = lang === "es" ? "https://aprende.rendimiento.ai/future/previews/#en-seco" : "https://learn.rendimiento.ai/future/previews/#en-seco";

/**
 * On a branch's run: its post-deploy tasks and tests with dryRun, run en seco, each target in a dashed section of its
 * own: against the release already published (a branch that changes only its checks), and against a copy of the
 * branch (its pull request's, or a short one made for them) when it changes the app or the checks ask for it. It only
 * warns: when one fails, the run passed with a warning, rendimiento does not merge it by itself and Renovate's
 * automerge waits; a person can still merge it.
 */
function EnSeco({ run }: { run: Run }) {
  const d = run.dry;
  if (!d) return null;
  return (
    <>
      {(d.release || d.note) && <EnSecoPublicada run={run} />}
      {d.copy && <EnSecoCopia run={run} />}
    </>
  );
}

/** Whether a dry run's checks did not all pass (a skipped one did not run because another failed). */
const noPasaron = (tareas: ReleaseTask[]) => tareas.some((x) => x.status === "failed" || x.status === "skipped");

/** Against the published release (step 1). */
function EnSecoPublicada({ run }: { run: Run }) {
  const d = run.dry!;
  if (!d.release) {
    return (
      <section className="en-seco" id="en-seco" aria-labelledby="en-seco-titulo">
        <div className="en-seco-cabeza">
          <h2 id="en-seco-titulo">{t("En seco")}</h2>
          <p>{d.note}</p>
        </div>
      </section>
    );
  }
  const tareas = d.tasks ?? [];
  const fallo = noPasaron(tareas);
  const andando = !fallo && (run.status === "running" || tareas.some((x) => x.status === "running"));
  return (
    <section className={`en-seco${fallo ? " bad" : ""}`} id="en-seco" aria-labelledby="en-seco-titulo">
      <div className="en-seco-cabeza">
        <h2 id="en-seco-titulo">
          {fallo ? t("En seco: a check did not pass against the published release #{n}", { n: d.release }) : t("En seco, against the published release #{n}", { n: d.release })}
        </h2>
        <p>
          {fallo
            ? t("This branch changes only the checks, so their new version ran against what is already published. One did not pass: nothing was rolled back and the release was not touched, but rendimiento does not merge it by itself, and Renovate's automerge waits.")
            : t("This branch changes only the checks, so their new version ran against what is already published. It only warns: nothing is rolled back and the release is not touched.")}
        </p>
      </div>
      <Revisiones run={run} tareas={tareas} andando={andando} />
    </section>
  );
}

/** Against a copy of the branch (step 2): its pull request's, or a short one; or why none ran. */
function EnSecoCopia({ run }: { run: Run }) {
  const c = run.dry!.copy!;
  const tareas = c.tasks ?? [];
  const sinCopia = c.kind !== "pull" && c.kind !== "short";
  const ocurrio = tareas.length > 0 || !c.note;
  if (sinCopia || !ocurrio) {
    return (
      <section className={`en-seco${c.failed ? " bad" : ""}`} id="en-seco-copia" aria-labelledby="en-seco-copia-titulo">
        <div className="en-seco-cabeza">
          <h2 id="en-seco-copia-titulo">
            {c.kind === "pull" ? t("En seco, against the copy of pull request #{n}", { n: c.pull ?? 0 })
              : c.kind === "short" ? t("En seco, against a short copy of this branch")
              : t("En seco")}
          </h2>
          <p>{c.note}</p>
          {c.kind === "off" && (
            <p className="en-seco-accion">
              <a className="btn" href={LIBRO_VISTAS} target="_blank" rel="noreferrer">{t("Turn on previews")}</a>
              <span className="small muted">{t("With previews, a branch that changes the app runs its checks en seco against a copy of itself.")}</span>
            </p>
          )}
        </div>
      </section>
    );
  }
  const fallo = !!c.failed;
  const andando = !fallo && (run.status === "running" || tareas.some((x) => x.status === "running"));
  const vive = !!c.url && !c.gone;
  const host = c.url?.replace(/^https?:\/\//, "").replace(/\/$/, "");
  const titulo = c.kind === "pull"
    ? (fallo ? t("En seco: a check did not pass against the copy of pull request #{n}", { n: c.pull ?? 0 }) : t("En seco, against the copy of pull request #{n}", { n: c.pull ?? 0 }))
    : (fallo ? t("En seco: a check did not pass against a short copy of this branch") : t("En seco, against a short copy of this branch"));
  const como = c.kind === "pull"
    ? t("The checks ran against the branch itself, live: rendimiento waited until its pull request's copy ran this commit, and ran them there, with the copy's own database, never production's.")
    : t("The branch has no pull request yet, so rendimiento made a short copy of it only for these checks, with a database of its own, never production's, and deletes it when they end.");
  return (
    <section className={`en-seco${fallo ? " bad" : ""}`} id="en-seco-copia" aria-labelledby="en-seco-copia-titulo">
      <div className="en-seco-cabeza">
        <h2 id="en-seco-copia-titulo">{titulo}</h2>
        {vive && <p className="small"><a href={c.url} target="_blank" rel="noreferrer">{host}</a></p>}
        <p>
          {como}{" "}
          {fallo
            ? t("One did not pass: nothing was rolled back and the release was not touched, but rendimiento does not merge it by itself, and Renovate's automerge waits.")
            : t("It only warns: nothing is rolled back and the release is not touched.")}
        </p>
        {c.note && <p className="small">{c.note}</p>}
      </div>
      <Revisiones run={run} tareas={tareas} andando={andando} copia />
      <p className="en-seco-fila small muted">
        {c.kind === "pull"
          ? (vive ? t("The copy stays live for you to open; it is deleted when the pull request is closed or merged.") : t("The copy was deleted with its pull request."))
          : (vive ? t("The short copy lives only while the checks run.") : t("The short copy was deleted when the checks ended."))}
      </p>
    </section>
  );
}

/** A dry run's checks, each with its log (copia: the ones against the copy). */
function Revisiones({ run, tareas, andando, copia = false }: { run: Run; tareas: ReleaseTask[]; andando: boolean; copia?: boolean }) {
  const [abierto, setAbierto] = useState<string>();
  const [registros, setRegistros] = useState<Record<string, string>>({});
  const ver = (x: ReleaseTask) => {
    if (abierto === x.name) return setAbierto(undefined);
    setAbierto(x.name);
    api.dryLog(run.id, x.name, copia).then((text) => setRegistros((cur) => ({ ...cur, [x.name]: text })), () => {});
  };
  return (
    <>
      {tareas.length === 0 && (
        <p className="en-seco-fila muted">{andando ? (copia ? t("Waiting for the copy to run this commit…") : t("Starting…")) : t("No output yet.")}</p>
      )}
      {tareas.map((x) => (
        <div className="en-seco-fila" key={x.name}>
          <div className="row">
            <span className={`en-seco-marca ${x.status}`} aria-hidden="true">{x.status === "succeeded" ? "✓" : x.status === "failed" ? "✕" : x.status === "skipped" ? "–" : "…"}</span>
            <span style={{ flex: 1 }}>
              <strong>{x.name === "pruebas" ? t("own tests") : x.name}</strong>
              {x.status !== "succeeded" && <> · <RunBadge status={x.status} /></>}
              {x.message && <span className="small muted"> · {x.message}</span>}
            </span>
            <span className="small muted">{duration(x.startedAt, x.finishedAt)}</span>
            {x.status !== "skipped" && <button className="small" aria-expanded={abierto === x.name} onClick={() => ver(x)}>{t("Log")}</button>}
          </div>
          {abierto === x.name && (registros[x.name] === undefined ? <EsqRenglones n={4} /> : <pre className="en-seco-registro">{registros[x.name]}</pre>)}
        </div>
      ))}
    </>
  );
}

/**
 * On a branch's run, in an app that merges by itself: the person's yes to
 * merging this commit, and who gave it. rendimiento opens the pull request
 * when the branch has none, and merges once the run has passed.
 */
function Approve({ run, app, onApproved }: { run: Run; app: App; onApproved: (r: Run) => void }) {
  const [busy, setBusy] = useState(false);
  const conexion = useConexion();
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
      <button id="merge-approve" {...apagado(conexion, busy)} onClick={() => {
        setBusy(true);
        anotar("merge");
        api.approveMerge(run.id).then((r) => { setBusy(false); onApproved({ ...run, ...r, steps: run.steps }); }, (e) => { setBusy(false); alert(e.message); });
      }}>{run.status === "succeeded" && run.dry?.failed ? t("Merge anyway") : t("Merge this branch")}</button>
      <span className="small muted">
        {forjaOf(app) === "gitlab" ? (run.status === "succeeded" && run.dry?.failed
          ? t("A check did not pass en seco. Your yes merges it anyway: rendimiento opens the merge request into {branch} if there is none, merges it, and notes in the Ruta that it was merged despite that.", { branch: app.defaultBranch })
          : run.status === "succeeded"
          ? t("Your yes to this commit: rendimiento opens the merge request into {branch} if there is none, merges it and deletes the branch.", { branch: app.defaultBranch })
          : t("Your yes to this commit: rendimiento opens the merge request into {branch} if there is none, and merges it when this run passes.", { branch: app.defaultBranch }))
          : run.status === "succeeded" && run.dry?.failed
          ? t("A check did not pass en seco. Your yes merges it anyway: rendimiento opens the pull request into {branch} if there is none, merges it, and notes in the Ruta that it was merged despite that.", { branch: app.defaultBranch })
          : run.status === "succeeded"
          ? t("Your yes to this commit: rendimiento opens the pull request into {branch} if there is none, merges it and deletes the branch.", { branch: app.defaultBranch })
          : t("Your yes to this commit: rendimiento opens the pull request into {branch} if there is none, and merges it when this run passes.", { branch: app.defaultBranch })}
      </span>
      <SoloAbrir run={run} app={app} />
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
            // Estimated like any other: a simple conflict is cheap, and the platform's coding agent
            // resolves it with merge_default_branch (by itself when the AI page says what rendimiento opens).
            api.createEncargo({ title: t("Resolve the conflict of {branch} in {app}", { branch: run.branch, app }), body, app, runId: run.id })
              .then((e) => navigate(`/ruta?tab=encargos#encargo-${e.id}`), (e) => { setBusy(false); alert(e.message); });
          }}>{t("Ask an agent to resolve it")}</button>
          <span className="small muted">{t("It is estimated like any encargo: a simple one goes to the platform's coding agent, which merges with merge_default_branch; a hard one to your own agent.")}</span>
        </div>
      )}
    </div>
  );
}

/**
 * A failed step whose log says a lockfile no longer matches its manifest
 * (npm's "not in sync", pnpm's outdated lockfile, a missing go.sum entry):
 * the app's declared fix regenerates it on this branch, in a pod, and a
 * commit with the person as co-author starts a new run; no AI is involved.
 * Or it is handed to the coding agent. Without a declared fix, a branch
 * that declares the recipe is offered, for the person to merge.
 */
function StaleLockfile({ run, app }: { run: Run; app: string }) {
  const navigate = useNavigate();
  const [o, setO] = useState<FixOffer | null>();
  const [busy, setBusy] = useState(false);
  const [added, setAdded] = useState<{ branch: string; link: string }>();
  const [error, setError] = useState("");
  const load = useCallback(() => api.runFix(run.id).then(setO, () => setO(null)), [run.id]);
  useEffect(() => { load(); }, [load]);
  const going = o?.runs.some((f) => f.status === "running") || busy;
  useEffect(() => {
    if (!going) return;
    const every = setInterval(load, 4000);
    return () => clearInterval(every);
  }, [going, load]);
  if (!o || (!o.stale && !o.runs.length)) return null;
  const canAct = session.role !== "guest" && o.available;
  const file = o.stale?.file ?? "";
  const folder = o.folder && o.folder !== "." ? `${o.folder}/` : t("the repository's root");
  const act = (p: Promise<unknown>) => {
    setBusy(true); setError("");
    p.then(() => load(), (e: Error) => setError(e.message)).finally(() => setBusy(false));
  };
  const askAgent = () => {
    if (!o.declared || !o.stale) return;
    setBusy(true);
    const body = [
      t("Run #{n} of {app} failed on {branch} ({sha}).", { n: run.id, app, branch: run.branch, sha: run.sha.slice(0, 7) }),
      `- ${o.step}: ${o.stale.line}`,
      t("{file} no longer matches its manifest. The app declares the fix {fix}: run it with run_fix on the branch, then commit_and_test.", { file, fix: o.declared.name }),
    ].join("\n");
    api.createEncargo({ title: t("Regenerate {file} on {branch}", { file, branch: run.branch }), body, app, runId: run.id })
      .then((e) => navigate(`/ruta?tab=encargos#encargo-${e.id}`), (e: Error) => { setBusy(false); setError(e.message); });
  };
  return (
    <section className="card candado" aria-label={t("Stale lockfile")}>
      {o.stale && <>
        <h2 style={{ margin: 0 }}>{t("It looks like a stale lockfile")}</h2>
        <p className="small" style={{ margin: 0 }}>{t("{step} failed:", { step: o.step ?? "" })}</p>
        <pre className="mono small candado-linea">{o.stale.line}</pre>
        {o.declared ? (
          <p className="small">{t("This app declares {fix} ({how}, in {folder}).", {
            fix: o.declared.name, how: o.declared.recipe ?? t("its own command"), folder })}</p>
        ) : (
          <p className="small">{t("This app declares no fix for {file} in {folder}. With one in rendimiento.yaml, rendimiento regenerates it from this page, and the coding agent with run_fix.", { file, folder })}</p>
        )}
        {canAct && (
          <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
            {o.declared && <>
              <button className="primary" disabled={going} onClick={() => act(api.startFix(run.id))}>{t("Regenerate {file}", { file })}</button>
              <button disabled={busy} onClick={askAgent}>{t("Ask the agent")}</button>
            </>}
            {!o.declared && o.add && !added && (
              <button disabled={busy} onClick={() => { setBusy(true); setError(""); api.addFix(run.id).then(setAdded, (e: Error) => setError(e.message)).finally(() => setBusy(false)); }}>
                {t("Add {recipe} fix to rendimiento.yaml", { recipe: o.add.recipe ?? "" })}
              </button>
            )}
          </div>
        )}
        {added && <p className="small">{t("Branch {branch} declares it. Open its pull request and merge it; then this button regenerates the file.", { branch: added.branch })}{" "}
          <a href={added.link} target="_blank" rel="noreferrer">{t("Open the pull request")}</a></p>}
        {!o.available && <p className="small muted">{t("Fixes cannot run on this platform (they need its build namespace).")}</p>}
        {error && <p className="small bad" role="alert">{error}</p>}
      </>}
      {o.runs.length > 0 && <ul className="candado-arreglos small">{o.runs.map((f) => <FixRunLine key={f.id} run={run} f={f} />)}</ul>}
    </section>
  );
}

/** One fix run for a run: who asked, how it went, and its whole log on demand. */
function FixRunLine({ run, f }: { run: Run; f: FixRun }) {
  const [log, setLog] = useState<string>();
  const who = f.askedKind === "renovate" ? t("by itself, for Renovate") : f.askedKind === "agent" ? t("by the coding agent") : t("by {who}", { who: f.askedBy });
  const files = f.files.join(", ");
  const how = f.status === "running" ? t("Fix #{n} ({fix}) is running…", { n: f.id, fix: f.fix })
    : f.status === "succeeded" ? (f.commitSha
      ? t("Fix #{n} ({fix}) wrote {files} and committed {sha} to {branch}: a new run tests it.", { n: f.id, fix: f.fix, files, sha: f.commitSha.slice(0, 7), branch: f.branch })
      : t("Fix #{n} ({fix}) wrote {files}.", { n: f.id, fix: f.fix, files }))
    : f.status === "unchanged" ? t("Fix #{n} ({fix}) ran, and nothing it may write changed.", { n: f.id, fix: f.fix })
    : t("Fix #{n} ({fix}) failed: {why}", { n: f.id, fix: f.fix, why: f.message ?? "" });
  return (
    <li className={f.status === "failed" ? "bad" : undefined}>
      {how} <span className="muted">{who} · {timeAgo(f.startedAt)}{f.finishedAt && ` · ${duration(f.startedAt, f.finishedAt)}`}{f.image && ` · ${f.image}`}</span>
      {f.ignored.length > 0 && <span className="muted"> · {t("left out: {files}", { files: f.ignored.join(", ") })}</span>}{" "}
      {f.status !== "running" && <button className="link small" onClick={() => log === undefined ? api.fixLog(run.id, f.id).then(setLog, () => setLog("")) : setLog(undefined)}>{log === undefined ? t("Log") : t("Hide the log")}</button>}
      {log !== undefined && <pre className="reg small candado-registro">{log}</pre>}
    </li>
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
      {d === undefined && <EsqRenglones n={3} />}
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
