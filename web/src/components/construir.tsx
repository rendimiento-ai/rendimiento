// "Build now": the app's default branch, built and deployed if it passes, and beside it any other branch, only
// tested. On the app's head and on its glance; never for a guest, who may not start runs.
import { useState } from "react";
import { api, session, type App } from "../api";
import { usePoll } from "./ui";
import { anotar } from "../uso";
import { t } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "./conexion-aviso";

/** Whether the person reading may start this app's runs: anyone but a guest (the API asks for a collaborator). */
export function puedeConstruir(app: App): boolean {
  return (app.role ?? session.role) !== "guest";
}

/**
 * "Build now", shimmering while the run is being asked for: the default branch, which deploys. Beside it, testing
 * another branch: one of those that ran lately, or any by name; it only builds and tests, and never deploys.
 */
export function BuildNow({ name, main, onDone }: { name: string; main: string; onDone: () => void }) {
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);
  const [branch, setBranch] = useState("");
  const conexion = useConexion();
  const { data: runs } = usePoll(() => (open ? api.runs(name) : Promise.resolve([])), [name, open], 0);
  const recent = [...new Set((runs ?? []).map((r) => r.branch).filter((b) => b && b !== main))].slice(0, 12);
  const go = async (b?: string) => {
    setBusy(true);
    try {
      anotar("build_now");
      await api.triggerRun(name, b);
      setOpen(false);
      setBranch("");
      onDone();
    } catch (e) {
      alert((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const other = branch.trim();
  return (
    <div className="build-now">
      <button className={`primary${busy ? " corre" : ""}`} {...apagado(conexion, busy, t("Builds {branch} and deploys it if it passes", { branch: main }))} onClick={() => go()}>{busy ? t("Starting…") : t("Build now")}</button>
      <button className="build-now-mas" aria-expanded={open} aria-label={t("Test another branch")} {...apagado(conexion, busy)} onClick={() => setOpen(!open)}>▾</button>
      {open && (
        <div className="card build-now-ramas">
          <strong className="small">{t("Test a branch")}</strong>
          <span className="small muted">{t("It builds and tests the branch; it never deploys it.")}</span>
          {recent.map((b) => (
            <button key={b} className="link" {...apagado(conexion, busy)} onClick={() => go(b)}>{b}</button>
          ))}
          <div className="row" style={{ gap: 6 }}>
            <input value={branch} placeholder={t("another branch, by name")} onChange={(e) => setBranch(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && other && go(other)} />
            <button {...apagado(conexion, busy || !other || other === main)} onClick={() => go(other)}>{t("Test it")}</button>
          </div>
        </div>
      )}
    </div>
  );
}
