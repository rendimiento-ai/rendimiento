import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { Link } from "react-router-dom";
import { suenoApi, type Publicacion, type Sueno } from "../api";
import { locale, t, tn } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "./conexion-aviso";

// Dormir and Despertar (Ruta 92): on a computer, the top bar has a «Sleep» button for the platform's administrators,
// which opens the dialog (B1); while asleep everyone sees since when, and an administrator wakes it; after a wake, a
// summary says what it did (B3). In a cluster none of it shows: rendimiento there stays awake.

// One poll for every part of the page that shows the sleep.
let estado: Sueno | undefined;
const oyentes = new Set<() => void>();
let reloj: ReturnType<typeof setInterval> | undefined;
let usuarios = 0;

function poner(s: Sueno | undefined) {
  estado = s;
  oyentes.forEach((f) => f());
}

async function mirar() {
  try {
    poner(await suenoApi.estado());
  } catch {
    /* without a connection, or shut down: keep what was known */
  }
}

/** The sleep as rendimiento says it, looked at every 15 seconds while a page shows it. */
export function useSueno(activo: boolean): [Sueno | undefined, (s: Sueno) => void] {
  const s = useSyncExternalStore(
    (f) => { oyentes.add(f); return () => { oyentes.delete(f); }; },
    () => estado,
  );
  useEffect(() => {
    if (!activo) return;
    usuarios++;
    if (usuarios === 1) {
      void mirar();
      reloj = setInterval(() => void mirar(), 15000);
    }
    return () => {
      usuarios--;
      if (usuarios === 0 && reloj) clearInterval(reloj);
    };
  }, [activo]);
  return [s, poner];
}

const hora = (iso?: string) => (iso ? new Date(iso).toLocaleTimeString(locale, { hour: "numeric", minute: "2-digit" }) : "");

function Luna({ size = 18 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z" />
    </svg>
  );
}

/** The top bar's button: «Sleep» for an administrator; while asleep it says so, and wakes it. */
export function BotonDormir({ operator, telefono }: { operator: boolean; telefono?: boolean }) {
  const [s, poner] = useSueno(true);
  const [abierto, setAbierto] = useState(false);
  const conexion = useConexion();
  if (!s) return null;
  if (s.estado === "despierto") {
    if (!operator) return null;
    return (
      <>
        <button type="button" className="dormir-boton" aria-haspopup="dialog" aria-expanded={abierto} {...apagado(conexion, false, t("Put rendimiento to sleep"))} onClick={() => setAbierto(true)}>
          <Luna /><span className={telefono ? "sr-only" : undefined}>{t("Sleep")}</span>
        </button>
        {abierto && <DialogoDormir s={s} cerrar={() => setAbierto(false)} poner={poner} />}
      </>
    );
  }
  const texto = s.estado === "durmiendose" ? t("Falling asleep…") : t("Asleep");
  return (
    <span className="dormir-boton dormido" title={s.desde ? t("Asleep since {time}", { time: hora(s.desde) }) : texto}>
      <Luna /><span className={telefono ? "sr-only" : undefined}>{texto}</span>
    </span>
  );
}

function publicandose(p: Publicacion): string {
  if (p.run && p.pasos) return t("{app} is being published (run #{run}, step {n} of {total})", { app: p.app, run: p.run, n: Math.min(p.paso ?? 0, p.pasos), total: p.pasos });
  return t("{app} is being published", { app: p.app });
}

