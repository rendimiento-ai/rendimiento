// Write to the team from this installation (decision 145): the person sees
// exactly what leaves before it leaves, and the answers a person on the
// team sent come back here and to the bell.
import { useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError, timeAgo, type AlEquipo as Vista } from "../api";
import { t } from "../i18n";
import { usePoll } from "../components/ui";

const MAX = 2000;

export function AlEquipo() {
  const { data, error, setData } = usePoll(() => api.alEquipo(), []);
  const [texto, setTexto] = useState("");
  const [enviando, setEnviando] = useState(false);
  const [fallo, setFallo] = useState("");
  const [listo, setListo] = useState(false);

  async function enviar(e: React.FormEvent) {
    e.preventDefault();
    setEnviando(true);
    setFallo("");
    try {
      const v: Vista = await api.escribirAlEquipo(texto);
      setData(v);
      setTexto("");
      setListo(true);
    } catch (err) {
      const status = err instanceof ApiError ? err.status : 0;
      setFallo(status === 429 ? t("You have written several times this hour. Please try again later.")
        : status === 409 ? t("Nothing leaves this installation: its señales are off. You can still write at rendimiento.ai/comentarios.")
        : t("It could not be sent. Your text is still here; try again in a moment."));
    } finally {
      setEnviando(false);
    }
  }

  if (error) return <p className="error">{error instanceof Error ? error.message : String(error)}</p>;
  if (!data) return <div className="muted">{t("Loading…")}</div>;
  const sale = { ...data.vista, texto: texto.trim() };

  return (
    <div className="buzon al-equipo">
      <h1>{t("Write to the team")}</h1>
      <p className="bienv-entrada">{t("An idea, something that did not work, a question: a person on the team reads it and answers it, in their own words. The answer comes back here and to your bell; it may take a few days.")}</p>
      {!data.encendidos ? (
        <div className="buzon-carta">
          <p>{t("Nothing leaves this installation: its señales are off ({why}), and comments go the same way.", { why: data.porque ?? "" })}</p>
          <p className="small muted">{t("You can still write without an account at")} <a href="https://rendimiento.ai/comentarios">rendimiento.ai/comentarios</a>.</p>
        </div>
      ) : (
        <form className="buzon-carta" onSubmit={enviar}>
          <label>
            <span>{t("Your comment")}</span>
            <textarea value={texto} onChange={(e) => { setTexto(e.target.value); setListo(false); }} maxLength={MAX} rows={6} required
              placeholder={t("What would you like rendimiento to do, or what got in your way?")} />
            <span className="small muted buzon-cuenta">{texto.length.toLocaleString()} / {MAX.toLocaleString()}</span>
          </label>
          <details>
            <summary className="small">{t("Exactly what will be sent")}</summary>
            <pre className="small">{JSON.stringify(sale, null, 2)}</pre>
            <p className="small muted">{t("Never your name, your apps or this installation's identity. The token whose hash goes stays here: only with it can this installation fetch the answer.")}</p>
          </details>
          {fallo && <p className="error" role="alert">{fallo}</p>}
          {listo && <p className="small" role="status">{t("Sent. The answer will come back here and to your bell.")}</p>}
          <button className="btn primary" disabled={enviando || !texto.trim()}>{enviando ? t("Sending…") : t("Send")}</button>
        </form>
      )}

      <h2>{t("Your comments")}</h2>
      {data.mios.length === 0 && <p className="muted small">{t("You have not written to the team yet.")}</p>}
      <ul className="buzon-lista">
        {data.mios.map((c) => (
          <li key={c.id} className="buzon-carta-leida">
            <div className="small muted">{timeAgo(c.createdAt)}</div>
            <p className="buzon-texto">{c.texto}</p>
            {c.respuesta ? (
              <div className="buzon-respuesta">
                <div className="small muted">{t("{who} answered, {when}", { who: c.respondidoPor ?? "", when: c.respondidoAt ? timeAgo(c.respondidoAt) : "" })}</div>
                <p className="buzon-texto">{c.respuesta}</p>
              </div>
            ) : <div className="small muted">{t("Waiting for an answer.")}</div>}
          </li>
        ))}
      </ul>
      <p className="small muted"><Link to="/">{t("Back to rendimiento")}</Link></p>
    </div>
  );
}
