// The lookout (el vigía): once a week it reads the platform's log, its runs
// and the interface's use and proposes a few findings, each with its
// evidence. A person decides each one: an encargo, a note in the Ruta, or
// not now. On the Log page, for the platform's administrators; its switch
// and monthly cap are on the AI page.
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, dollars, timeAgo, type Hallazgo, type VigiaView } from "../api";
import { lang, locale, t, tn } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "./conexion-aviso";
import { ErrorBox, Switch, usePoll } from "./ui";
import { EsqRenglones, Esqueleto, Hueso } from "./esqueleto";

/** The book's page on the lookout, in the reader's language. */
const LIBRO = lang === "es" ? "https://aprende.rendimiento.ai/guide/vigia/" : "https://learn.rendimiento.ai/guide/vigia/";

/** One finding: what it saw, its evidence, and the person's three choices (two for a fact). */
function Tarjeta({ h, onDecidido }: { h: Hallazgo; onDecidido: (h: Hallazgo) => void }) {
  const conexion = useConexion();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const decidir = (que: "encargo" | "ruta" | "descartar") => {
    setBusy(true); setError(undefined);
    api.decidirHallazgo(h.id, que).then((d) => { setBusy(false); onDecidido(d); }, (e) => { setBusy(false); setError(e); });
  };
  return (
    <li className={`vigia-hallazgo vigia-${h.estado}`} data-clave={h.clave}>
      <strong className="vigia-titulo">{h.titulo}</strong>
      <p className="small vigia-evidencia">{h.evidencia}</p>
      {h.estado === "nuevo" ? (
        <div className="row vigia-botones">
          {h.accionable && <button type="button" className="primary" {...apagado(conexion, busy)} onClick={() => decidir("encargo")}>{t("Turn it into an encargo")}</button>}
          <button type="button" {...apagado(conexion, busy)} onClick={() => decidir("ruta")}>{t("Note it in the Ruta")}</button>
          <button type="button" className="ghost" {...apagado(conexion, busy)} onClick={() => decidir("descartar")}>{t("Not now")}</button>
        </div>
      ) : (
        <p className="small muted vigia-decidido">
          {h.estado === "encargo" && h.encargoId !== undefined && <Link to="/ruta?tab=encargos">{t("Encargo #{n}", { n: h.encargoId })}</Link>}
          {h.estado === "ruta" && h.rutaId !== undefined && <Link to="/ruta">{t("Ruta entry {n}", { n: h.rutaId })}</Link>}
          {h.estado === "descartado" && t("Not now")}
          {h.decididoPor && <> · {t("{who} decided, {when}", { who: h.decididoPor, when: timeAgo(h.decididoAt) })}</>}
        </p>
      )}
      <ErrorBox error={error} />
    </li>
  );
}

/** "What the lookout saw": the latest review's findings, on the Log page. */
export function Vigia() {
  const conexion = useConexion();
  const [rapido, setRapido] = useState(false);
  const { data, error, reload, setData } = usePoll(api.vigia, [rapido], rapido ? 3000 : 60000);
  const [pidiendo, setPidiendo] = useState(false);
  const [errPedir, setErrPedir] = useState<unknown>();
  const r = data?.revision;
  const revisando = r?.estado === "revisando";
  // While it reviews, look every 3 seconds; then every minute.
  useEffect(() => { if (data) setRapido(revisando); }, [data, revisando]);

  const revisar = () => {
    setPidiendo(true); setErrPedir(undefined);
    api.revisarVigia().then(() => { setPidiendo(false); setRapido(true); reload(); }, (e) => { setPidiendo(false); setErrPedir(e); });
  };
  const decidido = (h: Hallazgo) => {
    if (!data?.revision) return;
    setData({ ...data, revision: { ...data.revision, hallazgos: data.revision.hallazgos.map((x) => (x.id === h.id ? h : x)) } });
  };

  const visibles = r?.hallazgos.filter((h) => h.estado !== "descartado") ?? [];
  const apartados = (r?.hallazgos.length ?? 0) - visibles.length;
  return (
    <section className="card vigia" aria-labelledby="vigia-titulo">
      <div className="row between vigia-cabeza">
        <h2 id="vigia-titulo">{t("What the lookout saw")}</h2>
        {data?.disponible && (
          <button type="button" {...apagado(conexion, pidiendo || revisando)} onClick={revisar}>
            {revisando ? t("Reviewing…") : t("Review now")}
          </button>
        )}
      </div>
      <p className="small muted vigia-sub">
        {t("Once a week it reads this log, the runs and how the interface is used, and proposes a few things worth your time, each with its evidence. It proposes; you decide: nothing becomes an encargo or enters the Ruta without your yes, and what you set aside is not proposed again.")}{" "}
        <a href={LIBRO} target="_blank" rel="noreferrer">{t("How it works")}</a>
      </p>
      <ErrorBox error={error ?? errPedir} />
      {!data && !error && <EsqRenglones n={2} />}
      {data && !r && <p className="small">{data.ajuste.encendido ? t("It has not reviewed yet: its first review comes this week, or ask for one now.") : t("It is off: turn it on on the AI page, or ask for a review now.")}</p>}
      {r && (
        <p className="small muted vigia-cuando">
          {revisando
            ? t("Reviewing the week since {date}…", { date: new Date(r.desde).toLocaleDateString(locale) })
            : t("Reviewed {when}, the week since {date}", { when: timeAgo(r.at), date: new Date(r.desde).toLocaleDateString(locale) })}
          {r.pedidaPor && <> · {t("asked by {who}", { who: r.pedidaPor })}</>}
          {!revisando && <> · {tn(r.senales, "{n} signal read", "{n} signals read")} · {r.proveedor === "local" || !r.costoMicros ? t("free") : dollars(r.costoMicros)}</>}
        </p>
      )}
      {r?.estado === "fallida" && <p className="small"><span className="badge warn">{t("It could not review")}</span> {r.nota}</p>}
      {r?.estado === "hecha" && visibles.length === 0 && <p className="small vigia-nada">{apartados > 0 ? t("You decided everything it saw.") : r.nota || t("Nothing to tell this week.")}</p>}
      {visibles.length > 0 && (
        <ul className="vigia-lista">
          {visibles.map((h) => <Tarjeta key={h.id} h={h} onDecidido={decidido} />)}
        </ul>
      )}
      {apartados > 0 && <p className="small muted">{tn(apartados, "{n} set aside: it will not propose it again.", "{n} set aside: it will not propose them again.")}</p>}
    </section>
  );
}

