// The public form to write to the team (decision 145): anyone, without an
// account, with an email to get the answer. It is served by every
// rendimiento but received only where señales are (rendimiento.ai); the
// person answers by hand, so nothing comes back at once.
import { useState } from "react";
import { api, ApiError } from "../api";
import { lang, t } from "../i18n";
import { Cielo, Guardapolvo, MovimientoSwitch, TemaSwitch, Techo } from "../components/ambiente";
import { LangSwitch, Logo } from "../components/marca";

const MAX = 2000;

export function ComentarioPublico() {
  const [texto, setTexto] = useState("");
  const [correo, setCorreo] = useState("");
  const [nombre, setNombre] = useState("");
  const [sitio, setSitio] = useState(""); // people never see it; robots fill it
  const [estado, setEstado] = useState<"" | "enviando" | "enviado">("");
  const [error, setError] = useState("");

  async function enviar(e: React.FormEvent) {
    e.preventDefault();
    setEstado("enviando");
    setError("");
    try {
      await api.enviarComentario({ texto, correo: correo.trim(), nombre: nombre.trim(), idioma: lang, sitio });
      setEstado("enviado");
    } catch (err) {
      setEstado("");
      const status = err instanceof ApiError ? err.status : 0;
      setError(status === 429 ? t("You have written several times this hour. Please try again later.")
        : status === 404 ? t("This rendimiento does not receive comments. Write at rendimiento.ai/comentarios.")
        : status === 400 ? t("Check the email and the text (up to 2,000 characters).")
        : t("It could not be sent. Your text is still here; try again in a moment."));
    }
  }

  return (
    <div className="bienv">
      <Cielo />
      <Techo />
      <header className="bienv-barra">
        <a className="brand" href="/"><Logo /> <span className="brand-name">rendimiento</span></a>
        <span className="spacer" />
        <TemaSwitch />
        <MovimientoSwitch />
        <LangSwitch />
      </header>
      <main className="bienv-main">
        <section className="bienv-seccion buzon" aria-labelledby="escribanos">
          <h1 id="escribanos">{t("Write to us")}</h1>
          <p className="bienv-entrada">
            {t("An idea, something that did not work, a question: a person reads every comment and answers it by email, in their own words. It may take a few days.")}
          </p>
          {estado === "enviado" ? (
            <div className="buzon-carta" role="status">
              <h2>{t("Thank you: it is in the mailbox.")}</h2>
              <p>{t("The answer will reach {correo}, once, from a person.", { correo: correo.trim() })}</p>
              <a className="btn" href="/">{t("Back to rendimiento")}</a>
            </div>
          ) : (
            <form className="buzon-carta" onSubmit={enviar}>
              <label>
                <span>{t("Your comment")}</span>
                <textarea value={texto} onChange={(e) => setTexto(e.target.value)} maxLength={MAX} rows={7} required
                  placeholder={t("What would you like rendimiento to do, or what got in your way?")} />
                <span className="small muted buzon-cuenta">{texto.length.toLocaleString(lang)} / {MAX.toLocaleString(lang)}</span>
              </label>
              <label>
                <span>{t("Your email")} <span className="muted small">({t("required, only to answer you")})</span></span>
                <input type="email" value={correo} onChange={(e) => setCorreo(e.target.value)} required maxLength={254} autoComplete="email" />
              </label>
              <label>
                <span>{t("Your name")} <span className="muted small">({t("optional")})</span></span>
                <input value={nombre} onChange={(e) => setNombre(e.target.value)} maxLength={80} autoComplete="name" />
              </label>
              <label className="buzon-trampa" aria-hidden="true">
                <span>Website</span>
                <input tabIndex={-1} autoComplete="off" value={sitio} onChange={(e) => setSitio(e.target.value)} />
              </label>
              {error && <p className="error" role="alert">{error}</p>}
              <button className="btn primary big" disabled={estado === "enviando" || !texto.trim() || !correo.trim()}>
                {estado === "enviando" ? t("Sending…") : t("Send")}
              </button>
              <p className="small muted">
                {t("Your email is kept apart from your comment, seen only by the team, used only to answer, and deleted 30 days after the answer. Nothing you write is published.")}
              </p>
            </form>
          )}
        </section>
      </main>
      <Guardapolvo />
    </div>
  );
}
