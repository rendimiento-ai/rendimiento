// What was seen of a release when it failed verification (Ruta 184): kept by
// the platform before the rollback removed the release's pods, since the
// cluster forgets its events in about an hour. Under a failed release in the
// Releases tab, and on the Problems page's entry for it: each service's new
// pods with their containers' state and last lines, and the events about them.
import { useEffect, useState } from "react";
import { api, ApiError, timeAgo, type ContainerEvidence, type ReleaseEvidence } from "../api";
import { locale, t, tn } from "../i18n";
import { Esqueleto, Hueso } from "./esqueleto";

/** The section's skeleton while it comes: a line, a pod's card, the events. */
function EsqEvidencia() {
  return (
    <Esqueleto className="evidencia-cuerpo">
      <Hueso w="46%" h={14} />
      <Hueso h={84} r={10} />
      <Hueso w="72%" h={14} />
      <Hueso h={56} r={10} />
    </Esqueleto>
  );
}

/**
 * «What was seen when it failed», opened on demand (or already open): fetched once, with its skeleton meanwhile.
 * Nothing kept (an older release, or more than 30 days ago) says so instead.
 */
export function EvidenciaFallo({ app, release, abierta }: { app: string; release: number; abierta?: boolean }) {
  const [open, setOpen] = useState(!!abierta);
  const [ev, setEv] = useState<ReleaseEvidence>();
  const [error, setError] = useState<string>();
  useEffect(() => {
    if (!open || ev || error) return;
    let vivo = true;
    api.releaseEvidence(app, release).then(
      (e) => vivo && setEv(e),
      (e: unknown) => vivo && setError(e instanceof ApiError && e.status === 404
        ? t("Nothing was kept of this release: it failed before rendimiento kept what it saw, or more than 30 days ago.")
        : (e as Error).message),
    );
    return () => {
      vivo = false;
    };
  }, [open, app, release, ev, error]);
  return (
    <div className="evidencia">
      <button className="chip-btn" aria-expanded={open} onClick={() => setOpen(!open)}>
        {open ? t("Hide what was seen") : t("What was seen when it failed")}
      </button>
      {open && (error ? <p className="small muted">{error}</p> : ev ? <EvidenciaVista ev={ev} /> : <EsqEvidencia />)}
    </div>
  );
}

/** A container's state in words: "waiting: CrashLoopBackOff", its exit code, its restarts and how it last ended. */
function estado(c: ContainerEvidence) {
  const parts = [c.reason ? `${t(c.state)}: ${c.reason}` : t(c.state)];
  if (c.exitCode !== undefined) parts.push(t("exit code {n}", { n: c.exitCode }));
  if (c.restarts > 0) parts.push(tn(c.restarts, "{n} restart", "{n} restarts"));
  if (c.lastExitCode !== undefined) parts.push(t("last ended with code {n}{why}", { n: c.lastExitCode, why: c.lastReason ? ` (${c.lastReason})` : "" }));
  return parts.join(" · ");
}

/** The evidence: each service's pods and containers, then the events, oldest first. */
function EvidenciaVista({ ev }: { ev: ReleaseEvidence }) {
  const events = ev.events ?? [];
  return (
    <div className="evidencia-cuerpo">
      <p className="small muted">
        {ev.where === "local"
          ? t("Seen {when} in this computer's containers, before they were removed.", { when: timeAgo(ev.capturedAt) })
          : t("Seen {when} in the cluster, before the rollback removed the pods.", { when: timeAgo(ev.capturedAt) })}
        {ev.truncated && <> {t("The logs were shortened to fit; their ends are kept.")}</>}
      </p>
      {ev.services.map((s) => (
        <section key={s.name} className="evidencia-servicio">
          <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
            <strong className="mono">{s.name}</strong>
            {s.replicaSet && <span className="small muted">{t("ReplicaSet {name}", { name: s.replicaSet })}</span>}
          </div>
          {s.message && <div className="small" style={{ color: "var(--bad)" }}>{s.message}</div>}
          {s.pods.length === 0 && <div className="small muted">{t("No pods of this release were running.")}</div>}
          {s.pods.map((p) => (
            <div key={p.name} className="evidencia-pod">
              <div className="small">
                <span className="mono">{p.name}</span> · {p.phase}{p.node && <> · {t("node {name}", { name: p.node })}</>}
              </div>
              {p.conditions?.map((c) => <div key={c} className="small muted">{c}</div>)}
              {p.containers.map((c) => (
                <div key={c.name} className="evidencia-contenedor">
                  <div className="row small" style={{ gap: 8, flexWrap: "wrap" }}>
                    <span className="mono">{c.name}</span>
                    <span className={`badge ${c.ready ? "ok" : "bad"}`}>{c.ready ? t("Ready") : t("Not ready")}</span>
                    <span className="muted">{estado(c)}</span>
                  </div>
                  {c.message && <div className="small muted">{c.message}</div>}
                  {c.log && (
                    <details open={!c.ready && !c.previousLog}>
                      <summary className="small">{t("Its last lines")}</summary>
                      <pre className="log evidencia-log">{c.log}</pre>
                    </details>
                  )}
                  {c.previousLog && (
                    <details open={!c.ready}>
                      <summary className="small">{t("Its last lines before it restarted")}</summary>
                      <pre className="log evidencia-log">{c.previousLog}</pre>
                    </details>
                  )}
                </div>
              ))}
            </div>
          ))}
        </section>
      ))}
      {events.length > 0 && (
        <details open={events.some((e) => e.type !== "Normal")}>
          <summary className="small">{tn(events.length, "{n} event about them", "{n} events about them")}</summary>
          <ul className="evidencia-eventos small">
            {events.map((e, i) => (
              <li key={i}>
                <span className="muted">{new Date(e.at).toLocaleTimeString(locale)}</span>{" "}
                <span className={`badge ${e.type === "Normal" ? "" : "warn"}`}>{e.reason}</span>{" "}
                <span className="mono">{e.object}</span>: {e.message}
                {(e.count ?? 0) > 1 && <span className="muted"> ({tn(e.count!, "{n} time", "{n} times")})</span>}
              </li>
            ))}
          </ul>
        </details>
      )}
      {ev.notes?.map((n) => <p key={n} className="small muted">{t("Could not be read: {what}", { what: n })}</p>)}
    </div>
  );
}
