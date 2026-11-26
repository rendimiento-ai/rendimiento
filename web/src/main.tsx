import { StrictMode, Suspense, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Link, NavLink, Navigate, Route, Routes } from "react-router-dom";
import { api, ApiError, session, type Forja, type MemberRole, type Org } from "./api";
import { MenuCuenta, OrgSwitch, memberRoleLabel } from "./components/cuenta";
import { Buscador, type PaginaBuscable } from "./components/buscador";
import { t } from "./i18n";
import { Dashboard } from "./pages/Dashboard";
import { PrimerosAdmins, Setup } from "./pages/Setup";
import { ProblemsNavLink } from "./components/enlace-problemas";
import { useUso } from "./uso";
import { EnvironmentNavLink } from "./pages/Version";
import { ComentarioPublico } from "./pages/ComentarioPublico";
import { Cielo, Guardapolvo, MovimientoSwitch, TemaSwitch, Techo, type Sistema } from "./components/ambiente";
import { BanderaSwitch, LangSwitch, Logo, Semilla } from "./components/marca";
import { Bienvenida, type BookLinks } from "./pages/Bienvenida";
import { Avisos } from "./components/avisos";
import { Campana } from "./components/campana";
import { Dibujo, Hoja, useTelefono, type Altura } from "./components/navegacion";
import { FallaFrontera, NoExiste, NoResponde, claseDe } from "./components/falla";
import { Cargando } from "./components/cargando";
import { perezosa } from "./components/perezosa";
import { AvisoConexion } from "./components/conexion-aviso";
import { AvisoSueno, BotonDormir } from "./components/dormir";
import { Ayudante } from "./components/ayudante";
import { olvidarGuardado, quienEs, registrarTrabajador } from "./trabajador/registro";
import "./styles.css";

registrarTrabajador(); // the page opens without a connection, from what this browser kept

// The first screen (setup, welcome, Apps, the public form) comes with the
// shell; every other page loads when it is opened.
const UsoPage = perezosa(() => import("./pages/Uso").then((m) => m.UsoPage));
const Numeros = perezosa(() => import("./pages/Numeros").then((m) => m.Numeros));
const Registro = perezosa(() => import("./pages/Registro").then((m) => m.Registro));
const NewApp = perezosa(() => import("./pages/NewApp").then((m) => m.NewApp));
const AppPage = perezosa(() => import("./pages/AppPage").then((m) => m.AppPage));
const RunPage = perezosa(() => import("./pages/RunPage").then((m) => m.RunPage));
const Environment = perezosa(() => import("./pages/Environment").then((m) => m.Environment));
const Llaves = perezosa(() => import("./pages/Llaves").then((m) => m.Llaves));
const Services = perezosa(() => import("./pages/Services").then((m) => m.Services));
const Universe = perezosa(() => import("./pages/Universe").then((m) => m.Universe));
const RequestPage = perezosa(() => import("./pages/Request").then((m) => m.RequestPage));
const Addons = perezosa(() => import("./pages/Addons").then((m) => m.Addons));
const Problems = perezosa(() => import("./pages/Problems").then((m) => m.Problems));
const Mercado = perezosa(() => import("./pages/Mercado").then((m) => m.Mercado));
const PuestoEditor = perezosa(() => import("./pages/Mercado").then((m) => m.PuestoEditor));
const Ruta = perezosa(() => import("./pages/Ruta").then((m) => m.Ruta));
const Bitacora = perezosa(() => import("./pages/Bitacora").then((m) => m.Bitacora));
const SenalesPage = perezosa(() => import("./pages/SenalesPage").then((m) => m.SenalesPage));
const IA = perezosa(() => import("./pages/IA").then((m) => m.IA));
const Buzon = perezosa(() => import("./pages/Buzon").then((m) => m.Buzon));
const AlEquipo = perezosa(() => import("./pages/AlEquipo").then((m) => m.AlEquipo));
type SignIn = { forja: Forja; id: number; host: string };
const Cuentas = perezosa(() => import("./pages/Cuentas").then((m) => m.Cuentas));
const Members = perezosa(() => import("./pages/Members").then((m) => m.Members));

/**
 * The app's frame: the setup page until the platform is installed (a forja and its first administrator), the welcome page
 * until you sign in, then the menu and pages.
 */
