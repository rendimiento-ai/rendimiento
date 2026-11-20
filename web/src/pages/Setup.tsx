import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { api, forjasApi, type Forja, type InstallState, type Member } from "../api";
import { ErrorBox } from "../components/ui";
import { EsqRenglones } from "../components/esqueleto";
import { forjaDeLogin, forjaName, forjaSello, forjasDeEntrada, loginEn, type ForjaDeEntrada } from "../forja";
import { t } from "../i18n";

const pasos = [
  "The setup token",
  "Where your code lives",
  "Who administers",
];

/** The three steps on the left: done, at hand, or waiting; the second says which forja was connected. */
function Pasos({ actual, conectada }: { actual: number; conectada?: string }) {
  return (
    <aside className="ld-lista">
      {pasos.map((p, i) => (
        <div key={p} className={`ld-item setup-paso${i === actual ? " on" : ""}${i < actual ? " hecho" : ""}`} aria-current={i === actual ? "step" : undefined}>
          <span className="setup-num" aria-hidden="true">{i < actual ? "✓" : i + 1}</span>
          <span className="ld-nombre">{i === 1 && conectada && i < actual ? t("{forja} connected", { forja: conectada }) : t(p)}</span>
        </div>
      ))}
    </aside>
  );
}

/**
 * First-run page, in the style of New app: the three steps on the left, and on the right the one at hand. The setup token
 * opens an install session; then the person picks where the code lives (GitHub through its App's manifest, a GitLab or a
 * Bitbucket workspace with an OAuth application) and authorizes it; they come back signed in as the first administrator
 * (PrimerosAdmins). With GitHub, the App is created first and they sign in with it after.
 */
export function Setup() {
  const [status, setStatus] = useState<{ tokenCommand?: string; install?: InstallState }>();
  const [err, setErr] = useState<unknown>();
  const load = () => api.setupStatus().then((s) => {
    setStatus(s);
    // GitHub brings the person back here once they chose the App's repositories (its setup URL, with setup_action):
    // signing in, as the first administrator, is what follows.
    if (s.install?.session && s.install.github && new URLSearchParams(location.search).has("setup_action")) location.replace("/api/auth/login");
  }, setErr);
  useEffect(() => { load(); }, []);
  const install = status?.install;
  const actual = !install?.session ? 0 : install.github ? 2 : 1;
  return (
    <main className="setup">
      <h1>{t("Set up rendimiento.ai")}</h1>
      <p className="sub" style={{ marginBottom: 0 }}>{t("One-time step: connect where your code lives, so rendimiento reads your repos, runs CI on every push and opens change requests, and say who administers it.")}</p>
      <ErrorBox error={err} />
      <div className="lista-detalle">
        <Pasos actual={actual} conectada={install?.github ? "GitHub" : undefined} />
        <section className="ld-detalle">
          {!status && err === undefined && <EsqRenglones n={6} />}
          {(status || err !== undefined) && actual === 0 && <PasoFicha command={status?.tokenCommand ?? ""} onDone={load} />}
          {actual === 1 && install && <PasoForja install={install} />}
          {actual === 2 && (
            <div className="stack">
              <h3 className="ld-h3">{t("Who administers")}</h3>
              <p style={{ margin: 0 }}>{t("The GitHub App is ready. Sign in with GitHub: you come in first, as administrator, and then add the others.")}</p>
              <div className="setup-pie"><a className="btn primary big" href="/api/auth/login">{t("Sign in with GitHub")}</a></div>
            </div>
          )}
        </section>
      </div>
    </main>
  );
}

/** Step 1: the setup token, checked by the server; a good one opens the install session in this browser. */
function PasoFicha({ command, onDone }: { command: string; onDone: () => void }) {
  const [token, setToken] = useState(new URLSearchParams(location.search).get("token") ?? "");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const send = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setErr(undefined);
    api.setupToken(token).then(() => { setBusy(false); history.replaceState(null, "", location.pathname); onDone(); }, (e) => { setBusy(false); setErr(e); });
  };
  return (
    <form className="stack" onSubmit={send}>
      <h3 className="ld-h3">{t("1. The setup token")}</h3>
      <p style={{ margin: "0 0 8px" }}>{t("Paste the setup token. Get it with:")}</p>
      <pre className="file">{command || "…"}</pre>
      <label className="field">
        <span>{t("Setup token")}</span>
        <input value={token} onChange={(e) => setToken(e.target.value)} placeholder={t("paste token")} autoComplete="off" />
      </label>
      <p className="small muted" style={{ margin: 0 }}>{t("It is the key: without it nobody can pick a forja or become an administrator.")}</p>
      <ErrorBox error={err} />
      <div className="setup-pie"><button className="primary big" type="submit" disabled={busy || !token.trim()}>{t("Continue")}</button></div>
    </form>
  );
}

const opciones: { forja: Forja; text: string }[] = [
  { forja: "github", text: "The GitHub App is created with one click (its manifest)." },
  { forja: "gitlab", text: "gitlab.com or your own, with an OAuth application of your group." },
  { forja: "bitbucket", text: "Bitbucket Cloud, with an OAuth consumer of your workspace." },
];

