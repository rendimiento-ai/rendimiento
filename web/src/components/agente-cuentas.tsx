import { useState } from "react";
import { Link } from "react-router-dom";
import { api, type CausaAgente, type CuentaCausa, type CuentasView } from "../api";
import { ErrorBox, usePoll } from "./ui";
import { locale, t, tn } from "../i18n";
import { Esqueleto, Hueso } from "./esqueleto";

/** The periods offered (0: all of it), as the API takes them (internal/api/ia_cuentas.go). */
const periodos = [7, 30, 90, 0];

/** Why it gave an encargo back, in words (store.Cause*). */
export function causaAgente(c: CausaAgente): string {
  switch (c) {
    case "budget": return t("It reached twice the estimate (its brake)");
    case "needs_decision": return t("It needed a decision of yours");
    case "no_tools": return t("It lacked a tool it needs");
    case "ci_timeout": return t("The run did not end in time");
    case "ai_error": return t("The AI failed");
    case "forge_error": return t("The forja answered with an error");
    case "conflict": return t("It could not merge the default branch (conflicts)");
    case "stopped": return t("A person stopped it");
    case "restart": return t("The platform restarted");
    default: return t("Something else");
  }
}

/** Links to encargos by their number. */
function Encargos({ ids }: { ids: number[] }) {
  return (
    <span className="cuentas-encargos">
      {ids.map((id, i) => (
        <span key={id}>{i > 0 && ", "}<Link to={`/ruta?tab=encargos#encargo-${id}`}>{id}</Link></span>
      ))}
    </span>
  );
}

/**
 * «How the coding agent is doing», on the AI page beside its settings: over a period, what it finished (at the
 * first try or after another), gave back to your agent and why, what you answered to what it delivered, what it
 * spent against the estimate, and the factor the estimator applies today. A warning when it gave back several
 * in a row for the same cause.
 */
export function ComoLeVa() {
  const [dias, setDias] = useState(30);
  const { data, error } = usePoll(() => api.iaCuentas(dias), [dias], 60000);
  const listo = data && data.days === dias ? data : undefined;
  return (
    <div className="cuentas-agente">
      <div className="row between" style={{ gap: 8, flexWrap: "wrap" }}>
        <div className="cab small muted">{t("How the coding agent is doing")}</div>
        <div className="rel-filters" role="group" aria-label={t("Time range")}>
          {periodos.map((d) => (
            <button key={d} type="button" aria-pressed={dias === d} onClick={() => setDias(d)}>
              {d ? tn(d, "Last {n} days", "Last {n} days") : t("All of it")}
            </button>
          ))}
        </div>
      </div>
      <ErrorBox error={error} />
      {!listo && !error && (
        <Esqueleto className="stack">
          <Hueso className="esq-renglon" w="40%" />
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(120px, 1fr))", gap: 8 }}>{[0, 1, 2, 3].map((i) => <Hueso key={i} h={52} r={10} />)}</div>
          <Hueso h={120} r={12} />
        </Esqueleto>
      )}
      {listo && <Cuentas c={listo} />}
    </div>
  );
}

