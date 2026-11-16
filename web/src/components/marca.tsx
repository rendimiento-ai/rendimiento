// The brand mark and the language switch, shared by the top bar and the welcome page.
import { lang, locale, setLang, t } from "../i18n";
import { BANDERAS, BanderaSVG, nombrePais, ponerBandera, useBandera } from "../banderas";

/** rendimiento's mark: the sun over the furrows, the worked field (it sways a little when the pointer is on it). */
export function Logo() {
  return (
    <span className="logo" aria-hidden>
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="var(--accent-text)" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
        <path d="M6 13a6 6 0 0 1 12 0M12 3v2M5 6l1.5 1.5M19 6l-1.5 1.5M2.5 12.5h2M19.5 12.5h2M3 16.5q9-3 18 0M3 20q9-3 18 0" />
      </svg>
    </span>
  );
}

/** The seed that sprouts: the icon of the button that makes a new app (it sprouts on hover and focus). */
export function Semilla() {
  return (
    <svg className="semilla" width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
      <ellipse cx="12" cy="18" rx="4.2" ry="2.8" />
      <path className="semilla-brote" d="M12 15.2V9M12 11c0-3 2-4.5 4.5-4.5C16.5 9.5 14.5 11 12 11M12 13c-2.5 0-4-1.5-4-4 2.5 0 4 1.5 4 4" />
    </svg>
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
