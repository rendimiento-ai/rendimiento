import { useEffect, useState } from "react";
import { Link, NavLink } from "react-router-dom";
import { envApi, session, timeAgo, type PlatformRelease, type Published } from "../api";
import { usePoll } from "../components/ui";
import { t } from "../i18n";

// The Environment link, marked when a newer rendimiento is ready.
export function EnvironmentNavLink() {
  const { data } = usePoll(() => envApi.version().catch(() => undefined), [], 5 * 60000);
  const ready = data?.status?.available && data.status.latest;
  const deploy = !!data?.status?.manifests?.changed?.length;
  return (
    <NavLink to="/environment" className="nav-link" title={ready ? t("A new version of rendimiento is ready") : deploy ? t("make deploy is needed") : undefined}>
      {t("Environment")}{ready ? <span className="nav-count">{t("new")}</span> : deploy && <span className="nav-count">make deploy</span>}
    </NavLink>
  );
}

const describe = (r: PlatformRelease) => t("release #{n} ({sha}, {when})", { n: r.release, sha: r.sha.slice(0, 7), when: timeAgo(r.at) });

// The version of rendimiento that runs, and the button that moves it to the
// newest image its own repository built and tested.
export function PlatformVersion() {
  const { data, error, setData } = usePoll(envApi.version, [], 60000);
  const [target, setTarget] = useState<number>();
  const [failed, setFailed] = useState<string>();

  // While it restarts the API stops answering; once it answers with the new
  // release, reload the page so the new UI loads too.
  useEffect(() => {
    if (!target) return;
    const id = setInterval(() => {
      envApi.version().then((v) => {
        if (v.status?.current?.release === target) location.reload();
        else setData(v);
      }, () => undefined);
    }, 5000);
    return () => clearInterval(id);
  }, [target, setData]);

  const st = data?.status;
  if ((error && !target) || !data?.enabled || !st) return null;
  const manifests = st.manifests;
  const update = async (n: number) => {
    if (!confirm(t("Update rendimiento to release #{n}? It restarts: for about a minute the page and webhooks do not answer, and runs in progress start again.", { n }))) return;
    setFailed(undefined);
    try {
      await envApi.update(n);
      setTarget(n);
    } catch (e) {
      setFailed(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className={`bando ${st.available ? "warn" : "ok"}`}>
      <span className="bando-titulo">{t("Bando")}</span>
      <strong>{t("rendimiento")}</strong>
      <span className="small">
        {st.current ? t("runs {release}", { release: describe(st.current) }) : t("runs an image built by hand, not from a release")}
      </span>
      {!st.available && <span className="sello puesto">{t("Up to date")} ✓</span>}
      {st.available && st.latest && (
        <>
          <span className="small">{t("{release} is ready", { release: describe(st.latest) })}</span>
          {target ? (
            <span className="badge warn">{t("Updating… the page reloads when the new version answers")}</span>
          ) : (
            <button className="sello" onClick={() => update(st.latest!.release)}>{t("Update")}</button>
          )}
        </>
      )}
      <Link className="small" to={`/apps/${st.app}/releases`}>{t("Releases")}</Link>
      {failed && <span className="small" style={{ color: "var(--bad)" }}>{failed}</span>}
      {manifests?.changed?.length ? (
        <div className="small" style={{ flexBasis: "100%" }}>
          <p style={{ margin: "4px 0" }}>
            <strong>{t("make deploy is needed")}</strong>{" "}
            {st.available
              ? t("This version also changes the platform's manifests, which the Update button does not apply. After updating, run in your copy of rendimiento:")
              : t("The platform's manifests changed since the last make deploy ({sha}), and the Update button does not apply them. Run in your copy of rendimiento:", { sha: (manifests.applied ?? "").slice(0, 7) })}
          </p>
          <pre className="file">git pull && make deploy</pre>
          <span className="muted">{t("Changed:")} <span className="mono">{manifests.changed.join(", ")}</span></span>
        </div>
      ) : manifests && !manifests.applied ? (
        <span className="small muted" style={{ flexBasis: "100%" }}>{t("rendimiento cannot tell yet whether make deploy is needed: it learns which commit was applied the next time make deploy runs.")}</span>
      ) : null}
    </div>
  );
}

/** The first lines of a release's notes, without GitHub's headings and links to every pull request. */
function resumen(notas: string): string[] {
  return notas.split("\n").map((l) => l.trim()).filter((l) => l.startsWith("- ") || l.startsWith("* ")).map((l) => l.slice(2)).slice(0, 5);
}

/**
 * The published rendimiento, for the platform's administrators only (decision 138): the version running here, the newest
 * on GitHub with its notes, and how this install takes it. With `onlyNewer`, it shows only when there is a newer one (the
 * home page); on the Environment page it always shows.
 */
export function Publicada({ onlyNewer = false }: { onlyNewer?: boolean }) {
  const { data } = usePoll(() => (session.operator ? envApi.published() : Promise.resolve(undefined as Published | undefined)), [], 30 * 60000);
  const [busy, setBusy] = useState<string>();
  const [failed, setFailed] = useState<string>();

  // While it restarts it does not answer; once it answers in another version, reload so the new UI loads too.
  useEffect(() => {
    if (!busy || !data) return;
    const before = data.version;
    const id = setInterval(() => {
      envApi.published().then((p) => { if (p.version !== before) location.reload(); }, () => undefined);
    }, 4000);
    return () => clearInterval(id);
  }, [busy, data]);

  if (!session.operator || !data || data.how === "self") return null; // a cluster that builds itself has its own card
  if (onlyNewer && !data.newer && !data.lastChange) return null;
  const l = data.latest;
  const cmd = data.how === "helm"
    ? `helm upgrade rendimiento oci://ghcr.io/rendimiento-ai/charts/rendimiento --version ${l?.version ?? ""} --reuse-values`
    : "curl -fsSL https://rendimiento.ai/instalar | sh";
  const act = (what: string, run: () => Promise<void>) => {
    setFailed(undefined);
    setBusy(what);
    run().catch((e: Error) => { setBusy(undefined); setFailed(e.message); });
  };
  return (
    <div className={`bando ${data.newer ? "warn" : "ok"} publicada`}>
      <span className="bando-titulo">{t("Version")}</span>
      <strong>{t("rendimiento {v}", { v: data.version })}</strong>
      {data.lastChange && (
        <span className="small">
          {data.lastChange.regreso
            ? data.lastChange.quien === "automatic"
              ? t("{a} did not stay up: rendimiento went back from it to {b} by itself", { a: data.lastChange.de, b: data.lastChange.a })
              : t("rendimiento went back from {a} to {b}", { a: data.lastChange.de, b: data.lastChange.a })
            : t("rendimiento was updated from {a} to {b}", { a: data.lastChange.de, b: data.lastChange.a })}
        </span>
      )}
      {data.error && <span className="small muted">{t("Could not ask GitHub which version is published: {why}", { why: data.error })}</span>}
      {l && !data.newer && <span className="sello puesto">{t("Up to date")} ✓</span>}
      {busy && <span className="badge warn">{t("Restarting in the other version… the page reloads when it answers")}</span>}
      {!busy && data.previous && (
        <button className="sello" onClick={() => act("back", () => envApi.goBack())}>{t("Go back to {v}", { v: data.previous })}</button>
      )}
      {l && data.newer && (
        <>
          <span className="small"><a href={l.url} target="_blank" rel="noreferrer">{t("{v} is published", { v: l.version })}</a> · {timeAgo(l.fecha)}</span>
          <span className={`badge ${data.onlyFix ? "ok" : "warn"}`}>{data.onlyFix ? t("fixes") : t("new features")}</span>
          {!l.firmada && <span className="badge bad">{t("not signed: rendimiento will not install it by itself")}</span>}
          {data.canInstall && !busy && (
            <button className="sello" onClick={() => act("install", () => envApi.install(l.version))}>{t("Update to {v}", { v: l.version })}</button>
          )}
          {resumen(l.notas).length > 0 && (
            <ul className="small publicada-notas">{resumen(l.notas).map((n, i) => <li key={i}>{n}</li>)}</ul>
          )}
          {!data.canInstall && (
            <div className="small" style={{ flexBasis: "100%" }}>
              <p style={{ margin: "4px 0" }}>{data.how === "helm" ? t("To update this cluster:") : t("To update this computer (your apps and data stay):")}</p>
              <pre className="file" style={{ whiteSpace: "pre-wrap" }}>{cmd}</pre>
            </div>
          )}
          {data.canInstall && (
            <p className="small" style={{ flexBasis: "100%", margin: 0 }}>{t("Your apps keep running while rendimiento restarts. If the new version does not stay up, rendimiento goes back by itself.")}</p>
          )}
        </>
      )}
      {failed && <span className="small" style={{ color: "var(--bad)", flexBasis: "100%" }}>{failed}</span>}
    </div>
  );
}
