import { useState } from "react";
import { Link } from "react-router-dom";
import { api, timeAgo } from "../api";
import { lang, t } from "../i18n";
import { ErrorBox, usePoll } from "./ui";

/** The book's page on the señales, in the reader's language. */
const LIBRO = lang === "es" ? "https://aprende.rendimiento.ai/guide/senales/" : "https://learn.rendimiento.ai/guide/senales/";

/**
 * What this installation sends rendimiento.ai once a week, exactly as it
 * would go, whether it goes, and the switch; and, where señales are
 * received (rendimiento.ai), how many installations are alive. For the
 * platform's operators.
 */
export function Senales() {
  const { data, error, setData } = usePoll(api.senales, [], 60000);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  if (!data) return error ? null : <ErrorBox error={error} />;
  const cambiar = async (encendidas: boolean) => {
    setBusy(true);
    setErr(undefined);
    try { setData(await api.putSenales({ encendidas, avisoVisto: true })); } catch (e) { setErr(e); } finally { setBusy(false); }
  };
  return (
    <section id="senales" style={{ marginTop: 28 }}>
      <h2>{t("What rendimiento sends")}</h2>
      <p className="sub">{t("Once a week, this installation tells rendimiento.ai it is alive, with a few anonymous counts, so the project knows how many installations there are. Never a name, a repository, a person or a secret, and rendimiento.ai keeps no address.")}{" "}
        <a href={LIBRO} target="_blank" rel="noreferrer">{t("More in the book")}</a>{" · "}<Link to="/senales">{t("Every señal sent and received")}</Link></p>
      <ErrorBox error={err} />
      <div className="card stack">
        <div className="row between" style={{ flexWrap: "wrap", gap: 8 }}>
          <span>
            <span className={`badge ${data.encendidas ? "ok" : ""}`}>{data.encendidas ? t("on") : t("off")}</span>{" "}
            {data.encendidas
              ? data.ultimoEnvio ? t("Last sent {when}.", { when: timeAgo(data.ultimoEnvio) }) : t("The first goes in a few minutes.")
              : data.porque === "SENALES=off" ? t("Turned off with SENALES=off.") : t("Turned off here.")}
          </span>
          {data.porque !== "SENALES=off" && (
            <button disabled={busy} onClick={() => cambiar(!data.encendidas)}>{data.encendidas ? t("Turn off") : t("Turn on")}</button>
          )}
        </div>
        <div className="small muted">{t("The next one, exactly as it would go:")}</div>
        <pre className="mono small" style={{ overflowX: "auto", margin: 0 }}>{JSON.stringify(data.proxima, null, 2)}</pre>
      </div>
      {data.resumen && (
        <div className="card stack" style={{ marginTop: 12 }}>
          <strong>{t("Installations alive (outside the team)")}</strong>
          <div>{t("{a} in the last 7 days · {b} in the last 28 · {c} of the team's own, apart", { a: data.resumen.activas7, b: data.resumen.activas28, c: data.resumen.equipo28 })}</div>
          <div className="small muted">
            {t("Their apps: {n}", { n: data.resumen.appsEnFuera28 })} ·{" "}
            {Object.entries(data.resumen.porModo).map(([k, v]) => `${k} ${v}`).join(", ") || "—"} ·{" "}
            {Object.entries(data.resumen.porMotor).map(([k, v]) => `${k} ${v}`).join(", ") || "—"} ·{" "}
            {Object.entries(data.resumen.porVersion).map(([k, v]) => `${k} ${v}`).join(", ") || "—"}
          </div>
        </div>
      )}
    </section>
  );
}

/**
 * The notice on the Apps page, for the operators, until one of them has
 * seen it: that the señales go, what they are, and how to turn them off.
 */
export function SenalesAviso() {
  const { data, setData } = usePoll(api.senales, [], 0);
  const [busy, setBusy] = useState(false);
  if (!data || data.avisoVisto || !data.encendidas) return null;
  const marcar = async (cambio: { encendidas?: boolean; avisoVisto?: boolean }) => {
    setBusy(true);
    try { setData(await api.putSenales(cambio)); } finally { setBusy(false); }
  };
  return (
    <div className="alert info" role="status">
      <strong>{t("rendimiento sends a señal de vida once a week.")}</strong>{" "}
      {t("A few anonymous counts (how many apps, runs and agents), so the project knows how many installations are alive. Never a name, a repository or a secret.")}{" "}
      <Link to="/senales">{t("See exactly what")}</Link>
      <div className="row" style={{ gap: 8, marginTop: 8 }}>
        <button className="primary" disabled={busy} onClick={() => marcar({ avisoVisto: true })}>{t("Fine")}</button>
        <button disabled={busy} onClick={() => marcar({ encendidas: false, avisoVisto: true })}>{t("Turn it off")}</button>
      </div>
    </div>
  );
}
