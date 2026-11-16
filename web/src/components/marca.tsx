// The brand mark and the language switch, shared by the top bar and the welcome page.
import { lang, locale, setLang, t } from "../i18n";
import { BANDERAS, BanderaSVG, nombrePais, ponerBandera, useBandera } from "../banderas";

/** rendimiento's mark: a pulse line, with a beat that runs along it (still when movement is off). */
export function Logo() {
  return (
    <span className="logo" aria-hidden>
      <svg width="20" height="20" viewBox="0 0 32 32">
        <path className="logo-fondo" d="M3 17 L10 17 L13 9 L18 25 L21 17 L29 17" stroke="var(--accent-text)" strokeWidth="3.4" fill="none" strokeLinecap="round" strokeLinejoin="round" />
        <path className="logo-latido" pathLength={100} d="M3 17 L10 17 L13 9 L18 25 L21 17 L29 17" stroke="var(--accent-text)" strokeWidth="3.4" fill="none" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </span>
  );
}

/** EN/ES: shows the other language, in that language. */
export function LangSwitch() {
  const other = lang === "es" ? "en" : "es";
  return (
    <button className="lang-switch" onClick={() => setLang(other)} title={other === "es" ? "Cambiar a español" : "Switch to English"}
      aria-label={other === "es" ? "Cambiar a español" : "Switch to English"}>
      {other.toUpperCase()}
    </button>
  );
}

/**
 * The flag of the presidencia municipal (Environment's street and the Universe's plaza), chosen by each person on this
 * device: the flag shows, and a list of countries opens over it.
 */
export function BanderaSwitch() {
  const b = useBandera();
  const paises = Object.keys(BANDERAS).map((c) => ({ c, n: nombrePais(c, locale) })).sort((x, y) => x.n.localeCompare(y.n, locale));
  return (
    <label className="bandera-switch" title={t("Your flag: {country}", { country: nombrePais(b, locale) })}>
      <svg width="22" height="15" viewBox="0 0 22 15" aria-hidden="true"><BanderaSVG code={b} w={22} h={15} /></svg>
      <select id="bandera-switch" value={b} onChange={(e) => ponerBandera(e.target.value)} aria-label={t("Your flag")}>
        {paises.map((p) => <option key={p.c} value={p.c}>{p.n}</option>)}
      </select>
    </label>
  );
}
