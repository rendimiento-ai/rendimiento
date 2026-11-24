import { useState } from "react";
import { api, type App, type PullOpened, type Run } from "../api";
import { t } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "./conexion-aviso";
import { forjaOf } from "../forja";

/**
 * Beside "Merge this branch": only opening the branch's pull request, with
 * the same title and body, without anyone's yes to merge it. Where the app
 * has previews, the open pull request gets its copy as any other does. One
 * already open is linked, not opened again.
 */
export function SoloAbrir({ run, app }: { run: Run; app: App }) {
  const [busy, setBusy] = useState(false);
  const [abierta, setAbierta] = useState<PullOpened>();
  const conexion = useConexion();
  const gitlab = forjaOf(app) === "gitlab";
  const copia = !!app.spec?.previews;
  // Where any open pull request is the yes, opening it would merge it (unless the run waits for a person anyway).
  if (app.mergeAny && !run.untested?.length && !run.dry?.failed) return null;
  if (abierta) {
    const n = { n: abierta.number };
    return (
      <p className="small" id="pull-abierta" style={{ flexBasis: "100%", margin: 0 }}>
        {gitlab
          ? (abierta.alreadyOpen ? t("Merge request !{n} was already open.", n) : t("Merge request !{n} is open, not merged.", n))
          : (abierta.alreadyOpen ? t("Pull request #{n} was already open.", n) : t("Pull request #{n} is open, not merged.", n))}{" "}
        <a href={abierta.url} target="_blank" rel="noreferrer">{gitlab ? t("See the merge request") : t("See the pull request")}</a>
      </p>
    );
  }
  return (
    <div className="row" style={{ flexBasis: "100%" }}>
      <button id="pull-open" {...apagado(conexion, busy)} onClick={() => {
        setBusy(true);
        api.openPull(run.id).then((r) => { setBusy(false); setAbierta(r); }, (e) => { setBusy(false); alert(e.message); });
      }}>
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" aria-hidden="true">
          <circle cx="6" cy="6" r="2.5" /><circle cx="6" cy="18" r="2.5" /><circle cx="18" cy="18" r="2.5" />
          <path d="M6 8.5v7M18 15.5V10a3 3 0 0 0-3-3h-4M13 4.5 10.5 7 13 9.5" />
        </svg>
        {gitlab ? t("Just open the merge request") : t("Just open the pull request")}
      </button>
      <span className="small muted">
        {gitlab
          ? (copia ? t("“Just open” creates the merge request without merging it, and with it its copy to try.") : t("“Just open” creates the merge request without merging it."))
          : (copia ? t("“Just open” creates the pull request without merging it, and with it its copy to try.") : t("“Just open” creates the pull request without merging it."))}
      </span>
    </div>
  );
}
