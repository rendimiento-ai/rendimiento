import { useEffect, useState } from "react";
import { api, timeAgo, type BitacoraRow } from "../api";
import { locale, t } from "../i18n";
import { ErrorBox, usePoll } from "../components/ui";

/** How an action ended, as a badge: HTTP 2xx and agents' "ok" are fine, 4xx a refusal, the rest a failure. */
function resultado(r: string): { cls: string; label: string } {
  if (r === "ok" || /^2\d\d$/.test(r)) return { cls: "ok", label: r === "ok" ? t("done") : r };
  if (/^4\d\d$/.test(r)) return { cls: "warn", label: r };
  return { cls: "bad", label: r.length > 40 ? r.slice(0, 40) + "…" : r };
}

/** What an action was on: "app tienda · id 3". */
function sobre(row: BitacoraRow): string {
  return Object.entries(row.sobre ?? {}).map(([k, v]) => `${k} ${v}`).join(" · ");
}

/**
 * The Bitácora: every action of the organization, by people (each call that
 * changed something) and by agents (each tool call), newest first; whether
 * its hash chain holds; filters, more on request, and an export.
 */
export function Bitacora() {
  const [quien, setQuien] = useState("");
  const [accion, setAccion] = useState("");
  const [app, setApp] = useState("");
  const [rows, setRows] = useState<BitacoraRow[]>();
  const [more, setMore] = useState(true);
  const [error, setError] = useState<unknown>();
  const { data: check, error: checkError } = usePoll(api.verificarBitacora, [], 0);
  const { data: apps } = usePoll(api.apps, [], 0);

  useEffect(() => {
    const timer = setTimeout(() => {
      api.bitacora({ quien, accion, app }).then((r) => { setRows(r); setMore(r.length === 100); setError(undefined); }, setError);
    }, 300);
    return () => clearTimeout(timer);
  }, [quien, accion, app]);

  const loadMore = () => {
    if (!rows?.length) return;
    api.bitacora({ quien, accion, app, antes: rows[rows.length - 1].id }).then((r) => { setRows([...rows, ...r]); setMore(r.length === 100); }, setError);
  };

  // The rows shown, one JSON object per line: what an auditor asks for.
  const exportar = () => {
    const blob = new Blob([(rows ?? []).map((r) => JSON.stringify(r)).join("\n") + "\n"], { type: "application/x-ndjson" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `bitacora-${new Date().toISOString().slice(0, 10)}.jsonl`;
    a.click();
    URL.revokeObjectURL(a.href);
  };

  return (
    <>
      <h1>{t("Bitácora")}</h1>
      <p className="sub">{t("Every action here, by people and by agents: who did what, on what, and how it ended. Each row carries the fingerprint of the one before it, so a row changed or removed in the database shows.")}</p>
      <ErrorBox error={error ?? checkError} />
      {check && (
        <div className={`alert ${check.rotaEn ? "" : "info"}`} role="status">
          {check.rotaEn
            ? <><strong>{t("The chain is broken at row #{id}.", { id: check.rotaEn })}</strong> {check.motivo === "this row was changed" ? t("That row was changed after it was written.") : t("The row before it was changed or removed.")}</>
            : <><span className="badge ok">{t("intact")}</span> {t("The chain holds: {n} actions checked.", { n: check.revisados.toLocaleString(locale) })}</>}
        </div>
      )}
      <div className="row" style={{ flexWrap: "wrap", gap: 8, margin: "12px 0" }}>
        <input type="search" value={quien} onChange={(e) => setQuien(e.target.value.trim())} placeholder={t("Who (a login or an agent key)")} aria-label={t("Who")} />
        <input type="search" value={accion} onChange={(e) => setAccion(e.target.value)} placeholder={t("Action (e.g. rollback)")} aria-label={t("Action")} />
        <select value={app} onChange={(e) => setApp(e.target.value)} aria-label={t("App")}>
          <option value="">{t("Every app")}</option>
          {apps?.map((a) => <option key={a.name} value={a.name}>{a.name}</option>)}
        </select>
        <span className="spacer" />
        <button type="button" onClick={exportar} disabled={!rows?.length}>{t("Export (JSONL)")}</button>
      </div>
      {rows && rows.length === 0 && <div className="card empty"><p>{t("Nothing here yet: every change people or agents make will appear.")}</p></div>}
      {rows && rows.length > 0 && (
        <div style={{ overflowX: "auto" }}>
          <table className="bitacora small">
            <thead><tr><th>{t("When")}</th><th>{t("Who")}</th><th>{t("Action")}</th><th>{t("On what")}</th><th>{t("Result")}</th></tr></thead>
            <tbody>
              {rows.map((r) => {
                const res = resultado(r.resultado);
                const args = typeof r.detalle?.argumentos === "string" ? r.detalle.argumentos : "";
                return (
                  <tr key={r.id} title={`#${r.id} · ${r.huella}`}>
                    <td title={new Date(r.cuando).toLocaleString(locale)}>{timeAgo(r.cuando)}</td>
                    <td>{r.quienTipo === "agente" ? t("agent") : r.quienTipo === "plataforma" ? t("platform") : t("person")} · <strong>{r.quien}</strong> <span className="muted">({r.por})</span></td>
                    <td className="mono" title={args}>{r.accion}</td>
                    <td className="mono">{sobre(r)}</td>
                    <td><span className={`badge ${res.cls}`} title={r.resultado}>{res.label}</span></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {rows && more && rows.length > 0 && <p><button type="button" onClick={loadMore}>{t("Show older actions")}</button></p>}
    </>
  );
}
