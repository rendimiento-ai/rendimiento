import { Link } from "react-router-dom";
import { api, timeAgo, type App, type Forja, type Release } from "../api";
import { branchURL, forjaOf } from "../forja";
import { RunBadge, usePoll } from "./ui";
import { statusClass, statusLabel } from "./encargos";
import { t } from "../i18n";

/** A branch's name as a link to it on its forja (GitHub, GitLab or Bitbucket), where its files, its commits and its change are. */
export function Rama({ repo, forja, branch, className = "mono" }: { repo: string; forja?: Forja; branch: string; className?: string }) {
  return (
    <a className={className} href={branchURL({ repo, forja }, branch)} target="_blank" rel="noreferrer"
      title={forjaOf({ forja }) === "gitlab" ? t("Open the branch {branch} on GitLab", { branch })
        : forjaOf({ forja }) === "bitbucket" ? t("Open the branch {branch} on Bitbucket", { branch })
        : t("Open the branch {branch} on GitHub", { branch })}
      onClick={(e) => e.stopPropagation()}>{branch}</a>
  );
}

export const verifyBadge: Record<string, [string, string]> = {
  verifying: ["live", "Verifying…"],
  passed: ["ok", "Verified"],
  failed: ["bad", "Failed · rolled back"],
  "failed-kept": ["bad", "Failed verification"],
  superseded: ["", "Not verified: replaced"],
  blocked: ["bad", "Not deployed"],
};

/** What release verification concluded, with its message on hover. */
export function VerifyBadge({ r }: { r: Release }) {
  const b = r.verifyStatus ? verifyBadge[r.verifyStatus] : undefined;
  if (!b) return null;
  return <span className={`badge ${b[0]}`} title={r.verifyMessage}>{t(b[1])}</span>;
}

/**
 * "Lo último": the app's recent runs, releases and encargos on one line of time, newest first, and the day it was born when
 * that is still among them. A new app has a few lines; an old one shows its latest.
 */
export function LoUltimo({ app, n = 6, plain }: { app: App; n?: number; plain?: boolean }) {
  const name = app.name;
  const { data: runs } = usePoll(() => api.runs(name), [name], 15000);
  const { data: rels } = usePoll(() => api.releases(name), [name], 30000);
  const { data: encargos } = usePoll(() => api.encargos().catch(() => []), [], 60000);
  const hechos = [
    ...(runs ?? []).slice(0, n).map((r) => ({
      at: r.createdAt, key: `r${r.id}`, to: `/apps/${name}/runs/${r.id}`,
      tone: r.status === "succeeded" ? "var(--ok)" : r.status === "failed" ? "var(--bad)" : "var(--warn)",
      que: t("Run #{n}", { n: r.id }), detalle: <><Rama repo={app.repo} forja={app.forja} branch={r.branch} /> · {r.message || r.sha.slice(0, 7)}</>, badge: <RunBadge status={r.status} />,
    })),
    ...(rels ?? []).slice(0, 4).map((r) => ({
      at: r.createdAt, key: `v${r.id}`, to: `/apps/${name}/releases`,
      tone: r.rollbackOf ? "var(--warn)" : "var(--grana)",
      que: r.rollbackOf ? t("Rolled back to #{n}", { n: r.rollbackOf }) : t("Release #{n}", { n: r.number }), detalle: <>{r.sha.slice(0, 7)}</>, badge: <VerifyBadge r={r} />,
    })),
    ...(encargos ?? []).filter((e) => e.app === name).slice(0, 4).map((e) => ({
      at: e.updatedAt, key: `e${e.id}`, to: `/ruta?tab=encargos#encargo-${e.id}`,
      tone: "var(--rosa)",
      que: t("encargo {n}", { n: e.id }), detalle: <>{e.title}</>, badge: <span className={`badge ${statusClass[e.status]}`}>{t(statusLabel[e.status])}</span>,
    })),
    { at: app.createdAt, key: "nacio", to: "", tone: "var(--cempasuchil)", que: t("{app} was born", { app: name }), detalle: <>{t("Connected from {repo}", { repo: app.repo })}</>, badge: null },
  ].sort((a, b) => b.at.localeCompare(a.at)).slice(0, n);
  const lista = (
    <ol className="lo-ultimo">
      {hechos.map((h, i) => (
        <li key={h.key} style={{ ["--c" as string]: h.tone, ["--d" as string]: `${i * 60}ms` }}>
          {h.to ? <Link to={h.to}><strong>{h.que}</strong></Link> : <strong>{h.que}</strong>} {h.badge} <span className="muted small">{timeAgo(h.at)}</span>
          <div className="small muted">{h.detalle}</div>
        </li>
      ))}
    </ol>
  );
  if (plain) return lista;
  return (
    <div className="card stack">
      <strong>{t("The latest")}</strong>
      {lista}
    </div>
  );
}