/** B1: when to sleep, and whether to shut down completely. */
function DialogoDormir({ s, cerrar, poner }: { s: Sueno; cerrar: () => void; poner: (s: Sueno) => void }) {
  const [ya, setYa] = useState(false);
  const [aHora, setAHora] = useState(!!s.despertarA);
  const [hh, setHh] = useState(s.despertarA || "08:00");
  const [enviando, setEnviando] = useState<"" | "dormir" | "apagar">("");
  const [falla, setFalla] = useState<string>();
  const [apagandose, setApagandose] = useState(false);
  const conexion = useConexion();
  const caja = useRef<HTMLElement>(null);
  const pub = s.publicando ?? [];

  useEffect(() => {
    caja.current?.querySelector<HTMLElement>("input, button")?.focus();
    const tecla = (e: KeyboardEvent) => { if (e.key === "Escape") cerrar(); };
    document.addEventListener("keydown", tecla);
    return () => document.removeEventListener("keydown", tecla);
  }, [cerrar]);

  const ir = async (apagar: boolean) => {
    setEnviando(apagar ? "apagar" : "dormir");
    setFalla(undefined);
    try {
      const quiere = aHora ? hh : "";
      if (quiere !== s.despertarA) await suenoApi.opciones(quiere, s.tapa);
      const nuevo = await suenoApi.dormir(ya, apagar);
      poner(nuevo);
      if (apagar) setApagandose(true);
      else cerrar();
    } catch (e) {
      setFalla(e instanceof Error ? e.message : String(e));
    } finally {
      setEnviando("");
    }
  };

  const principal = pub.length === 0 ? t("Sleep") : ya ? t("Sleep now") : t("Sleep when it ends");
  return (
    <div className="dormir-fondo" onClick={(e) => { if (e.target === e.currentTarget) cerrar(); }}>
      <section className="dormir-dialogo" role="dialog" aria-modal="true" aria-labelledby="dormir-titulo" ref={caja}>
        <div className="dormir-noche">
          <svg width="56" height="56" viewBox="0 0 64 64" aria-hidden="true" className="dormir-luna">
            <circle cx="32" cy="32" r="31" fill="#2E3E78" />
            <path d="M44 38A15 15 0 1 1 26 18a12 12 0 0 0 18 20z" fill="#F2C14E" />
            <circle cx="48" cy="16" r="2" fill="#F4EADA" className="dormir-estrella" />
            <circle cx="14" cy="44" r="1.6" fill="#F4EADA" className="dormir-estrella tarde" />
          </svg>
          <div>
            <h2 id="dormir-titulo">{t("Put rendimiento to sleep")}</h2>
            <p>{t("It stops the apps and the CI. The data, the releases and the Ruta stay as they are.")}</p>
          </div>
        </div>
        {apagandose ? (
          <div className="dormir-cuerpo">
            <p><strong>{t("rendimiento is shutting down.")}</strong> {t("Once the apps are asleep it stops too, and this page stops answering. It comes back, awake, when the computer starts.")}</p>
            <div className="dormir-botones"><button type="button" onClick={cerrar}>{t("Close")}</button></div>
          </div>
        ) : (
          <div className="dormir-cuerpo">
            {pub.map((p) => (
              <div key={p.app} className="dormir-publica">
                <strong>{publicandose(p)}</strong>
                <span>{p.actual ? t("If it sleeps now, the publish is cut and the app stays on release #{n}.", { n: p.actual }) : t("If it sleeps now, the publish is cut and the app stays as it is.")}</span>
              </div>
            ))}
            {pub.length > 0 && (
              <fieldset className="dormir-cuando">
                <legend>{t("When?")}</legend>
                <label className={!ya ? "elegida" : undefined}>
                  <input type="radio" name="dormir-cuando" checked={!ya} onChange={() => setYa(false)} />
                  <span><strong>{t("When the publish ends")}</strong><span>{t("Branch runs stop now and run again on waking.")}</span></span>
                </label>
                <label className={ya ? "elegida" : undefined}>
                  <input type="radio" name="dormir-cuando" checked={ya} onChange={() => setYa(true)} />
                  <span><strong>{t("Without waiting")}</strong><span>{t("The app stays on the release it runs; the new one is published on waking.")}</span></span>
                </label>
              </fieldset>
            )}
            {pub.length === 0 && s.corriendo > 0 && (
              <p className="small muted">{tn(s.corriendo, "1 run is going: it stops now and runs again on waking, not marked failed.", "{n} runs are going: they stop now and run again on waking, not marked failed.")}</p>
            )}
            <div className="dormir-datos">
              <div><strong>{t("Webhooks")}</strong><span>{t("GitHub's, GitLab's and Bitbucket's are kept and handled on waking, in order.")}</span></div>
              <div><strong>{t("The apps' addresses")}</strong><span>{t("They answer “Back soon” while it sleeps.")}</span></div>
            </div>
            <label className="dormir-hora">
              <input type="checkbox" checked={aHora} onChange={(e) => setAHora(e.target.checked)} />
              {t("Wake up by itself at")}
              <input type="time" value={hh} aria-label={t("Hour to wake up")} disabled={!aHora} onChange={(e) => setHh(e.target.value)} />
            </label>
            {falla && <div className="alert">{falla}</div>}
            <div className="dormir-botones">
              <button type="button" onClick={cerrar}>{t("Better not")}</button>
              <button type="button" className="dormir-apagar" {...apagado(conexion, !!enviando)} onClick={() => void ir(true)}>
                {enviando === "apagar" ? t("Shutting down…") : t("Shut down completely")}
              </button>
              <button type="button" className="primary" {...apagado(conexion, !!enviando)} onClick={() => void ir(false)}>
                {enviando === "dormir" ? t("Falling asleep…") : principal}
              </button>
            </div>
            <span className="dormir-nota">{t("“Shut down completely” also stops rendimiento; it comes back when the computer starts.")}</span>
          </div>
        )}
      </section>
    </div>
  );
}

