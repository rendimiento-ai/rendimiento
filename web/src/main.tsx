import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Link, NavLink, Navigate, Route, Routes } from "react-router-dom";
import { api, ApiError, session, type MemberRole, type Org } from "./api";
import { Members, OrgSwitch, memberRoleLabel } from "./pages/Members";
import { IA } from "./pages/IA";
import { t } from "./i18n";
import { Dashboard } from "./pages/Dashboard";
import { NewApp } from "./pages/NewApp";
import { AppPage } from "./pages/AppPage";
import { RunPage } from "./pages/RunPage";
import { Setup } from "./pages/Setup";
import { Environment } from "./pages/Environment";
import { Services } from "./pages/Services";
import { Universe } from "./pages/Universe";
import { RequestPage } from "./pages/Request";
import { Addons } from "./pages/Addons";
import { Problems, ProblemsNavLink } from "./pages/Problems";
import { Mercado, PuestoEditor } from "./pages/Mercado";
import { Ruta } from "./pages/Ruta";
import { Bitacora } from "./pages/Bitacora";
import { SenalesPage } from "./pages/SenalesPage";
import { EnvironmentNavLink } from "./pages/Version";
import { ComentarioPublico } from "./pages/ComentarioPublico";
import { Buzon } from "./pages/Buzon";
import { AlEquipo } from "./pages/AlEquipo";
import { Cielo, Guardapolvo, MovimientoSwitch, TemaSwitch, Techo, type Sistema } from "./components/ambiente";
import { BanderaSwitch, LangSwitch, Logo } from "./components/marca";
import { Bienvenida, type BookLinks } from "./pages/Bienvenida";
import { Avisos } from "./components/avisos";
import { Campana } from "./components/campana";
import { Dibujo, Hoja, useTelefono, type Altura } from "./components/navegacion";
import "./styles.css";

/**
 * The app's frame: setup until the GitHub App exists, the welcome page until you sign in, then the menu and pages.
 */
