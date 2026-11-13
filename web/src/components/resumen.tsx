import { useState } from "react";
import { Link } from "react-router-dom";
import { api, session, type DaySummary } from "../api";
import { usePoll } from "./ui";
import { lang, t } from "../i18n";

const HIDE_KEY = "rendimiento.resumen.oculto";

/** The day it was hidden on, so it shows again the next day. */
function hiddenOn(): string {
  try { return localStorage.getItem(HIDE_KEY) ?? ""; } catch { return ""; }
}

/** A day as the reader says it: «Ayer, 12 de noviembre». */
function dayTitle(day: string) {
  const d = new Date(day + "T12:00:00");
  const yesterday = new Date(Date.now() - 864e5);
  const locale = lang === "es" ? "es-MX" : "en";
  const name = d.toLocaleDateString(locale, { day: "numeric", month: "long" });
  return d.toDateString() === yesterday.toDateString() ? t("Yesterday, {day}", { day: name }) : d.toLocaleDateString(locale, { weekday: "long", day: "numeric", month: "long" });
}

/**
 * The day's summary at the top of the home page: a few lines the platform's AI wrote from what was recorded each day since the
 * reader's last visit (at most three days), each about an app they may see. It asks again while a day is still being written.
 */
export function ResumenDelDia() {
  const [hidden, setHidden] = useState(() => hiddenOn() === new Date().toDateString());
  const [redoing, setRedoing] = useState(false);
  const { data, setData } = usePoll(() => api.summary(), [], 20000);
  if (hidden || !data || (data.days.length === 0 && !data.pending)) return null;
  const hide = () => {
    try { localStorage.setItem(HIDE_KEY, new Date().toDateString()); } catch { /* the next visit shows it */ }
    setHidden(true);
  };
  const redo = (d: DaySummary) => {
    setRedoing(true);
    api.redoSummary(d.day).then(() => setData({ ...data, pending: true, days: data.days.filter((x) => x.day !== d.day) }), () => {}).finally(() => setRedoing(false));
  };
  return (
    <section className="card resumen-dia" aria-label={t("The day's summary")}>
      {data.days.map((d) => (
        <div key={d.day}>
          <div className="resumen-dia-fecha">{dayTitle(d.day)}</div>
          <ul>
            {d.lines.map((l, i) => (
              <li key={i}>{l.app && <><Link to={`/apps/${l.app}`}>{l.app}</Link>: </>}{l.text}</li>
            ))}
          </ul>
        </div>
      ))}
      {data.pending && <p className="small muted" style={{ margin: 0 }}>{t("Writing the summary of the last days…")}</p>}
      <div className="resumen-dia-pie small muted">
        <span>{data.days.some((d) => d.source === "ia") ? t("Written by the platform's AI from what was recorded; it can be wrong.") : data.days.some((d) => d.source === "counts") ? t("Only the numbers: no model could write it.") : t("Written from what was recorded.")}</span>
        {session.role !== "guest" && data.days[0] && <button className="link small" disabled={redoing} onClick={() => redo(data.days[0])}>{t("Redo")}</button>}
        <button className="link small" onClick={hide}>{t("Hide for today")}</button>
      </div>
    </section>
  );
}
