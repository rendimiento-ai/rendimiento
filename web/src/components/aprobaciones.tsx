import { useState } from "react";
import { Link } from "react-router-dom";
import { api, session, timeAgo, type Aprobacion } from "../api";
import { lang, t, tn } from "../i18n";
import { ErrorBox, usePoll } from "./ui";

/**
 * What agents ask to change in production and wait for a person to decide:
 * each with who asked, why, and Approve or Reject for an administrator.
 * Approved, the platform does it at once in that person's name.
 */
export function Aprobaciones() {
  const { data, error, reload } = usePoll(() => api.aprobaciones(true, lang), [], 15000);
  if (!data || data.length === 0) return <ErrorBox error={error} />;
  return (
    <section className="card stack aprobaciones" aria-label={t("Waiting for your approval")}>
      <h2 style={{ margin: 0 }}>{t("Waiting for your approval")}</h2>
      <ErrorBox error={error} />
      {data.map((a) => <Pedido key={a.id} a={a} onDone={reload} />)}
    </section>
  );
}

function Pedido({ a, onDone }: { a: Aprobacion; onDone: () => void }) {
  const [comentario, setComentario] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [hecha, setHecha] = useState<Aprobacion>();
  const decidir = async (aprobar: boolean) => {
    setBusy(true);
    setError(undefined);
    try {
      setHecha(await api.decidirAprobacion(a.id, aprobar, comentario));
      onDone();
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="stack" style={{ borderTop: "1px solid var(--border)", paddingTop: 10 }}>
      <div>
        <strong>{t("{agent} asks to roll {app} back to release #{n}", { agent: a.pedidaPor, app: a.app, n: a.version })}</strong>
        <span className="small muted"> · {timeAgo(a.creada)}</span>
      </div>
      <div className="small">{t("Why")}: {a.motivo}</div>
      {a.motivoOriginal && (
        <details className="small muted"><summary>{t("As the agent wrote it")}</summary>{a.motivoOriginal}</details>
      )}
      <ErrorBox error={error} />
      {hecha ? (
        <div className={`alert ${hecha.estado === "aprobada" ? "info" : ""}`}>
          {hecha.estado === "aprobada" ? t("Approved: {result}", { result: hecha.resultado ?? "" })
            : hecha.estado === "rechazada" ? t("Rejected.") : t("It failed: {why}", { why: hecha.resultado ?? "" })}
        </div>
      ) : session.role === "admin" ? (
        <div className="row" style={{ flexWrap: "wrap", gap: 8 }}>
          <input value={comentario} onChange={(e) => setComentario(e.target.value)} placeholder={t("A comment (optional)")} aria-label={t("A comment (optional)")} />
          <button className="primary" disabled={busy} onClick={() => decidir(true)}>{t("Approve and roll back")}</button>
          <button disabled={busy} onClick={() => decidir(false)}>{t("Reject")}</button>
        </div>
      ) : (
        <div className="small muted">{t("An administrator decides.")}</div>
      )}
    </div>
  );
}

/** A line on the Apps page when agents wait for a decision. */
export function AprobacionesAviso() {
  const { data } = usePoll(() => api.aprobaciones(true), [], 30000);
  if (!data || data.length === 0) return null;
  return (
    <div className="alert" role="status">
      {tn(data.length, "{n} change to production waits for a person's approval.", "{n} changes to production wait for a person's approval.")} <Link to="/bitacora">{t("Approve or reject")}</Link>
    </div>
  );
}

/** How a request ended, as a badge. */
const estados: Record<Aprobacion["estado"], { cls: string; label: string }> = {
  pendiente: { cls: "warn", label: "waiting" },
  aprobada: { cls: "ok", label: "approved" },
  rechazada: { cls: "bad", label: "rejected" },
  vencida: { cls: "", label: "expired" },
  fallida: { cls: "bad", label: "approved, but it failed" },
};

/**
 * The requests already decided, newest first: who asked, why, who decided,
 * how, with their comment, and what came of it.
 */
export function Decididas() {
  const { data } = usePoll(() => api.aprobaciones(false, lang), [], 30000);
  const decididas = (data ?? []).filter((a) => a.estado !== "pendiente");
  if (decididas.length === 0) return null;
  return (
    <details className="card" style={{ marginTop: 12 }}>
      <summary><strong>{t("Agents' requests already decided")}</strong> <span className="small muted">({decididas.length})</span></summary>
      <div className="stack" style={{ marginTop: 8 }}>
        {decididas.map((a) => {
          const e = estados[a.estado];
          return (
            <div key={a.id} className="stack" style={{ borderTop: "1px solid var(--border)", paddingTop: 8 }}>
              <div>
                <span className={`badge ${e.cls}`}>{t(e.label)}</span>{" "}
                <strong>{t("{agent} asked to roll {app} back to release #{n}", { agent: a.pedidaPor, app: a.app, n: a.version })}</strong>
                <span className="small muted"> · {timeAgo(a.creada)}</span>
              </div>
              <div className="small">{t("Why")}: {a.motivo}</div>
              {a.decididaPor && (
                <div className="small">
                  {t("Decided by {who}", { who: a.decididaPor })}{a.decidida ? ` · ${timeAgo(a.decidida)}` : ""}
                  {a.comentario ? ` · “${a.comentario}”` : ""}{a.resultado ? ` · ${a.resultado}` : ""}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </details>
  );
}