function Shell() {
  const [state, setState] = useState<{ login?: string; role?: MemberRole; org?: Org; operator?: boolean; demo?: boolean; cluster?: boolean; buzon?: boolean; sistema?: Sistema; configured?: boolean; book?: BookLinks; activity?: boolean; loading: boolean }>({ loading: true });
  const telefono = useTelefono();
  const [altura, setAltura] = useState<Altura>(0);

  useEffect(() => {
    (async () => {
      const setup = await api.setupStatus().catch(() => ({ configured: false, book: undefined, activity: false }));
      if (!setup.configured) return setState({ configured: false, loading: false });
      try {
        const me = await api.me();
        session.operator = me.operator;
        session.role = me.role;
        session.cluster = !!me.cluster;
        session.mercado = !!me.mercado;
        session.appPrefix = me.appPrefix ?? "";
        setState({ login: me.login, role: me.role, org: me.org, operator: me.operator, demo: me.demo, cluster: me.cluster, buzon: me.buzon,
          sistema: { version: me.version, modo: me.modo, os: me.os, arch: me.arch }, configured: true, loading: false });
      } catch (e) {
        if (!(e instanceof ApiError && e.status === 401)) throw e;
        // GitHub brings the person back here once they chose the repositories (the GitHub App's setup URL, with
        // setup_action): signing in is what follows, so it starts without asking.
        if (new URLSearchParams(location.search).has("setup_action")) return location.replace("/api/auth/login");
        setState({ configured: true, book: setup.book, activity: setup.activity, loading: false });
      }
    })();
  }, []);

  if (state.loading) return <div className="center muted">{t("Loading…")}</div>;

  if (!state.configured) {
    return (
      <Routes>
        <Route path="*" element={<Setup />} />
      </Routes>
    );
  }

  // The public form to write to the team, signed in or not.
  if (location.pathname === "/comentarios") return <ComentarioPublico />;

  if (!state.login) return <Bienvenida book={state.book ?? {}} activity={!!state.activity} />;

  // The pages and the account: in the top bar on a wide screen; on a phone,
  // in the sheet that peeks from the bottom (components/navegacion).
  const paginas = (
    <>
      <NavLink to="/" end className="nav-link"><Dibujo de="apps" />{t("Apps")}</NavLink>
      <NavLink to="/universe" className="nav-link"><Dibujo de="universo" />{t("Universe")}</NavLink>
      {state.operator && state.cluster && <NavLink to="/services" className="nav-link"><Dibujo de="servicios" />{t("Services")}</NavLink>}
      {state.operator && state.cluster && <NavLink to="/addons" className="nav-link"><Dibujo de="complementos" />{t("Add-ons")}</NavLink>}
      {state.operator && state.cluster && <EnvironmentNavLink dibujo={<Dibujo de="entorno" />} />}
      <NavLink to="/ruta" className="nav-link"><Dibujo de="ruta" />{t("Ruta")}</NavLink>
      {state.role && state.role !== "guest" && <NavLink to="/bitacora" className="nav-link"><Dibujo de="bitacora" />{t("Bitácora")}</NavLink>}
      <NavLink to="/ia" className="nav-link"><Dibujo de="ia" />{t("AI")}</NavLink>
      {state.operator && <ProblemsNavLink dibujo={<Dibujo de="problemas" />} />}
      {state.buzon && <NavLink to="/buzon" className="nav-link"><Dibujo de="comentarios" />{t("Comments")}</NavLink>}
    </>
  );
  const cuenta = (
    <>
      {state.role === "admin" && <Link to="/new" className="btn primary">{t("New app")}</Link>}
      <OrgSwitch current={state.org} />
      {state.demo
        ? <span className="badge demo-badge" title={t("You are seeing a demo: you can look at everything, and change nothing.")}>{t("Read-only demo")}</span>
        : <NavLink to="/members" className="nav-link small" title={t(memberRoleLabel[state.role ?? "guest"])}>{state.login}</NavLink>}
      <TemaSwitch />
      <MovimientoSwitch />
      <LangSwitch />
      <BanderaSwitch />
      <button onClick={() => api.logout().then(() => location.assign("/"))}>{t("Sign out")}</button>
    </>
  );

  return (
    <>
      <Cielo />
      <Techo />
      <header className="topbar">
        <Link to="/" className="brand"><Logo /> <span className="brand-name">rendimiento</span></Link>
        {!telefono && paginas}
        <span className="spacer" />
        <Campana role={state.role} operator={state.operator} buzon={state.buzon} />
        {telefono
          ? <button type="button" className="topbar-yo" title={t("Your account and preferences")} onClick={() => setAltura(altura === 2 ? 0 : 2)}>{(state.login ?? "?").slice(0, 1)}<span className="sr-only">{t("Your account and preferences")}</span></button>
          : cuenta}
      </header>
      {telefono && <Hoja altura={altura} poner={setAltura} paginas={paginas} cuenta={cuenta} />}
      <main>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/new" element={<NewApp />} />
          <Route path="/mercado" element={<Mercado />} />
          <Route path="/mercado/nuevo" element={<PuestoEditor />} />
          <Route path="/mercado/:app" element={<PuestoEditor />} />
          <Route path="/environment" element={<Environment />} />
          <Route path="/universe" element={<Universe />} />
          <Route path="/universe/requests/:id" element={<RequestPage />} />
          <Route path="/services" element={<Services />} />
          <Route path="/addons" element={<Addons />} />
          <Route path="/problems" element={<Problems />} />
          <Route path="/ruta" element={<Ruta />} />
          <Route path="/bitacora" element={<Bitacora />} />
          <Route path="/senales" element={<SenalesPage />} />
          <Route path="/ia" element={<IA />} />
          <Route path="/buzon" element={<Buzon />} />
          <Route path="/escribir" element={<AlEquipo />} />
          <Route path="/members" element={<Members me={state.login ?? ""} role={state.role ?? "guest"} org={state.org} />} />
          <Route path="/addons/:name" element={<Addons />} />
          <Route path="/services/:ns/:name" element={<Services />} />
          <Route path="/apps/:name" element={<AppPage />} />
          <Route path="/apps/:name/:tab" element={<AppPage />} />
          <Route path="/apps/:name/runs/:id" element={<RunPage />} />
          <Route path="/setup" element={<Navigate to="/" />} />
          <Route path="*" element={<div className="empty">{t("Page not found.")} <Link to="/">{t("Go home")}</Link></div>} />
        </Routes>
      </main>
      <Avisos />
      <Guardapolvo sistema={state.sistema} />
      {telefono && <div className="menu-hoja-espacio" />}
    </>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <Shell />
    </BrowserRouter>
  </StrictMode>,
);