/** Step 2: where the code lives, three cards; the one chosen shows what it needs. */
function PasoForja({ install }: { install: InstallState }) {
  const [forja, setForja] = useState<Forja>("github");
  const puede = (f: Forja) => f === "github" || (f === "gitlab" ? install.canGitLab : install.canBitbucket);
  return (
    <div className="stack">
      <h3 className="ld-h3">{t("Where does your code live?")}</h3>
      <p style={{ margin: 0 }}>{t("Pick one to start. The first administrators come in with it; you can connect the others later from Environment.")}</p>
      <div className="roles-elegir" role="radiogroup" aria-label={t("Where your code lives")}>
        {opciones.map((o) => (
          <button key={o.forja} type="button" role="radio" aria-checked={forja === o.forja} className={`rol-tarjeta${forja === o.forja ? " on" : ""}`}
            disabled={!puede(o.forja)} onClick={() => setForja(o.forja)}>
            <span className="row" style={{ gap: 8 }}>
              <span className={`forja-sello ${o.forja}`} aria-hidden="true">{forjaSello(o.forja)}</span>
              <strong>{forjaName(o.forja)}</strong>
            </span>
            <span className="small muted">{puede(o.forja) ? t(o.text) : t("This platform cannot connect it.")}</span>
          </button>
        ))}
      </div>
      {forja === "github" && (
        <>
          <p style={{ margin: 0 }}>{t("GitHub opens and asks you to create the app. The name and permissions are already filled in. Then pick which repos it can see; you can change this later.")}</p>
          <div className="setup-pie"><a className="btn primary big" href="/api/setup/github">{t("Create GitHub App")}</a></div>
        </>
      )}
      {forja === "gitlab" && <ConGitLab callback={install.gitlabCallback} />}
      {forja === "bitbucket" && <ConBitbucket callback={install.bitbucketCallback} />}
    </div>
  );
}

/** What to set on the forja's side, beside the form. */
function Instrucciones({ children }: { children: ReactNode }) {
  return <div className="card small stack" style={{ margin: 0 }}>{children}</div>;
}

/** The address to copy into the forja, with a button that copies it. */
function Copiar({ value, label }: { value: string; label: string }) {
  const [copiado, setCopiado] = useState(false);
  return (
    <div className="row" style={{ gap: 8 }}>
      <input readOnly value={value} onFocus={(e) => e.target.select()} aria-label={label} className="mono" />
      <button type="button" onClick={() => navigator.clipboard?.writeText(value).then(() => setCopiado(true), () => setCopiado(false))}>{copiado ? t("Copied") : t("Copy")}</button>
    </div>
  );
}

/** Step 2 with a GitLab: its address, the OAuth application's id and secret; GitLab then asks to authorize it. */
function ConGitLab({ callback }: { callback: string }) {
  const [baseUrl, setBaseUrl] = useState("https://gitlab.com");
  const [appId, setAppId] = useState("");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const send = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setErr(undefined);
    api.setupGitLab(baseUrl, appId, secret).then((r) => { location.href = r.authorizeUrl; }, (e) => { setBusy(false); setErr(e); });
  };
  return (
    <form className="stack" onSubmit={send}>
      <div className="form-grid">
        <label className="field"><span>{t("Address")}</span>
          <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} inputMode="url" autoComplete="off" />
        </label>
        <label className="field"><span>{t("Application ID")}</span>
          <input value={appId} onChange={(e) => setAppId(e.target.value)} autoComplete="off" required />
        </label>
        <label className="field"><span>{t("Secret")}</span>
          <input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} autoComplete="new-password" required />
        </label>
      </div>
      <Instrucciones>
        <strong>{t("On GitLab: Settings → Applications")}</strong>
        <span>{t("Redirect URI (copy it as is):")}</span>
        <Copiar value={callback} label={t("Redirect URI")} />
        <span>{t("Confidential: yes. Scopes: api and read_user.")}</span>
      </Instrucciones>
      <ErrorBox error={err} />
      <div className="setup-pie"><button className="primary big" type="submit" disabled={busy || !appId || !secret}>{busy ? t("Connecting…") : t("Connect GitLab and continue")}</button></div>
    </form>
  );
}