/** On the AI page, for the platform's operators: the lookout's switch, its monthly cap and what it spent. */
export function VigiaIA() {
  const conexion = useConexion();
  const { data, error, setData } = usePoll(api.vigia, [], 0);
  const [tope, setTope] = useState<number>();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  if (!data) return error ? <ErrorBox error={error} /> : <Esqueleto><Hueso h={170} r={14} style={{ marginTop: 16 }} /></Esqueleto>;
  const a = data.ajuste;
  const valor = tope ?? a.topeMicros / 1e6;
  const guardar = (encendido: boolean, dolares: number) => {
    setBusy(true); setErr(undefined);
    api.putVigia(encendido, dolares).then((v: VigiaView) => { setBusy(false); setData(v); setTope(undefined); }, (e) => { setBusy(false); setErr(e); });
  };
  const pct = a.topeMicros > 0 ? Math.min(100, Math.round((data.gastadoMicros / a.topeMicros) * 100)) : 100;
  return (
    <div className="card stack vigia-ia" style={{ marginTop: 16 }}>
      <div className="row between">
        <h2 style={{ margin: 0 }}>{t("The lookout")}</h2>
        <Switch checked={a.encendido} disabled={busy || conexion.sin} label={t("Review every week")} onChange={(on) => guardar(on, valor)} />
      </div>
      <p className="small muted" style={{ margin: 0 }}>
        {t("Once a week it reads the platform's log, its runs and the interface's use, and proposes a few findings on the Log page. It sends the model counts, times and the log's messages, never what people wrote, a repository's contents or anything that looks like a secret. A review costs about a cent with the platform's model, nothing with the local one.")}{" "}
        <Link to="/registro">{t("Its findings")}</Link>
      </p>
      <div className="ia-meter">
        <div className="row between small"><span>{t("This month")}</span><span className="muted">{t("{used} of {total}", { used: dollars(data.gastadoMicros), total: dollars(a.topeMicros) })}</span></div>
        <div className="meter" role="meter" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={t("The lookout this month")}>
          <div style={{ width: `${pct}%`, background: pct >= 90 ? "var(--bad)" : pct >= 75 ? "var(--warn)" : "var(--ok)" }} />
        </div>
      </div>
      <label className="small row">
        {t("At most, a month (dollars)")}{" "}
        <input type="number" min={0} max={100} step={0.05} value={valor} style={{ width: 90 }} onChange={(e) => setTope(Number(e.target.value))} />
        {tope !== undefined && tope !== a.topeMicros / 1e6 && (
          <button type="button" className="primary" {...apagado(conexion, busy)} onClick={() => guardar(a.encendido, tope)}>{t("Save")}</button>
        )}
      </label>
      <p className="small muted" style={{ margin: 0 }}>{t("When the month's cap is spent it waits for the next month; a review asked for on the Log page counts too.")}</p>
      {a.updatedBy && <p className="small muted" style={{ margin: 0 }}>{t("Last changed by {who}, {when}.", { who: a.updatedBy, when: timeAgo(a.updatedAt) })}</p>}
      <ErrorBox error={err} />
    </div>
  );
}
