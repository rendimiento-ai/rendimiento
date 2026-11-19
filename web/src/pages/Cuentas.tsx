// The person's accounts on the forjas (Ruta 102): who they are in rendimiento, and the GitLab accounts linked to them,
// with which they sign in as themselves (the same roles and the same Ruta).
import { useState } from "react";
import { forjasApi, type Identity } from "../api";
import { ErrorBox, usePoll } from "../components/ui";
import { t } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "../components/conexion-aviso";
import { EsqCosa } from "../components/esqueleto";

/** The Your accounts page, from the account menu. */
export function Cuentas() {
  const conexion = useConexion();
  const { data, error, setData } = usePoll(() => forjasApi.identities(), [], 0);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  if (error) return <ErrorBox error={error} />;
  if (!data) return <EsqCosa />;
  const link = (id: number) => {
    setBusy(true); setErr(undefined);
    forjasApi.link(id).then((r) => { location.href = r.authorizeUrl; }, (e) => { setBusy(false); setErr(e); });
  };
  const unlink = (i: Identity) => {
    if (!confirm(t("Unlink {user} at {host}? Signing in with it will no longer be you.", { user: i.username, host: i.baseUrl }))) return;
    setBusy(true); setErr(undefined);
    forjasApi.unlink(i).then(() => forjasApi.identities()).then((d) => { setBusy(false); setData(d); }, (e) => { setBusy(false); setErr(e); });
  };
  return (
    <>
      <h1 data-listo="">{t("Your accounts")}</h1>
      <p className="sub">{t("You are {login} in rendimiento. Link your account on a GitLab connected here, and signing in with it is signing in as you: the same roles and the same Ruta.", { login: data.login })}</p>
      <ErrorBox error={err} />
      <section className="card stack">
        <h2 style={{ margin: 0 }}>{t("Linked accounts")}</h2>
        {data.identities.length === 0 ? <p className="small muted" style={{ margin: 0 }}>{t("None yet.")}</p> : (
          <ul className="cuentas-ligadas">
            {data.identities.map((i) => (
              <li key={`${i.forja}-${i.baseUrl}-${i.externalId}`}>
                <span className="forja-sello gitlab" aria-hidden="true">GL</span>
                <b>@{i.username}</b> <span className="small muted mono">{i.baseUrl}</span>
                <button className="small" {...apagado(conexion, busy)} onClick={() => unlink(i)}>{t("Unlink")}</button>
              </li>
            ))}
          </ul>
        )}
        {data.gitlab.length > 0 ? (
          <div className="row">
            {data.gitlab.map((g) => (
              <button key={g.id} {...apagado(conexion, busy)} onClick={() => link(g.id)}>{t("Link my account on {host}", { host: g.host })}</button>
            ))}
          </div>
        ) : <p className="small muted" style={{ margin: 0 }}>{t("No GitLab is connected here yet: an operator connects one on the Environment page.")}</p>}
      </section>
    </>
  );
}
