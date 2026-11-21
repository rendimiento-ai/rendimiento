import { Link } from "react-router-dom";
import { api, session } from "../api";
import { usePoll } from "../components/ui";
import { EsqBloques } from "../components/esqueleto";
import { VisitsPanel } from "../components/visits";
import { DeliveryPanel } from "../components/delivery";
import { t } from "../i18n";

/**
 * Numbers: what used to close the home page. The visitors of the apps (from Umami), and, for the platform's
 * operators, the delivery numbers and the way to how rendimiento's own interface is used. The home page keeps one
 * line of it, with a link here.
 */
export function Numeros() {
  const { data: visitas, error } = usePoll(() => api.visitsSummary(), [], 300000);
  const listo = visitas || error !== undefined ? "" : undefined;
  return (
    <>
      <h1>{t("Numbers")}</h1>
      <p className="sub">{t("How your apps are visited and how often changes reach them. The home page keeps one line of this.")}</p>
      <div className="numeros" data-listo={listo}>
        <section id="visitas" aria-label={t("Visits")}>
          {visitas && visitas.configured && (visitas.apps.length > 0 || visitas.platform)
            ? <VisitsPanel esqueleto />
            : !visitas && error === undefined ? <EsqBloques alturas={[380]} /> : (
              <div className="card">
                <h2 style={{ marginTop: 0 }}>{t("Visitors")}</h2>
                <p className="small muted" style={{ margin: 0 }}>
                  {visitas && !visitas.configured
                    ? t("Visitor numbers are off: rendimiento has no Umami account to read them with.")
                    : t("No visits counted yet: they show here once Umami counts your apps' addresses.")}
                </p>
              </div>
            )}
        </section>
        {session.operator && (
          <section id="entregas" aria-label={t("Delivery")}>
            <DeliveryPanel esqueleto />
          </section>
        )}
        {session.operator && (
          <p className="small muted">
            {t("How people use rendimiento's own pages, as totals without names:")} <Link to="/uso">{t("Use of the interface")} ▸</Link>
          </p>
        )}
      </div>
    </>
  );
}
