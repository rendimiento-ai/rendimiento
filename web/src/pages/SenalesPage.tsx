import { useState } from "react";
import { api, timeAgo, type Instalacion } from "../api";
import { locale, t } from "../i18n";
import { ErrorBox, usePoll } from "../components/ui";
import { Senales } from "../components/senales";

/**
 * The señales de vida, for the operators: this installation's (whether they
 * go, the switch, and the last ones sent, exactly as they went) and, where
 * they are received (rendimiento.ai), every installation heard of, each
 * one markable as the team's own.
 */
export function SenalesPage() {
  const { data } = usePoll(api.senales, [], 60000);
  return (
    <>
      <h1>{t("Señales de vida")}</h1>
      <Senales />
      <section style={{ marginTop: 28 }}>
        <h2>{t("Sent from here")}</h2>
        {data && data.enviadas.length === 0 && <div className="card empty"><p>{t("None sent yet.")}</p></div>}
        <div className="stack">
          {data?.enviadas.map((e) => (
            <details key={e.cuando} className="card">
              <summary>
                <strong title={new Date(e.cuando).toLocaleString(locale)}>{timeAgo(e.cuando)}</strong>{" "}
                <span className="small muted">
                  {t("{apps} apps · {runs} runs · {calls} agent calls", { apps: e.senal.semana.apps, runs: e.senal.semana.ejecuciones, calls: e.senal.semana.llamadas_de_agentes })}
                </span>
              </summary>
              <pre className="mono small" style={{ overflowX: "auto", margin: "8px 0 0" }}>{JSON.stringify(e.senal, null, 2)}</pre>
            </details>
          ))}
        </div>
      </section>
      {data?.resumen && <Recibidas />}
    </>
  );
}

/** Every installation whose señales came here, and the team's-own switch. */
function Recibidas() {
  const { data, error, reload } = usePoll(api.instalaciones, [], 60000);
  const [err, setErr] = useState<unknown>();
  const marcar = (i: Instalacion) => {
    setErr(undefined);
    api.marcarEquipo(i.instalacion, !i.equipo).then(reload, setErr);
  };
  return (
    <section style={{ marginTop: 28 }}>
      <h2>{t("Received from every installation")}</h2>
      <ErrorBox error={err ?? error} />
      {data && data.length === 0 && <div className="card empty"><p>{t("No installation has sent one yet.")}</p></div>}
      {data && data.length > 0 && (
        <div style={{ overflowX: "auto" }}>
          <table className="bitacora small">
            <thead><tr><th>{t("Installation")}</th><th>{t("Last heard")}</th><th>{t("First heard")}</th><th>{t("Where")}</th><th>{t("Version")}</th><th>{t("Apps")}</th><th>{t("Señales")}</th><th>{t("Team")}</th></tr></thead>
            <tbody>
              {data.map((i) => (
                <tr key={i.instalacion}>
                  <td className="mono" title={i.instalacion}>{i.instalacion.slice(0, 8)}</td>
                  <td>{timeAgo(i.ultima)}</td>
                  <td>{timeAgo(i.primera)}</td>
                  <td>{i.ultimo.modo} · {i.ultimo.motor} · {i.ultimo.so}/{i.ultimo.arquitectura}</td>
                  <td className="mono">{i.ultimo.version}</td>
                  <td>{i.ultimo.semana?.apps ?? 0}</td>
                  <td>{i.envios}</td>
                  <td>
                    <button className="small" onClick={() => marcar(i)} title={t("The team's own are counted apart, never in the numbers given out.")}>
                      {i.equipo ? t("Team's · undo") : t("Mark as the team's")}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