/** Under the top bar: asleep since when, what waits, and (for an administrator) «Wake up now»; after a wake, B3. */
export function AvisoSueno({ operator }: { operator: boolean }) {
  const [s, poner] = useSueno(true);
  const [despertando, setDespertando] = useState(false);
  const [falla, setFalla] = useState<string>();
  const [visto, setVisto] = useState(() => {
    try { return localStorage.getItem("rendimiento.despertar-visto") ?? ""; } catch { return ""; }
  });
  const conexion = useConexion();
  if (!s) return null;

  const despertar = async () => {
    setDespertando(true);
    setFalla(undefined);
    try {
      poner(await suenoApi.despertar());
    } catch (e) {
      setFalla(e instanceof Error ? e.message : String(e));
    } finally {
      setDespertando(false);
    }
  };

  if (s.estado !== "despierto") {
    const partes = [
      s.estado === "durmiendose"
        ? s.esperada ? t("rendimiento falls asleep once {app} is published.", { app: s.esperada }) : t("rendimiento is falling asleep.")
        : t("rendimiento is asleep since {time}.", { time: hora(s.desde) }),
      s.avisos > 0 ? tn(s.avisos, "1 webhook waits.", "{n} webhooks wait.") : "",
      s.despiertaA ? t("It wakes up at {time}.", { time: hora(s.despiertaA) }) : "",
    ].filter(Boolean);
    return (
      <div className="dormir-aviso" role="status">
        <Luna size={20} />
        <span>{partes.join(" ")}</span>
        {operator && (
          <button type="button" className="primary" {...apagado(conexion, despertando)} onClick={() => void despertar()}>
            {despertando ? t("Waking up…") : t("Wake up now")}
          </button>
        )}
        {falla && <span className="alert">{falla}</span>}
      </div>
    );
  }

  const r = s.resumen;
  if (!r || r.hasta === visto || Date.now() - new Date(r.hasta).getTime() > 24 * 3600e3) return null;
  const listo = () => {
    try { localStorage.setItem("rendimiento.despertar-visto", r.hasta); } catch { /* not kept: it shows until reload */ }
    setVisto(r.hasta);
  };
  return (
    <section className="dormir-resumen" aria-labelledby="despertar-titulo">
      <h2 id="despertar-titulo">{t("It woke up")}</h2>
      <p className="muted">{r.desde ? t("It slept from {from} to {to}. This is what it did on coming back.", { from: hora(r.desde), to: hora(r.hasta) }) : t("This is what it did on coming back.")}</p>
      <ul>
        <li className="hecho">
          <strong>{tn(r.apps.length, "1 app started", "{n} apps started")}</strong>
          <span>{r.apps.length ? t("{apps}: the same releases as before.", { apps: r.apps.join(", ") }) : t("None was running when it fell asleep.")}</span>
        </li>
        {r.esperada && (
          <li className="hecho"><strong>{t("{app} published", { app: r.esperada })}</strong><span>{t("It waited for it to end before sleeping.")}</span></li>
        )}
        <li className={r.fallidos ? "ojo" : "hecho"}>
          <strong>{tn(r.avisos, "1 webhook handled", "{n} webhooks handled")}</strong>
          <span>{r.fallidos ? tn(r.fallidos, "In the order they came; 1 could not be handled.", "In the order they came; {n} could not be handled.") : t("In the order they came.")}</span>
        </li>
        <li className="hecho">
          <strong>{tn(r.repetidas.length, "1 run running again", "{n} runs running again")}</strong>
          <span>{t("Sleep cut them; they start again and are not marked failed.")}</span>
        </li>
        {r.problema && <li className="ojo"><strong>{t("Something did not start")}</strong><span>{r.problema}</span></li>}
      </ul>
      <p className="small muted"><strong>{t("What it did not see while asleep.")}</strong> {t("The lookout does not count these hours as downtime: the apps were asleep, not broken.")}</p>
      <div className="dormir-botones">
        <Link to="/" className="btn primary" onClick={listo}>{t("Go to my apps")}</Link>
        <button type="button" onClick={listo}>{t("Close")}</button>
      </div>
    </section>
  );
}
