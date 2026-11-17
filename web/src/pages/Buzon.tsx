// The team's mailbox (decision 145): every comment people wrote, with what
// the platform's AI made of it, for a person to rewrite the draft in their
// own words and send it with their name. Nothing goes out by itself.
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError, timeAgo, type ClaseOpinion, type EstadoOpinion, type Opinion, type OpinionDetalle } from "../api";
import { EsqListaDetalle } from "../components/esqueleto";
import { t, tn } from "../i18n";
import { usePoll } from "../components/ui";

const clases: Record<ClaseOpinion, string> = {
  ya_existe: "It exists already",
  nuevo: "New",
  falla: "Something fails",
  sin_sentido: "Nonsense",
};
const estados: Record<EstadoOpinion, string> = {
  por_revisar: "To review",
  respondido: "Answered",
  descartado: "Dismissed",
};

/** Where a match points: a book page, a Ruta entry, or an earlier comment. */
function Coincidencia({ ref_, abrir }: { ref_: string; abrir: (id: number) => void }) {
  const [tipo, valor] = ref_.split(":", 2);
  if (tipo === "libro") {
    const archivo = valor.replace(/\/$/, "") + ".md";
    return <a href={`https://github.com/rendimiento-ai/rendimiento/blob/main/docs/content/${archivo}`} target="_blank" rel="noreferrer">{t("Book: {page}", { page: valor })}</a>;
  }
  if (tipo === "ruta") return <span>{t("Ruta entry {n}", { n: valor })}</span>;
  if (tipo === "opinion") return <button type="button" className="link" onClick={() => abrir(Number(valor))}>{t("Comment #{n}", { n: valor })}</button>;
  return <span>{ref_}</span>;
}

export function Buzon() {
  const { data, error, reload } = usePoll(() => api.opiniones(), [], 30000);
  const [filtro, setFiltro] = useState<EstadoOpinion>("por_revisar");
  const [abierta, setAbierta] = useState<number>();
  const lista = data ?? [];
  // A group's count: how many comments ask the same thing (the evidence of demand).
  const grupos = useMemo(() => {
    const n: Record<number, number> = {};
    for (const o of lista) {
      const raiz = o.grupo ?? o.id;
      n[raiz] = (n[raiz] ?? 0) + 1;
    }
    return n;
  }, [lista]);
  const vistas = lista.filter((o) => o.estado === filtro);
  const cuenta = (e: EstadoOpinion) => lista.filter((o) => o.estado === e).length;

  return (
    <div className="buzon-pagina">
      <h1>{t("Comments")}</h1>
      <p className="muted">{t("What people wrote to the team, from the app and from the public form. The platform's AI sorts each one and drafts an answer; you rewrite it in your words and send it, with your name.")}</p>
      <div className="tabs" role="tablist">
        {(Object.keys(estados) as EstadoOpinion[]).map((e) => (
          <button key={e} role="tab" aria-selected={filtro === e} className={filtro === e ? "tab on" : "tab"} onClick={() => setFiltro(e)}>
            {t(estados[e])} <span className="nav-count">{cuenta(e)}</span>
          </button>
        ))}
      </div>
      {error ? <p className="error">{error instanceof Error ? error.message : String(error)}</p> : null}
      {!data && !error && <EsqListaDetalle cabeza={false} />}
      {(data || !!error) && <div className="buzon-cuerpo">
        <ul className="buzon-lista">
          {vistas.length === 0 && <li className="muted small">{t("No comments here.")}</li>}
          {vistas.map((o) => (
            <li key={o.id}>
              <button type="button" className={`buzon-fila${abierta === o.id ? " on" : ""}`} onClick={() => setAbierta(o.id)}>
                <span className="buzon-fila-arriba">
                  <span className={`badge clase-${o.clase || "ninguna"}`}>{o.clase ? t(clases[o.clase]) : o.iaError ? t("Unclassified") : t("Sorting…")}</span>
                  <span className="small muted">{o.origen === "app" ? t("From the app") : t("From the form")} · {timeAgo(o.createdAt)}</span>
                </span>
                <span className="buzon-fila-texto">{o.resumen || o.texto.slice(0, 140)}</span>
                {(grupos[o.grupo ?? o.id] ?? 1) > 1 && (
                  <span className="small muted">{tn(grupos[o.grupo ?? o.id], "{n} person asks this", "{n} people ask this")}</span>
                )}
              </button>
            </li>
          ))}
        </ul>
        {abierta ? <Detalle id={abierta} abrir={setAbierta} cambio={reload} lista={lista} /> : <div className="buzon-detalle muted">{t("Choose a comment to read it.")}</div>}
      </div>}
    </div>
  );
}

