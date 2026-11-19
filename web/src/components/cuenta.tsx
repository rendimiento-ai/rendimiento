// The account's pieces the top bar shows on every page: the role's name,
// the organization switch, and on a wide screen the account's menu (the
// organization, the theme, the language, your comments and Sign out).
// Apart from pages/Members, which loads when opened.
import { useEffect, useRef, useState, type KeyboardEvent as TeclaReact } from "react";
import { Link, useLocation } from "react-router-dom";
import { api, type MemberRole, type Org } from "../api";
import { usePoll } from "./ui";
import { t } from "../i18n";
import { MovimientoSwitch, TemaSwitch } from "./ambiente";
import { BanderaSwitch, LangSwitch } from "./marca";

export const memberRoleLabel: Record<MemberRole, string> = { guest: "Guest", collaborator: "Collaborator", admin: "Administrator" };

/**
 * The organization the person works in, as a choice in the top bar when
 * they belong to more than one; choosing another reloads the page in it.
 * With sola, an organization of one shows its name instead of nothing.
 */
export function OrgSwitch({ current, sola }: { current?: Org; sola?: boolean }) {
  const { data } = usePoll(api.orgs, [], 0);
  if (!current) return null;
  if (!data || data.length < 2) return sola ? <span className="small muted">{current.name}</span> : null;
  return (
    <select className="org-switch" aria-label={t("Organization")} value={current.id} onChange={(e) => {
      api.switchOrg(Number(e.target.value)).then(() => location.assign("/"), (err: Error) => alert(err.message));
    }}>
      {data.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
    </select>
  );
}

const enfocables = (el: HTMLElement | null) =>
  [...(el?.querySelectorAll<HTMLElement>("a[href], button:not([disabled]), select, [tabindex='0']") ?? [])]
    .filter((x) => !x.closest(".tema-panel"));

/**
 * The account's button in the top bar (its initial and login) and its menu:
 * who you are, the organization, the theme, the language, your comments to
 * the team and Sign out. Escape or a click outside closes it, and the focus
 * goes back to the button; the arrow keys move between its rows, Tab too.
 */
export function MenuCuenta({ login, role, org, demo, salir }: { login: string; role?: MemberRole; org?: Org; demo?: boolean; salir: () => void }) {
  const [abierto, setAbierto] = useState(false);
  const caja = useRef<HTMLDivElement>(null);
  const boton = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const { pathname } = useLocation();
  useEffect(() => setAbierto(false), [pathname]);

  useEffect(() => {
    if (!abierto) return;
    enfocables(panel.current)[0]?.focus();
    const fuera = (e: MouseEvent) => { if (caja.current && !caja.current.contains(e.target as Node)) setAbierto(false); };
    const tecla = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      if (panel.current?.querySelector(".tema-panel")) return; // the list of colors closes first, by itself
      setAbierto(false);
      boton.current?.focus();
    };
    document.addEventListener("mousedown", fuera);
    document.addEventListener("keydown", tecla);
    return () => {
      document.removeEventListener("mousedown", fuera);
      document.removeEventListener("keydown", tecla);
    };
  }, [abierto]);

  // Up and down (and Home, End) move between the menu's rows; in a list (the organization, the flag) they choose in it.
  const mover = (e: TeclaReact<HTMLDivElement>) => {
    if ((e.target as HTMLElement).tagName === "SELECT" || (e.target as HTMLElement).closest(".tema-panel")) return;
    const todos = enfocables(panel.current);
    const i = todos.indexOf(document.activeElement as HTMLElement);
    const a = e.key === "ArrowDown" ? (i + 1) % todos.length : e.key === "ArrowUp" ? (i - 1 + todos.length) % todos.length
      : e.key === "Home" ? 0 : e.key === "End" ? todos.length - 1 : -1;
    if (a < 0) return;
    e.preventDefault();
    todos[a]?.focus();
  };

  return (
    <div className="cuenta" ref={caja} onBlur={(e) => { if (e.relatedTarget && !caja.current?.contains(e.relatedTarget as Node)) setAbierto(false); }}>
      <button type="button" ref={boton} className="cuenta-boton" aria-expanded={abierto} aria-controls="menu-cuenta"
        title={t("Your account and preferences")} onClick={() => setAbierto(!abierto)}>
        <span className="cuenta-inicial" aria-hidden="true">{login.slice(0, 1)}</span>
        <span className="cuenta-login">{login}</span>
        <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M2.5 4.5 6 8l3.5-3.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>
        <span className="sr-only">{t("Your account and preferences")}</span>
      </button>
      {abierto && (
        <div className="menu-cuenta" id="menu-cuenta" ref={panel} role="group" aria-label={t("Your account and preferences")} onKeyDown={mover}>
          {demo
            ? <span className="badge demo-badge" title={t("You are seeing a demo: you can look at everything, and change nothing.")}>{t("Read-only demo")}</span>
            : <Link to="/members" className="menu-cuenta-fila menu-cuenta-quien"><b>{login}</b><small>{t(memberRoleLabel[role ?? "guest"])}</small></Link>}
          {org && <div className="menu-cuenta-fila"><span>{t("Organization")}</span><OrgSwitch current={org} sola /></div>}
          <div className="menu-cuenta-fila"><span>{t("Theme")}</span><span className="menu-cuenta-ctl"><TemaSwitch /><MovimientoSwitch /></span></div>
          <div className="menu-cuenta-fila"><span>{t("Language")}</span><span className="menu-cuenta-ctl"><LangSwitch /><BanderaSwitch /></span></div>
          <hr />
          {!demo && <Link to="/cuentas" className="menu-cuenta-fila">{t("Your accounts")}</Link>}
          <Link to="/escribir" className="menu-cuenta-fila">{t("My comments")}</Link>
          <button type="button" className="menu-cuenta-fila menu-cuenta-salir" onClick={salir}>{t("Sign out")}</button>
        </div>
      )}
    </div>
  );
}