function Cuentas({ c }: { c: CuentasView }) {
  const desde = c.since ? new Date(c.since).toLocaleDateString(locale, { month: "long", day: "numeric" }) : "";
  const cuantos = tn(c.encargos, "{n} encargo", "{n} encargos");
  const head = desde ? t("Since {date} · {count}", { date: desde, count: cuantos }) : t("All of it · {count}", { count: cuantos });
  if (c.encargos === 0) {
    return <p className="small muted">{c.since ? t("It took no encargo in this period.") : t("It has not taken an encargo yet.")}</p>;
  }
  const aprox = [...c.causes, ...c.retried].some((k) => k.approx > 0);
  return (
    <>
      <span className="small muted">{head}</span>
      {c.racha && (
        <p className="cuentas-aviso small" role="status">
          <strong>{t("It gave back {n} in a row for the same cause:", { n: c.racha.n })}</strong> {causaAgente(c.racha.cause)}.{" "}
          {t("Encargos")} <Encargos ids={c.racha.encargos} />.{" "}
          {t("Look into it before sending it more like these.")}
        </p>
      )}
      <div className="boleta-cifras">
        <div>
          <strong>{c.finished}</strong>
          <span className="small">{t("finished by it")}</span>
          <span className="small muted">{t("{first} at the first try, {retry} after another try", { first: c.firstTry, retry: c.afterRetry })}</span>
        </div>
        <div>
          <strong>{c.gaveBack}</strong>
          <span className="small">{t("given back to your agent")}</span>
          {c.working > 0 && <span className="small muted">{tn(c.working, "{n} it has now", "{n} it has now")}</span>}
        </div>
        <div>
          <strong>{c.accepted}</strong>
          <span className="small">{t("you accepted the first time")}</span>
          <span className="small muted">
            {t("{returned} sent back with a comment", { returned: c.returned })}
            {c.toReview > 0 && <> · {tn(c.toReview, "{n} to review", "{n} to review")}</>}
          </span>
        </div>
        <div>
          <strong>{c.all.n ? `×${c.all.x.toFixed(1)}` : "—"}</strong>
          <span className="small">{t("spent against the estimate")}</span>
          <span className="small muted">{c.all.n ? tn(c.all.n, "the middle of {n} it finished", "the middle of {n} it finished") : t("none finished with an estimate")}</span>
        </div>
      </div>

      {c.causes.length > 0 && (
        <div className="stack cuentas-por-que" style={{ gap: 6 }}>
          <strong className="small">{tn(c.gaveBack, "Why it gave back the {n}", "Why it gave back the {n}")}</strong>
          <ul className="cuentas-causas">
            {c.causes.map((k) => <Causa key={k.cause} k={k} />)}
          </ul>
        </div>
      )}
      {c.retried.length > 0 && (
        <p className="small muted" style={{ margin: 0 }}>
          {t("It also gave back, before taking them again:")}{" "}
          {c.retried.map((k, i) => (
            <span key={k.cause}>{i > 0 && "; "}{causaAgente(k.cause)} ({t("encargos")} <Encargos ids={k.encargos} />)</span>
          ))}.
        </p>
      )}
      {aprox && <p className="small muted" style={{ margin: 0 }}>{t("≈ read from the note of a release from before the cause was recorded: approximate.")}</p>}

      <div className="stack" style={{ gap: 4 }}>
        <strong className="small">{t("Spent against the estimate, by difficulty")}</strong>
        <span className="small">
          {c.ratios.length
            ? c.ratios.map((r) => t("{from}–{to}: ×{x} ({n})", { from: r.from, to: r.to, x: r.x.toFixed(1), n: r.n })).join(" · ")
            : t("none finished with an estimate")}
        </span>
        <span className="small muted">{factorHoy(c)}</span>
      </div>
    </>
  );
}

function Causa({ k }: { k: CuentaCausa }) {
  return (
    <li>
      <span className="small">{causaAgente(k.cause)}{k.approx > 0 && <span className="muted" title={t("read from its note: approximate")}> ≈</span>}</span>
      <span className="small muted">{t("encargos")} <Encargos ids={k.encargos} /></span>
      <strong className="small">{k.n}</strong>
    </li>
  );
}

/** The factor the estimator applies today: the numbers of the record above (agentes.Learned), not computed again. */
function factorHoy(c: CuentasView): string {
  if (!c.learnOn) return t("The estimator uses the fixed table: correcting it is off, under its record.");
  const all = c.learned.all;
  if (all.n < c.learnFrom) return t("The estimator learns from {need} finished encargos; it has {n}.", { need: c.learnFrom, n: all.n });
  return t("The estimator applies ×{x} today: what it spent on {n} encargos it finished.", { x: all.x.toFixed(1), n: all.n });
}