function Shell() {
  const [state, setState] = useState<{ login?: string; role?: MemberRole; org?: Org; operator?: boolean; demo?: boolean; cluster?: boolean; buzon?: boolean; sistema?: Sistema; configured?: boolean; book?: BookLinks; activity?: boolean; signIn?: SignIn[]; github?: boolean; loading: boolean; sinRespuesta?: boolean }>({ loading: true });
  const telefono = useTelefono();
  const [altura, setAltura] = useState<Altura>(0);
  useUso(!!state.login);

  useEffect(() => {
    (async () => {
      // Nothing answers and nothing was kept: say so (it comes back by itself), not the setup page.
      const setup = await api.setupStatus().catch((e) => (claseDe(e) === "unreachable" ? null : { configured: false, book: undefined, activity: false, signIn: undefined, github: false }));
      if (!setup) return setState({ loading: false, sinRespuesta: true });
      if (!setup.configured) return setState({ configured: false, loading: false });
      try {
        const me = await api.me();
        quienEs(me.login, me.org?.name);
        session.operator = me.operator;
        session.role = me.role;
        session.cluster = !!me.cluster;
        session.mercado = !!me.mercado;
        session.appPrefix = me.appPrefix ?? "";
        session.demo = !!me.demo;
        setState({ login: me.login, role: me.role, org: me.org, operator: me.operator, demo: me.demo, cluster: me.cluster, buzon: me.buzon,
          sistema: { version: me.version, modo: me.modo, os: me.os, arch: me.arch }, configured: true, loading: false });
      } catch (e) {
        if (claseDe(e) === "unreachable") return setState({ loading: false, sinRespuesta: true });
        if (!(e instanceof ApiError && e.status === 401)) throw e;
        // GitHub brings the person back here once they chose the repositories (the GitHub App's setup URL, with
        // setup_action): signing in is what follows, so it starts without asking.
        if (new URLSearchParams(location.search).has("setup_action")) return location.replace("/api/auth/login");
        setState({ configured: true, book: setup.book, activity: setup.activity, signIn: setup.signIn, github: setup.github, loading: false });
      }
    })();
  }, []);

  if (state.loading) return <div className="center muted">{t("Loading…")}</div>;
  if (state.sinRespuesta) return <main><NoResponde /></main>;

  if (!state.configured) {
    return (
      <Routes>
        <Route path="*" element={<Setup />} />
      </Routes>
    );
  }

  // The public form to write to the team, signed in or not.
  if (location.pathname === "/comentarios") return <ComentarioPublico />;

  if (!state.login) return <Bienvenida book={state.book ?? {}} activity={!!state.activity} signIn={state.signIn ?? []} github={state.github} />;

  // The pages and the account: in the top bar on a wide screen; on a phone,
  // in the sheet that peeks from the bottom (components/navegacion).
  const salir = () => { olvidarGuardado(); api.logout().then(() => location.assign("/")); };
  const paginas = (
    <>
      <NavLink to="/" end className="nav-link"><Dibujo de="apps" />{t("Apps")}</NavLink>
      <NavLink to="/universe" className="nav-link"><Dibujo de="universo" />{t("Universe")}</NavLink>
      <NavLink to="/numeros" className="nav-link"><Dibujo de="numeros" />{t("Numbers")}</NavLink>
      {state.operator && state.cluster && <NavLink to="/services" className="nav-link"><Dibujo de="servicios" />{t("Services")}</NavLink>}
      {state.operator && state.cluster && <NavLink to="/addons" className="nav-link"><Dibujo de="complementos" />{t("Add-ons")}</NavLink>}
      {state.operator && (state.cluster || state.sistema?.modo === "local") && <EnvironmentNavLink dibujo={<Dibujo de="entorno" />} />}
      <NavLink to="/ruta" className="nav-link"><Dibujo de="ruta" />{t("Ruta")}</NavLink>
      {state.role && state.role !== "guest" && <NavLink to="/bitacora" className="nav-link"><Dibujo de="bitacora" />{t("Bitácora")}</NavLink>}
      <NavLink to="/ia" className="nav-link"><Dibujo de="ia" />{t("AI")}</NavLink>
      {state.operator && <ProblemsNavLink dibujo={<Dibujo de="problemas" />} />}
      {state.buzon && <NavLink to="/buzon" className="nav-link"><Dibujo de="comentarios" />{t("Comments")}</NavLink>}
    </>
  );
  const cuenta = (
    <>
      {state.role === "admin" && <Link to="/new" className="btn primary"><Semilla />{t("New app")}</Link>}
      <OrgSwitch current={state.org} />
      {state.demo
        ? <span className="badge demo-badge" title={t("You are seeing a demo: you can look at everything, and change nothing.")}>{t("Read-only demo")}</span>
        : <NavLink to="/members" className="nav-link small" title={t(memberRoleLabel[state.role ?? "guest"])}>{state.login}</NavLink>}
      <TemaSwitch />
      <MovimientoSwitch />
      <LangSwitch />
      <BanderaSwitch />
      <button onClick={salir}>{t("Sign out")}</button>
    </>
  );
  // The same pages, by name, for the search; and a few more one can look for.
  const muestra = {
    servicios: !!(state.operator && state.cluster),
    entorno: !!(state.operator && (state.cluster || state.sistema?.modo === "local")),
    bitacora: !!(state.role && state.role !== "guest"),
  };
  const buscables: PaginaBuscable[] = [
    { to: "/", nombre: t("Apps") },
    { to: "/universe", nombre: t("Universe") },
    { to: "/numeros", nombre: t("Numbers") },
    ...(muestra.servicios ? [{ to: "/services", nombre: t("Services") }, { to: "/addons", nombre: t("Add-ons") }] : []),
    ...(muestra.entorno ? [{ to: "/environment", nombre: t("Environment") }] : []),
    { to: "/ruta", nombre: t("Ruta") },
    ...(muestra.bitacora ? [{ to: "/bitacora", nombre: t("Bitácora") }] : []),
    { to: "/ia", nombre: t("AI") },
    ...(state.operator ? [{ to: "/problems", nombre: t("Problems") }] : []),
    ...(state.buzon ? [{ to: "/buzon", nombre: t("Comments") }] : []),
    ...(state.role === "admin" ? [{ to: "/new", nombre: t("New app") }] : []),
    { to: "/members", nombre: t("Members") },
    { to: "/escribir", nombre: t("My comments") },
  ];

  return (
    <>
      <Cielo />
      <Techo />
      {/* On a wide screen two rows: the brand, the search in the middle of the bar, and the bell, New app and the
          account's menu at the end; under them every page, centered. On a phone one short row, and the pages and the
          account in the sheet. */}
      <header className="topbar">
        <div className="topbar-fila">
          <Link to="/" className="brand"><Logo /> <span className="brand-name">rendimiento</span></Link>
          {!telefono && <Buscador paginas={buscables} />}
          <div className="topbar-acciones">
            {telefono && <Buscador paginas={buscables} telefono />}
            {/* Sleep is for rendimiento on a computer; a cluster stays awake (components/dormir). */}
            {state.sistema?.modo === "local" && <BotonDormir operator={!!state.operator} telefono={telefono} />}
            <Campana role={state.role} operator={state.operator} buzon={state.buzon} />
            {telefono
              ? <button type="button" className="topbar-yo" title={t("Your account and preferences")} onClick={() => setAltura(altura === 2 ? 0 : 2)}>{(state.login ?? "?").slice(0, 1)}<span className="sr-only">{t("Your account and preferences")}</span></button>
              : <>
                {state.role === "admin" && <Link to="/new" className="btn primary" title={t("New app")}><Semilla /><span className="nueva-app-texto">{t("New app")}</span></Link>}
                <MenuCuenta login={state.login} role={state.role} org={state.org} demo={state.demo} salir={salir} />
              </>}
          </div>
        </div>
        {!telefono && <nav className="topbar-secciones" aria-label={t("Pages")}>{paginas}</nav>}
      </header>
      {telefono && <Hoja altura={altura} poner={setAltura} paginas={paginas} cuenta={cuenta} />}
      <main>
        <AvisoConexion />
        {state.sistema?.modo === "local" && <AvisoSueno operator={!!state.operator} />}
        <FallaFrontera version={state.sistema?.version} org={state.org?.name}>
        <Suspense fallback={<Cargando />}>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/new" element={<NewApp />} />
          <Route path="/mercado" element={<Mercado />} />
          <Route path="/mercado/nuevo" element={<PuestoEditor />} />
          <Route path="/mercado/:app" element={<PuestoEditor />} />
          <Route path="/environment" element={<Environment />} />
          <Route path="/environment/llaves" element={<Llaves />} />
          <Route path="/universe" element={<Universe />} />
          <Route path="/universe/requests/:id" element={<RequestPage />} />
          <Route path="/services" element={<Services />} />
          <Route path="/addons" element={<Addons />} />
          <Route path="/problems" element={<Problems />} />
          <Route path="/registro" element={<Registro />} />
          <Route path="/ruta" element={<Ruta me={state.login ?? ""} />} />
          <Route path="/bitacora" element={<Bitacora />} />
          <Route path="/senales" element={<SenalesPage />} />
          <Route path="/uso" element={<UsoPage />} />
          <Route path="/numeros" element={<Numeros />} />
          <Route path="/ia" element={<IA />} />
          <Route path="/buzon" element={<Buzon />} />
          <Route path="/escribir" element={<AlEquipo />} />
          <Route path="/cuentas" element={<Cuentas />} />
          <Route path="/members" element={<Members me={state.login ?? ""} role={state.role ?? "guest"} org={state.org} />} />
          <Route path="/addons/:name" element={<Addons />} />
          <Route path="/services/:ns/:name" element={<Services />} />
          <Route path="/apps/:name" element={<AppPage />} />
          <Route path="/apps/:name/:tab" element={<AppPage />} />
          <Route path="/apps/:name/runs/:id" element={<RunPage />} />
          <Route path="/setup" element={<Navigate to="/" />} />
          <Route path="/instalacion" element={<PrimerosAdmins me={state.login ?? ""} />} />
          <Route path="*" element={<NoExiste />} />
        </Routes>
        </Suspense>
        </FallaFrontera>
      </main>
      <Ayudante />
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
