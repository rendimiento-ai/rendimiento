import { useState, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { envApi, session, timeAgo, type AjusteProbado, type AjusteResultado, type Llave, type LlaveResultado } from "../api";
import { ErrorBox, usePoll } from "../components/ui";
import { locale, t, tn } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "../components/conexion-aviso";
import { EsqCosa } from "../components/esqueleto";
import { Icono, PapelPicado, type Dibujo } from "./Environment";

/** What the page says of each key: its drawing, its name and what it is for. */
const QUE: Record<Llave["id"], { dibujo: Dibujo; color: string; nombre: () => string; para: () => string }> = {
  cloudflare: { dibujo: "nube", color: "var(--cempasuchil)", nombre: () => "Cloudflare", para: () => t("The apps' DNS and certificates") },
  correo: { dibujo: "sobre", color: "var(--rosa)", nombre: () => t("Email (Resend)"), para: () => t("Notices of failures and outages") },
  ia: { dibujo: "chispa", color: "#7B2F8E", nombre: () => t("Platform AI"), para: () => t("Diagnosis and the coding agent") },
  r2: { dibujo: "respaldo", color: "#1F8A8A", nombre: () => t("Off-site backups (R2)"), para: () => t("Access key and secret") },
  registros: { dibujo: "archivo", color: "#6B5444", nombre: () => t("Log archive (S3)"), para: () => t("The old logs of the steps") },
  umami: { dibujo: "grafica", color: "var(--rosa)", nombre: () => t("Visits (Umami)"), para: () => t("A read-only user") },
};

/** The settings tested before they change: their name, what happens to what already exists, and what the test does. */
const AJUSTES: Record<AjusteProbado["id"], { nombre: () => string; existe: () => string; prueba: () => string[] }> = {
  registro: {
    nombre: () => t("Image registry"),
    existe: () => t("The apps keep their images of today. New builds go to the new registry, starting with no cache; each app moves at its next release."),
    prueba: () => [t("It answers the registry API"), t("BuildKit pushes a test image to it"), t("Every node pulls it"), t("The test image is deleted")],
  },
  dns: {
    nombre: () => t("DNS zone and target"),
    existe: () => t("The apps' records point to the new target at their next check; the zone is where new apps get their address."),
    prueba: () => [t("The Cloudflare token in use sees the zone"), t("It creates and deletes a test record"), t("The target is an IP, resolves, or is auto")],
  },
  emisor: {
    nombre: () => t("Certificate issuer"),
    existe: () => t("The apps' certificates are renewed by the new issuer; until then the ones of today keep working."),
    prueba: () => [t("The ClusterIssuer exists"), t("cert-manager says it is ready")],
  },
  almacenamiento: {
    nombre: () => t("Storage"),
    existe: () => t("Volumes that exist keep their storage class (it cannot change); new volumes use the new one."),
    prueba: () => [t("The StorageClass exists"), t("A small test volume binds, and is deleted")],
  },
  entrada: {
    nombre: () => t("Entrance (ingress class)"),
    existe: () => t("Every app's entrance moves to the new controller: for a moment, an app may not answer."),
    prueba: () => [t("The IngressClass exists"), t("Its controller is running")],
  },
  direccion: {
    nombre: () => t("Public address"),
    existe: () => t("rendimiento answers at the new address. Change it too in the forjas' apps (sign-in and webhooks), or people cannot sign in."),
    prueba: () => [t("The address resolves"), t("It answers over HTTPS as this platform")],
  },
};

const fecha = (iso: string) => new Date(iso).toLocaleDateString(locale, { day: "numeric", month: "short", year: "numeric" });

/** When a key expires, or when it was changed here, in a few words. */
function estadoDe(l: Llave): { texto: string; aviso?: boolean } {
  if (l.externa) return { texto: t("Outside rendimiento") };
  if (!l.set) return { texto: t("Not set") };
  if (l.venceEn) {
    const d = Math.floor((new Date(l.venceEn).getTime() - Date.now()) / 86400000);
    if (d < 0) return { texto: t("Expired"), aviso: true };
    return { texto: tn(d, "Expires in {n} day", "Expires in {n} days"), aviso: d < 30 };
  }
  if (l.cambiadaEn) return { texto: t("Changed {when}", { when: timeAgo(l.cambiadaEn) }) };
  return { texto: t("Does not expire") };
}

/**
 * Keys and settings: every key rendimiento talks to other services with (never its value), and the settings it runs with.
 * A key changes here only after it passes its connection test; if it does not, the one before keeps working.
 */
export function Llaves() {
  const { data, error, setData } = usePoll(() => (session.operator ? envApi.llaves() : Promise.resolve(undefined)), [], 60000);
  const [params, setParams] = useSearchParams();
  const sel = params.get("llave") ?? undefined;
  const selAjuste = params.get("ajuste") ?? undefined;
  const pick = (id?: string) => setParams(id ? { llave: id } : {}, { replace: true });
  const pickAjuste = (id?: string) => setParams(id ? { ajuste: id } : {}, { replace: true });

  if (!session.operator) {
    return (
      <>
        <h1 data-listo="">{t("Keys and settings")}</h1>
        <p>{t("Only the platform's administrators see its keys.")}</p>
      </>
    );
  }
  if (error) return <ErrorBox error={error} />;
  if (!data) return <EsqCosa />;
  const elegida = data.llaves.find((l) => l.id === sel);
  const ajuste = data.ajustes.find((a) => a.id === selAjuste);
  const recargar = () => envApi.llaves().then(setData, () => undefined);

  return (
    <div className="entorno">
      <PapelPicado n={10} />
      <div className="llaves-cabeza">
        <Link to="/environment" className="small" style={{ fontWeight: 800 }}>← {t("Environment")}</Link>
        <h1 data-listo="">{t("Keys and settings")}</h1>
        <p className="sub" style={{ margin: 0 }}>{t("Everything rendimiento uses to talk to other services. Nothing changes without passing its connection test first; if it does not pass, what was there stays.")}</p>
      </div>
      <div className="llaves-pagina">
        <div className="stack-sm">
          <section className="stack-sm" aria-labelledby="llaves-lista">
            <h2 id="llaves-lista" className="entorno-h2">{t("Keys")}</h2>
            <ul className="llaves-lista">
              {data.llaves.map((l) => {
                const q = QUE[l.id];
                const e = estadoDe(l);
                return (
                  <li key={l.id} className={`llave-fila${sel === l.id ? " on" : ""}`}>
                    <Icono d={q.dibujo} color={q.color} size={40} />
                    <span className="llave-texto"><strong>{q.nombre()}</strong><span className="small muted">{q.para()}</span></span>
                    <span className={`llave-estado small${e.aviso ? " aviso" : ""}`}>{e.aviso && <i className="punto" />}{e.texto}</span>
                    <button className={`pildora${sel === l.id ? " primary" : ""}`} aria-pressed={sel === l.id} onClick={() => pick(sel === l.id ? undefined : l.id)}>
                      {l.externa ? t("See") : sel === l.id ? t("Changing") : t("Change")}
                    </button>
                  </li>
                );
              })}
            </ul>
          </section>
          <section className="stack-sm" aria-labelledby="llaves-ajustes">
            <h2 id="llaves-ajustes" className="entorno-h2">{t("Settings tested before they change")}</h2>
            <div className="ajustes-probados">
              {data.ajustes.map((a) => (
                <button key={a.id} type="button" className={`ajuste-probado${selAjuste === a.id ? " on" : ""}`} aria-pressed={selAjuste === a.id}
                  onClick={() => pickAjuste(selAjuste === a.id ? undefined : a.id)}>
                  <span><strong>{AJUSTES[a.id].nombre()}</strong>{a.guardado && <> <span className="badge">{t("Changed here")}</span></>}</span>
                  <span className="small mono">{a.valores.map((v) => v.value || "—").join(" → ")}</span>
                </button>
              ))}
            </div>
            <p className="small muted" style={{ margin: 0 }}>
              {t("Only with Helm, because without them the platform does not start: the database, the encryption key and the mode.")}{" "}
              <span className="mono">{data.helm.join(", ")}</span>
            </p>
          </section>
        </div>
        {ajuste ? <CambiarAjuste key={ajuste.id} a={ajuste} onDone={recargar} />
          : elegida ? <Cambiar key={elegida.id} l={elegida} onDone={recargar} />
          : <section className="cambiar-panel vacio"><p className="small muted" style={{ margin: 0 }}>{t("Choose a key to change it. Its value is never shown: only whether it is set, when it expires and when it was changed.")}</p></section>}
      </div>
    </div>
  );
}

/** How to make the new key, and what its test does, by key; usuario: the label and placeholder of its first value, for a key of two. */
function instrucciones(l: Llave): { pasos: ReactNode[]; prueba: string[]; guarda: string; etiqueta: string; placeholder: string; usuario?: { etiqueta: string; placeholder: string } } {
  switch (l.id) {
    case "registros":
      return {
        pasos: [
          t("In your S3 storage (Garage, MinIO, R2…), make an access key that may read, write and delete in the logs' bucket, and only in it."),
          t("Paste its id and its secret here:"),
        ],
        prueba: [t("The archive accepts it"), t("The bucket is there"), t("It writes, reads back and deletes a test object")],
        guarda: t("Only if everything passes is it saved for the log archive, and used with no restart."),
        usuario: { etiqueta: t("Access key id"), placeholder: t("Paste the id") },
        etiqueta: t("Secret key"),
        placeholder: t("Paste the secret"),
      };
    case "umami":
      return {
        pasos: [
          t("In Umami, under Settings → Users, make a user with the «View only» role, in the team that owns your websites."),
          t("Write its name and paste its password here:"),
        ],
        prueba: [t("Umami lets it in"), t("It sees your websites")],
        guarda: t("Only if both pass is it saved for the visitor numbers, and used with no restart."),
        usuario: { etiqueta: t("User name"), placeholder: t("Write the user name") },
        etiqueta: t("Password"),
        placeholder: t("Paste the password"),
      };
    case "cloudflare":
      return {
        pasos: [
          <>{t("In Cloudflare, open")} <a href="https://dash.cloudflare.com/profile/api-tokens" target="_blank" rel="noreferrer">My Profile → API Tokens → Create Token</a>{t(", with the «Edit zone DNS» template.")}</>,
          <>{t("Permissions")} <code>Zone · DNS · Edit</code> {t("and")} <code>Zone · Zone · Read</code>{t(", on all your zones. You are told 30 days before it expires.")}</>,
          t("Paste it here:"),
        ],
        prueba: [t("Cloudflare accepts it"), l.zonas ? tn(l.zonas, "It reads your {n} zone", "It reads your {n} zones") : t("It sees the zones your apps use"), t("It creates and deletes a test record")],
        guarda: t("Only if everything passes is it saved for the DNS and the certificates, and used with no restart."),
        etiqueta: t("New token"),
        placeholder: t("Paste the token"),
      };
    case "correo":
      return {
        pasos: [
          <>{t("In Resend, open")} <a href="https://resend.com/api-keys" target="_blank" rel="noreferrer">API Keys → Create API Key</a>{t(", with sending access to the domain your notices come from.")}</>,
          t("Paste it here:"),
        ],
        prueba: [t("Resend accepts it (nothing is sent)")],
        guarda: t("Only if it passes is it saved for the notices, and used with no restart."),
        etiqueta: t("New key"),
        placeholder: t("Paste the key (re_…)"),
      };
    default:
      return {
        pasos: [
          <>{t("In Anthropic's console, open")} <a href="https://console.anthropic.com/settings/keys" target="_blank" rel="noreferrer">Settings → API Keys → Create Key</a>.</>,
          t("Paste it here:"),
        ],
        prueba: [t("Anthropic accepts it, for the platform's model")],
        guarda: t("Only if it passes is it saved for the AI gateway, and used with no restart."),
        etiqueta: t("New key"),
        placeholder: t("Paste the key (sk-ant-…)"),
      };
  }
}

/** A step of the test, in words, by key. */
function nombrePaso(llave: string, id: LlaveResultado["pasos"][number]["id"]): string {
  switch (id) {
    case "verify":
      switch (llave) {
        case "cloudflare": return t("Cloudflare accepts it");
        case "correo": return t("Resend accepts it");
        case "registros": return t("The archive accepts it");
        case "umami": return t("Umami lets it in");
        default: return t("Anthropic accepts it");
      }
    case "bucket":
      return t("The bucket is there");
    case "object":
      return t("It wrote, read back and deleted a test object");
    case "websites":
      return t("It sees your websites");
    case "zones":
      return t("It sees the zones");
    case "record":
      return t("It created and deleted a test record");
    case "send":
      return t("A test email went out");
    case "save":
      return llave === "cloudflare" ? t("Saved for the DNS and the certificates") : t("Saved where it is used");
    default:
      return t("In use, with no restart");
  }
}

/**
 * Changing one key: how to make the new one, where to paste it, and what its test does; then how it went. The value lives
 * only in the field until it is sent, and the field is emptied as soon as it is.
 */
function Cambiar({ l, onDone }: { l: Llave; onDone: () => void }) {
  const conexion = useConexion();
  const [valor, setValor] = useState("");
  const [usuario, setUsuario] = useState("");
  const [prueba, setPrueba] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const [res, setRes] = useState<LlaveResultado>();
  const q = QUE[l.id];

  if (!l.cambiable) {
    return (
      <section className="cambiar-panel">
        <span className="cambiar-sello" aria-hidden="true">{t("Key")}</span>
        <h2 className="entorno-h2">{l.externa ? q.nombre() : t("Change: {what}", { what: q.nombre() })}</h2>
        <p style={{ margin: 0 }}>{l.reason}</p>
        {l.externa && <p className="small muted" style={{ margin: 0 }}>{t("rendimiento does not write secrets it does not own: what uses this key keeps it, and changes it.")}</p>}
      </section>
    );
  }
  const i = instrucciones(l);
  const falta = !valor.trim() || (!!i.usuario && !usuario.trim());
  const enviar = async (e: React.FormEvent) => {
    e.preventDefault();
    if (falta) return;
    setBusy(true);
    setErr(undefined);
    const v = valor;
    setValor(""); // the value is not kept in the page once it is sent
    try {
      const r = await envApi.cambiarLlave(l.id, v, l.id === "correo" && prueba, i.usuario ? usuario.trim() : undefined);
      setRes(r);
      if (r.guardada) onDone();
    } catch (x) {
      setErr(x);
    } finally {
      setBusy(false);
    }
  };
  const formulario = (otraVez: boolean) => (
    <form className="stack-sm" onSubmit={enviar}>
      {i.usuario && (
        <label className="field">
          <span>{i.usuario.etiqueta}</span>
          <input autoComplete="off" spellCheck={false} value={usuario} onChange={(e) => setUsuario(e.target.value)} placeholder={i.usuario.placeholder} />
        </label>
      )}
      <label className="field">
        <span>{i.etiqueta}</span>
        <input type="password" autoComplete="off" spellCheck={false} value={valor} onChange={(e) => setValor(e.target.value)}
          placeholder={otraVez ? t("Paste the corrected key") : i.placeholder} />
      </label>
      {l.id === "correo" && (
        <label className="small"><input type="checkbox" checked={prueba} onChange={(e) => setPrueba(e.target.checked)} /> {t("Also send a test email to the notices' recipients")}</label>
      )}
      <ErrorBox error={err} />
      <div className="row">
        <button className="primary pildora" type="submit" {...apagado(conexion, busy || falta)}>{busy ? t("Testing…") : otraVez ? t("Try again") : t("Test and save")}</button>
      </div>
    </form>
  );

  if (res) return <Resultado l={l} res={res} otraVez={formulario(true)} onOtra={() => setRes(undefined)} />;
  return (
    <section className="cambiar-panel" aria-labelledby="cambiar-titulo">
      <span className="cambiar-sello" aria-hidden="true">{t("Key")}</span>
      <h2 id="cambiar-titulo" className="entorno-h2">{t("Change: {what}", { what: q.nombre() })}</h2>
      <ol className="cambiar-pasos">{i.pasos.map((p, j) => <li key={j}>{p}</li>)}</ol>
      <div className="cambiar-prueba small">
        <strong>{t("The connection test:")}</strong>
        {i.prueba.map((p) => <span key={p}><i className="punto" aria-hidden="true" />{p}</span>)}
        <span className="muted">{i.guarda}</span>
      </div>
      {formulario(false)}
      <span className="small muted">{t("Administrators only. Who changed it and when is recorded, never the key.")}</span>
    </section>
  );
}

/** How the change went: every step with a ✓ or a ✗, and what to do next. */
function Resultado({ l, res, otraVez, onOtra }: { l: Llave; res: LlaveResultado; otraVez: ReactNode; onOtra: () => void }) {
  const fallo = res.pasos.find((p) => !p.ok);
  const lista = (
    <ul className="resultado-pasos">
      {res.pasos.map((p) => (
        <li key={p.id} className={p.ok ? "ok" : "bad"}>
          <span className="marca" aria-hidden="true">{p.ok ? "✓" : "✗"}</span>
          <span><strong>{nombrePaso(l.id, p.id)}</strong>{p.detail && <span className="small muted"> · {p.detail}</span>}</span>
        </li>
      ))}
    </ul>
  );
  if (res.guardada) {
    return (
      <section className="cambiar-panel resultado ok" aria-labelledby="resultado-titulo" role="status">
        <div className="confeti" aria-hidden="true">{Array.from({ length: 7 }, (_, i) => <i key={i} style={{ ["--x" as string]: `${8 + i * 13}%`, ["--d" as string]: `${i * 0.15}s` }} />)}</div>
        <h2 id="resultado-titulo" className="entorno-h2">✓ {t("The new key is in use")}</h2>
        {lista}
        {res.venceEn && <span className="small">{t("It expires on {date}; you are told on {aviso}.", { date: fecha(res.venceEn), aviso: res.avisoEn ? fecha(res.avisoEn) : "—" })}</span>}
        {l.id === "cloudflare" && <div className="nota-falta small"><strong>{t("Still to do:")}</strong> {t("delete the token before in Cloudflare, so it no longer works.")}</div>}
        <div className="row">
          <Link className="btn primary pildora" to="/environment">{t("Back to the Environment")}</Link>
          <button className="link small" onClick={onOtra}>{t("Change it again")}</button>
        </div>
      </section>
    );
  }
  return (
    <section className="cambiar-panel resultado bad" aria-labelledby="resultado-titulo" role="alert">
      <h2 id="resultado-titulo" className="entorno-h2">✗ {t("It was not saved")}</h2>
      {lista}
      {fallo?.detail && <p style={{ margin: 0 }}>{fallo.detail}</p>}
      <div className="nota-falta small">{t("The key before is still in use: nothing stopped working.")}</div>
      {otraVez}
    </section>
  );
}

/** A step of a setting's test or change, in words. */
function nombrePasoAjuste(id: string): string {
  const nombres: Record<string, () => string> = {
    valid: () => t("The value is well formed"),
    answers: () => t("It answers the registry API"),
    push: () => t("BuildKit pushed a test image"),
    pull: () => t("Every node pulled it"),
    delete: () => t("The test image"),
    zones: () => t("The token sees the zone"),
    record: () => t("It created and deleted a test record"),
    target: () => t("The target"),
    exists: () => t("It exists"),
    ready: () => t("It is ready"),
    bind: () => t("A test volume binds"),
    controller: () => t("Its controller is running"),
    resolves: () => t("The address resolves"),
    identity: () => t("It answers as this platform"),
    save: () => t("Saved"),
    restart: () => t("The platform restarts"),
  };
  return (nombres[id] ?? (() => id))();
}

function PasosAjuste({ pasos }: { pasos: AjusteResultado["pasos"] }) {
  return (
    <ul className="resultado-pasos">
      {pasos.map((p) => (
        <li key={p.id} className={p.ok ? "ok" : "bad"}>
          <span className="marca" aria-hidden="true">{p.ok ? "✓" : "✗"}</span>
          <span><strong>{nombrePasoAjuste(p.id)}</strong>{p.detail && <span className="small muted"> · {p.detail}</span>}</span>
        </li>
      ))}
    </ul>
  );
}

/**
 * Changing a setting tested first: now and new, what happens to what already exists, the connection test, and
 * «Change» only once the test passed for these very values. Saving restarts the platform; the ConfigMap's value is one
 * click away.
 */
function CambiarAjuste({ a, onDone }: { a: AjusteProbado; onDone: () => void }) {
  const conexion = useConexion();
  const q = AJUSTES[a.id];
  const [nuevos, setNuevos] = useState<Record<string, string>>(() => Object.fromEntries(a.valores.map((v) => [v.key, v.value])));
  const [busy, setBusy] = useState<"" | "prueba" | "cambio" | "vuelta">("");
  const [err, setErr] = useState<unknown>();
  const [prueba, setPrueba] = useState<AjusteResultado>();
  const [hecho, setHecho] = useState<AjusteResultado>();
  const distinto = a.valores.some((v) => (nuevos[v.key] ?? "").trim() !== v.value);
  const correr = async (que: "prueba" | "cambio" | "vuelta") => {
    setBusy(que);
    setErr(undefined);
    try {
      if (que === "prueba") {
        setPrueba(await envApi.probarAjuste(a.id, nuevos));
      } else {
        const r = que === "cambio" ? await envApi.cambiarAjuste(a.id, nuevos, prueba?.ficha ?? "") : await envApi.volverAjuste(a.id);
        setHecho(r);
        setPrueba(undefined);
        if (r.guardado) onDone();
      }
    } catch (x) {
      setErr(x);
    } finally {
      setBusy("");
    }
  };
  const cambiar = (k: string, v: string) => {
    setNuevos((n) => ({ ...n, [k]: v }));
    setPrueba(undefined); // a test is for the values it tried
  };

  if (!a.cambiable) {
    return (
      <section className="cambiar-panel">
        <span className="cambiar-sello" aria-hidden="true">{t("Setting")}</span>
        <h2 className="entorno-h2">{t("Change: {what}", { what: q.nombre() })}</h2>
        <p style={{ margin: 0 }}>{a.reason}</p>
      </section>
    );
  }
  if (hecho) {
    const ok = hecho.guardado;
    return (
      <section className={`cambiar-panel resultado ${ok ? "ok" : "bad"}`} aria-labelledby="ajuste-hecho" role={ok ? "status" : "alert"}>
        <h2 id="ajuste-hecho" className="entorno-h2">{ok ? `✓ ${t("Saved")}` : `✗ ${t("It was not saved")}`}</h2>
        <PasosAjuste pasos={hecho.pasos} />
        {ok && hecho.reinicio && <div className="nota-falta small">{hecho.reinicio}</div>}
        {!ok && <div className="nota-falta small">{t("The value before is still in use: nothing stopped working.")}</div>}
        <div className="row">
          <Link className="btn primary pildora" to="/environment">{t("Back to the Environment")}</Link>
          <button className="link small" onClick={() => setHecho(undefined)}>{t("Change it again")}</button>
        </div>
      </section>
    );
  }
  const pendiente = a.valores.some((v) => v.pendiente);
  return (
    <section className="cambiar-panel" aria-labelledby="ajuste-titulo">
      <span className="cambiar-sello" aria-hidden="true">{t("Setting")}</span>
      <h2 id="ajuste-titulo" className="entorno-h2">{t("Change: {what}", { what: q.nombre() })}</h2>
      <form className="stack-sm" onSubmit={(e) => { e.preventDefault(); if (distinto) void correr("prueba"); }}>
        {a.valores.map((v) => (
          <div key={v.key} className="stack-sm">
            <span className="small muted">{t("Now")} · <span className="mono">{v.key}</span>: <span className="mono">{v.value || "—"}</span>
              {v.savedBy && <> · {t("changed here by {who}, {when}", { who: v.savedBy, when: v.savedAt ? timeAgo(v.savedAt) : "" })}</>}
            </span>
            <label className="field">
              <span>{t("New")}</span>
              <input className="mono" autoComplete="off" spellCheck={false} value={nuevos[v.key] ?? ""} onChange={(e) => cambiar(v.key, e.target.value)} />
            </label>
          </div>
        ))}
        <div className="nota-falta small"><strong>{t("What happens to what already exists:")}</strong> {q.existe()}</div>
        <ErrorBox error={err} />
        <div className="row">
          <button className="pildora" type="submit" {...apagado(conexion, busy !== "" || !distinto)}>{busy === "prueba" ? t("Testing…") : t("Test the connection")}</button>
        </div>
      </form>
      <div className="cambiar-prueba small">
        <strong>{t("The connection test:")}</strong>
        {prueba ? <PasosAjuste pasos={prueba.pasos} /> : q.prueba().map((p) => <span key={p}><i className="punto" aria-hidden="true" />{p}</span>)}
        <span className="muted">{prueba?.pasa === false ? t("It did not pass: the value of now stays, and nothing changed.")
          : t("If it passes, you see the summary and confirm with «Change». The platform then restarts to use it (a minute or two).")}</span>
      </div>
      <div className="row">
        <button className="primary pildora" type="button" onClick={() => void correr("cambio")} {...apagado(conexion, busy !== "" || !prueba?.pasa)}>
          {busy === "cambio" ? t("Saving…") : t("Change")}
        </button>
        {a.guardado && (
          <button className="link small" type="button" onClick={() => void correr("vuelta")} {...apagado(conexion, busy !== "")}>
            {t("Back to the ConfigMap's value ({value})", { value: a.valores.map((v) => v.env || "—").join(" → ") })}
          </button>
        )}
      </div>
      {pendiente && <p className="small muted" style={{ margin: 0 }}>{t("A value saved here is waiting for the platform to restart.")}</p>}
      <span className="small muted">{t("Administrators only. Who changed it and when is recorded. Only with Helm: the database, the encryption key, the mode and the role.")}</span>
    </section>
  );
}
