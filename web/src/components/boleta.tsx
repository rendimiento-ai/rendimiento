import { Link } from "react-router-dom";
import { dollars, type CoderBoleta } from "../api";
import { Switch } from "./ui";
import { t } from "../i18n";

/**
 * What an encargo was estimated at against what the coding agent spent on
 * it, on one scale, with a mark at twice the estimate: where its brake
 * stops it.
 */
export function EstimateBar({ estimate, spent }: { estimate: number; spent: number }) {
  if (!estimate && !spent) return null;
  const scale = Math.max(estimate * 2, spent, 1);
  const pct = (n: number) => `${Math.min(100, (n / scale) * 100)}%`;
  const times = estimate > 0 ? spent / estimate : 0;
  const label = estimate > 0
    ? t("Spent {spent} of {estimate} estimated ({times}×)", { spent: dollars(spent), estimate: dollars(estimate), times: times.toFixed(1) })
    : t("Spent {spent}", { spent: dollars(spent) });
  return (
    <div className="estimado-gastado" role="img" aria-label={label} title={label}>
      <div className="eg-pista">
        <div className="eg-estimado" style={{ width: pct(estimate) }} />
        <div className={`eg-gastado${estimate > 0 && spent > estimate * 2 ? " pasado" : ""}`} style={{ width: pct(spent) }} />
        {estimate > 0 && <span className="eg-freno" style={{ left: pct(estimate * 2) }} />}
      </div>
      <span className="small muted">{label}</span>
    </div>
  );
}

/**
 * The coding agent's record, on the AI page: encargos taken, delivered at
 * the first try, sent back and given back; what it spent against what was
 * estimated; and the factor the estimator learned from it, which it may
 * stop using (the fixed table again).
 */
export function Boleta({ b, learn, onLearn, admin }: { b: CoderBoleta; learn: boolean; onLearn: (on: boolean) => void; admin: boolean }) {
  const all = b.learned.all;
  const byD = Object.entries(b.learned.byDifficulty).filter(([, f]) => f.n >= b.learnFrom).sort(([a], [c]) => Number(a) - Number(c));
  return (
    <div className="boleta">
      <div className="cab small muted">{t("Its record")}</div>
      <div className="boleta-cifras">
        <div><strong>{b.taken}</strong><span className="small muted">{t("encargos taken")}</span></div>
        <div><strong>{b.firstTry}</strong><span className="small muted">{t("delivered at the first try")}</span></div>
        <div><strong>{b.returned}</strong><span className="small muted">{t("sent back by a person")}</span></div>
        <div><strong>{b.gaveBack}</strong><span className="small muted">{t("given back to your agent")}</span></div>
        <div><strong>{dollars(b.spentMicros)}</strong><span className="small muted">{t("spent, of {estimate} estimated", { estimate: dollars(b.estimateMicros) })}</span></div>
      </div>
      <div className="row between" style={{ gap: 8 }}>
        <span className="small">
          {all.n >= b.learnFrom
            ? t("What it really spends is {x}× the price table, from {n} encargos it finished.", { x: all.x.toFixed(1), n: all.n })
            : t("The estimator learns from {need} finished encargos; it has {n}.", { need: b.learnFrom, n: all.n })}
          {byD.length > 0 && <span className="muted"> {byD.map(([d, f]) => t("Difficulty {d}: {x}×", { d, x: f.x.toFixed(1) })).join(" · ")}</span>}
        </span>
        <span className="row small" style={{ gap: 6 }}>
          <Switch checked={learn} disabled={!admin} onChange={onLearn} label={t("Correct the estimates by what it spends")} />
          {t(learn ? "corrects the estimates" : "the fixed table")}
        </span>
      </div>
      <ul className="boleta-lista">
        {b.recent.map((r) => (
          <li key={r.id}>
            <Link className="small" to={`/ruta?tab=encargos#encargo-${r.id}`}><span className="muted">#{r.id}</span> {r.title}</Link>
            {r.difficulty && <span className="small muted"> · {r.difficulty}/10</span>}
            <EstimateBar estimate={r.estimateMicros ?? 0} spent={r.spentMicros} />
          </li>
        ))}
      </ul>
    </div>
  );
}