function Detalle({ id, abrir, cambio, lista }: { id: number; abrir: (id: number) => void; cambio: () => void; lista: Opinion[] }) {
  const [o, setO] = useState<OpinionDetalle>();
  const [respuesta, setRespuesta] = useState("");
  const [seguro, setSeguro] = useState(false); // the second press sends
  const [ocupado, setOcupado] = useState(false);
  const [error, setError] = useState("");
  const [grupo, setGrupo] = useState("");

  useEffect(() => {
    setO(undefined);
    setError("");
    setSeguro(false);
    api.opinion(id).then((d) => {
      setO(d);
      setRespuesta(d.borrador ?? "");
      setGrupo(d.grupo ? String(d.grupo) : "");
    }, (e) => setError(e instanceof Error ? e.message : String(e)));
  }, [id]);

  // While the AI sorts it (or drafts it again), look every few seconds, and
  // put its new draft in the answer when it comes.
  const ordenando = !!o && !o.clasificadaAt && !o.iaError;
  useEffect(() => {
    if (!ordenando) return;
    const tm = window.setInterval(() => {
      api.opinion(id).then((d) => {
        if (!d.clasificadaAt && !d.iaError) return;
        setO(d);
        if (d.estado !== "respondido") setRespuesta(d.borrador ?? "");
        cambio();
      }, () => undefined);
    }, 4000);
    return () => window.clearInterval(tm);
  }, [id, ordenando, cambio]);

  async function hacer(f: () => Promise<OpinionDetalle | void>) {
    setOcupado(true);
    setError("");
    try {
      const d = await f();
      if (d) {
        setO(d);
        setRespuesta(d.respuesta || d.borrador || "");
      }
      cambio();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setOcupado(false);
      setSeguro(false);
    }
  }

  if (!o) return <div className="buzon-detalle">{error ? <p className="error">{error}</p> : <span className="muted">{t("Loading…")}</span>}</div>;
  const respondido = o.estado === "respondido";
  const mismos = lista.filter((x) => x.id !== o.id && (x.grupo ?? x.id) === (o.grupo ?? o.id));

  return (
    <div className="buzon-detalle">
      <div className="buzon-carta-leida">
        <div className="small muted">
          {t("Comment #{n}", { n: o.id })} · {o.origen === "app" ? t("From the app") : t("From the form")} · {new Date(o.createdAt).toLocaleString()}
        </div>
        {o.nombre && <div><b>{o.nombre}</b></div>}
        <p className="buzon-texto">{o.texto}</p>
        {o.correo && <div className="small">{t("Email")}: <code>{o.correo}</code></div>}
        {o.datos && Object.keys(o.datos).length > 0 && (
          <div className="small muted">{[o.datos.version && `v${o.datos.version}`, o.datos.modo, o.datos.motor, o.datos.so].filter(Boolean).join(" · ")}</div>
        )}
      </div>

      <div className="buzon-ia">
        <div className="small muted">{t("What the AI made of it")}</div>
        {o.iaError ? <p className="small">{t("It could not sort it: {why}", { why: o.iaError })}</p> : !o.clasificadaAt ? <p className="small">{o.rutaId ? t("Writing the draft again, now saying it was noted…") : t("Sorting…")}</p> : (
          <>
            {o.resumen && <p>{o.resumen}</p>}
            {o.coincidencias.length > 0 && (
              <ul className="small buzon-coincidencias">
                {o.coincidencias.map((c) => <li key={c}><Coincidencia ref_={c} abrir={abrir} /></li>)}
              </ul>
            )}
          </>
        )}
        {!respondido && (
          <div className="buzon-acciones">
            <label className="small">
              {t("Class")}{" "}
              <select value={o.clase || ""} disabled={ocupado} onChange={(e) => hacer(() => api.cambiarOpinion(o.id, { clase: e.target.value as ClaseOpinion }))}>
                {!o.clase && <option value="">—</option>}
                {(Object.keys(clases) as ClaseOpinion[]).map((c) => <option key={c} value={c}>{t(clases[c])}</option>)}
              </select>
            </label>
            <label className="small">
              {t("Same as comment #")}{" "}
              <input value={grupo} onChange={(e) => setGrupo(e.target.value.replace(/\D/g, ""))} size={5} inputMode="numeric" />
              <button type="button" disabled={ocupado} onClick={() => hacer(() => api.cambiarOpinion(o.id, { grupo: Number(grupo) || 0 }))}>{t("Save")}</button>
            </label>
            <button type="button" disabled={ocupado} onClick={() => hacer(async () => { await api.reclasificarOpinion(o.id); return api.opinion(o.id); })}>{t("Sort it again")}</button>
          </div>
        )}
        <div className="buzon-acciones">
          {o.rutaId ? (
            <span className="small">{t("In the Ruta as entry {n}.", { n: o.rutaId })} <Link to="/ruta">{t("Open the Ruta")}</Link></span>
          ) : (
            <button type="button" disabled={ocupado} onClick={() => hacer(() => api.pasarOpinionALaRuta(o.id))}
              title={t("It becomes an idea in the Ruta, with your name and their words, and the AI drafts the answer again saying it was noted.")}>
              {t("Pass it to the Ruta")}
            </button>
          )}
        </div>
        {mismos.length > 0 && <div className="small muted">{t("Asks the same as: {list}", { list: mismos.map((m) => `#${m.id}`).join(", ") })}</div>}
      </div>

      {respondido ? (
        <div className="buzon-respuesta">
          <div className="small muted">{t("Answered by {who}, {when}", { who: o.respondidoPor ?? "", when: o.respondidoAt ? timeAgo(o.respondidoAt) : "" })}</div>
          <p className="buzon-texto">{o.respuesta}</p>
        </div>
      ) : o.estado === "descartado" ? (
        <div className="buzon-respuesta">
          <p className="small muted">{t("Set aside: it is not answered, and its email was deleted.")}</p>
          <button type="button" disabled={ocupado} onClick={() => hacer(() => api.cambiarOpinion(o.id, { estado: "por_revisar" }))}>{t("Back to review")}</button>
        </div>
      ) : (
        <div className="buzon-respuesta">
          <label>
            <span className="small muted">{t("Your answer (the AI's draft: make it yours)")}</span>
            <textarea rows={8} value={respuesta} onChange={(e) => { setRespuesta(e.target.value); setSeguro(false); }} maxLength={8000} />
          </label>
          <div className="small muted">
            {o.origen === "formulario"
              ? t("It goes once by email to {email}, with your name and their comment.", { email: o.correo || "—" })
              : t("It waits for their installation to fetch it, and shows in their bell.")}
          </div>
          {error && <p className="error" role="alert">{error}</p>}
          <div className="buzon-acciones">
            <button type="button" className="btn primary" disabled={ocupado || !respuesta.trim()} onClick={() => (seguro ? hacer(() => api.responderOpinion(o.id, respuesta)) : setSeguro(true))}>
              {seguro ? t("Yes, send it") : t("Send the answer")}
            </button>
            {seguro && <button type="button" onClick={() => setSeguro(false)}>{t("Not yet")}</button>}
            <button type="button" disabled={ocupado} onClick={() => hacer(() => api.cambiarOpinion(o.id, { estado: "descartado" }))}>{t("Set aside")}</button>
          </div>
        </div>
      )}
    </div>
  );
}