/** Step 2 with Bitbucket: the workspace, the OAuth consumer's key and secret; Bitbucket then asks to authorize it. */
function ConBitbucket({ callback }: { callback: string }) {
  const [workspace, setWorkspace] = useState("");
  const [key, setKey] = useState("");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const send = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setErr(undefined);
    api.setupBitbucket(workspace, key, secret).then((r) => { location.href = r.authorizeUrl; }, (e) => { setBusy(false); setErr(e); });
  };
  return (
    <form className="stack" onSubmit={send}>
      <div className="form-grid">
        <label className="field"><span>{t("Workspace")}</span>
          <input value={workspace} onChange={(e) => setWorkspace(e.target.value)} placeholder="my-team" autoComplete="off" required />
        </label>
        <label className="field"><span>{t("Key")}</span>
          <input value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" required />
        </label>
        <label className="field"><span>{t("Secret")}</span>
          <input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} autoComplete="new-password" required />
        </label>
      </div>
      <Instrucciones>
        <strong>{t("On Bitbucket: the workspace's Settings → OAuth consumers → Add consumer")}</strong>
        <span>{t("Callback URL (copy it as is):")}</span>
        <Copiar value={callback} label={t("Callback URL")} />
        <span>{t("Private consumer: yes. Permissions: Account (read), Repositories (write), Pull requests (write) and Webhooks (read and write).")}</span>
      </Instrucciones>
      <ErrorBox error={err} />
      <div className="setup-pie"><button className="primary big" type="submit" disabled={busy || !workspace || !key || !secret}>{busy ? t("Connecting…") : t("Connect Bitbucket and continue")}</button></div>
    </form>
  );
}

/**
 * Step 3, signed in: the person who authorized the forja came in as the first administrator. They add the other
 * administrators now (by forja and username) or later in Members, and go in.
 */
export function PrimerosAdmins({ me }: { me: string }) {
  const navigate = useNavigate();
  const [members, setMembers] = useState<Member[]>();
  const [forjas, setForjas] = useState<ForjaDeEntrada[]>([]);
  const [elegida, setElegida] = useState(0);
  const [user, setUser] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  useEffect(() => {
    api.members().then(setMembers, setErr);
    forjasApi.identities().then((ids) => setForjas(forjasDeEntrada(ids)), setErr);
  }, []);
  const admins = (members ?? []).filter((m) => m.role === "admin");
  const f = forjas[elegida];
  const login = f ? loginEn(f, user) : "";
  const yo = (l: string) => l.toLowerCase() === me.toLowerCase();
  const act = (p: Promise<unknown>) => {
    setBusy(true); setErr(undefined);
    p.then(() => api.members()).then((ms) => { setMembers(ms); setBusy(false); setUser(""); }, (e) => { setBusy(false); setErr(e); });
  };
  const mia = forjaDeLogin(me);
  return (
    <main className="setup">
      <h1>{t("The first administrators")}</h1>
      <div className="lista-detalle">
        <Pasos actual={2} conectada={forjas.find((x) => x.forja === mia)?.label ?? forjaName(mia)} />
        <section className="ld-detalle">
          <h3 className="ld-h3">{t("Who administers")}</h3>
          <p style={{ margin: 0 }}>{t("You come in first, with the account you just authorized. Add the other administrators now or later, in Members.")}</p>
          {!members && !err && <EsqRenglones n={2} />}
          <ul className="setup-admins">
            {admins.map((m) => (
              <li key={m.login}>
                <span className={`forja-sello ${forjaDeLogin(m.login)}`} aria-hidden="true">{forjaSello(forjaDeLogin(m.login))}</span>
                <strong className="mono">{m.login}</strong>
                {yo(m.login) ? <span className="muted small">{t("You · administrator")}</span>
                  : <button type="button" disabled={busy} onClick={() => act(api.removeMember(m.login))}>{t("Remove")}</button>}
              </li>
            ))}
          </ul>
          <form className="form-grid" onSubmit={(e) => { e.preventDefault(); if (login) act(api.setMember(login, "admin")); }}>
            <label className="field"><span>{t("Forja")}</span>
              <select value={elegida} onChange={(e) => setElegida(Number(e.target.value))}>
                {forjas.map((x, i) => <option key={`${x.forja}-${x.host}`} value={i}>{x.label}</option>)}
              </select>
            </label>
            <label className="field"><span>{t("Username")}</span>
              <input value={user} onChange={(e) => setUser(e.target.value)} autoComplete="off" placeholder="luis" />
            </label>
            <div style={{ alignSelf: "end" }}><button type="submit" disabled={busy || !login}>{t("Add")}</button></div>
          </form>
          {login && <p className="small muted" style={{ margin: 0 }}>{t("They will come in as {login}.", { login })}</p>}
          <ErrorBox error={err} />
          <div className="setup-pie"><button className="primary big" onClick={() => navigate("/", { replace: true })}>{t("Finish and go in")}</button></div>
          <div className="small stack">
            <div><strong>{t("How each one is written")}</strong>
              <p style={{ margin: "2px 0 0" }}>{t("GitHub: ana. GitLab: ana@gitlab.com or ana@git.example.com. Bitbucket: ana@bitbucket.org.")}</p></div>
            <div><strong>{t("With ALLOWED_USERS")}</strong>
              <p style={{ margin: "2px 0 0" }}>{t("Whoever installs with Helm can keep naming them there, the same way: ALLOWED_USERS=ana@gitlab.com,luis@gitlab.com. It only counts while there are no members.")}</p></div>
            <div><strong>{t("What does not change")}</strong>
              <p style={{ margin: "2px 0 0" }}>{t("The setup token is still the key: without it nobody can pick a forja or become an administrator.")}</p></div>
          </div>
        </section>
      </div>
    </main>
  );
}
