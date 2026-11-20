// «Where your code lives», on the Environment page (Ruta 102): GitHub, through its App, the GitLabs connected here
// (gitlab.com or one's own), and Bitbucket Cloud workspaces.
import { useState, type FormEvent } from "react";
import { forjasApi, session, type BitbucketConnection, type GitLabConnection } from "../api";
import { ErrorBox, usePoll } from "./ui";
import { t, tn } from "../i18n";
import { useConexion } from "../conexion";
import { apagado } from "./conexion-aviso";

/** The section: one card per forja, and who signs in. */
export function Forjas() {
  const { data, error, setData } = usePoll(() => forjasApi.list(), [], 60000);
  if (error) return <ErrorBox error={error} />;
  if (!data) return null;
  const reload = () => forjasApi.list().then(setData, () => {});
  return (
    <section className="card stack forjas" id="forjas">
      <div>
        <small className="muted">{t("Environment / Code")}</small>
        <h2 style={{ margin: 0 }}>{t("Where your code lives")}</h2>
        <p className="small muted" style={{ margin: 0 }}>{t("rendimiento reads your repositories, runs each push and opens change requests from here. You can connect more than one; each app says which is its own.")}</p>
      </div>
      <div className="forjas-tarjetas">
        <article className="forja-tarjeta">
          <div className="forja-cabeza">
            <span className="forja-sello github" aria-hidden="true">GH</span>
            <strong>GitHub</strong>
            {data.github.configured ? <span className="badge ok">{t("Connected")}</span> : <span className="badge warn">{t("Not connected")}</span>}
          </div>
          {data.github.configured ? (
            <>
              <p className="small" style={{ margin: 0 }}>{t("With the GitHub App «{slug}».", { slug: data.github.slug ?? "" })}</p>
              <div className="forja-datos small">
                <span>{t("Apps")} <b>{data.github.apps}</b></span>
                <span className="muted">{t("Push notices signed (HMAC)")}</span>
              </div>
              {data.github.htmlUrl && <a className="small" href={data.github.htmlUrl} target="_blank" rel="noreferrer">{t("Manage on GitHub")}</a>}
            </>
          ) : (
            <>
              <p className="small" style={{ margin: 0 }}>{t("With a GitHub App, created with one click (its manifest); then you pick which repositories it sees.")}</p>
              {session.operator && <div><a className="btn" href="/api/setup/github">{t("Connect GitHub")}</a></div>}
            </>
          )}
        </article>

        {data.gitlab.map((g) => <GitLabTarjeta key={g.id} g={g} onChange={reload} />)}
        <ConectarGitLab callback={data.callback} first={data.gitlab.length === 0} />

        {(data.bitbucket ?? []).map((b) => <BitbucketTarjeta key={`bitbucket-${b.id}`} b={b} onChange={reload} />)}
        <ConectarBitbucket callback={data.bitbucketCallback} first={false} />
      </div>
      <div className="forja-quien">
        <strong>{t("Who signs in")}</strong>
        <p className="small" style={{ margin: "4px 0 0" }}>{t("People sign in with any of the connected forjas. When the same person has accounts on several (GitHub, GitLab, Bitbucket), they link them from their account menu and are one person in rendimiento (their roles and their Ruta).")}</p>
      </div>
    </section>
  );
}

/** The warning's second line: who can no longer sign in once the forja goes (their only account is there). */
function pierden(members: string[]): string {
  return members.length === 0 ? "" : "\n\n" + t("They can no longer sign in, since their only account is there: {who}.", { who: members.join(", ") });
}

/** A GitLab connected here: its address, who rendimiento acts as, and its apps. */
function GitLabTarjeta({ g, onChange }: { g: GitLabConnection; onChange: () => void }) {
  const conexion = useConexion();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const authorize = () => {
    setBusy(true); setErr(undefined);
    forjasApi.authorizeGitLab(g.id).then((r) => { location.href = r.authorizeUrl; }, (e) => { setBusy(false); setErr(e); });
  };
  const remove = () => {
    setBusy(true); setErr(undefined);
    forjasApi.accesoGitLab(g.id).then((a) => {
      setBusy(false);
      if (a.admins.length > 0) return setErr(new Error(t("{who} administers here and signs in only with this GitLab. Link another account to them (Your accounts) or take away their role first.", { who: a.admins.join(", ") })));
      if (!confirm(t("Forget the GitLab at {host}? Its webhooks stop being heard.", { host: g.host }) + pierden(a.members))) return;
      setBusy(true);
      forjasApi.removeGitLab(g.id).then(onChange, (e) => { setBusy(false); setErr(e); });
    }, (e) => { setBusy(false); setErr(e); });
  };
  return (
    <article className="forja-tarjeta">
      <div className="forja-cabeza">
        <span className="forja-sello gitlab" aria-hidden="true">GL</span>
        <strong>GitLab</strong>
        {g.authorized ? <span className="badge ok">{t("Connected")}</span> : <span className="badge warn">{t("Waiting for authorization")}</span>}
      </div>
      <p className="small mono" style={{ margin: 0 }}>{g.baseUrl}</p>
      <p className="small" style={{ margin: 0 }}>
        {g.authorized ? t("rendimiento acts as @{user} there.", { user: g.username }) : t("Someone must authorize the OAuth application on GitLab before rendimiento can read its projects.")}
      </p>
      <div className="forja-datos small">
        <span>{tn(g.apps, "{n} app", "{n} apps")}</span>
        <span className="muted">{t("Push notices with a secret token")}</span>
      </div>
      <ErrorBox error={err} />
      {session.operator && (
        <div className="row">
          <button className={g.authorized ? "" : "primary"} {...apagado(conexion, busy)} onClick={authorize}>{g.authorized ? t("Authorize again") : t("Authorize on GitLab")}</button>
          {g.apps === 0 && <button {...apagado(conexion, busy)} onClick={remove}>{t("Remove")}</button>}
        </div>
      )}
    </article>
  );
}

/** Connecting a GitLab: its address and an OAuth application of yours; then GitLab asks you to authorize it. */
function ConectarGitLab({ callback, first }: { callback: string; first: boolean }) {
  const conexion = useConexion();
  const [open, setOpen] = useState(first);
  const [baseUrl, setBaseUrl] = useState("");
  const [appId, setAppId] = useState("");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  if (!session.operator) return null;
  const connect = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setErr(undefined);
    forjasApi.connectGitLab(baseUrl, appId, secret).then((r) => { location.href = r.authorizeUrl; }, (e) => { setBusy(false); setErr(e); });
  };
  if (!open) {
    return (
      <article className="forja-tarjeta">
        <div className="forja-cabeza">
          <span className="forja-sello gitlab" aria-hidden="true">GL</span>
          <strong>{t("Another GitLab")}</strong>
        </div>
        <p className="small" style={{ margin: 0 }}>{t("gitlab.com or your own, each with its own OAuth application.")}</p>
        <div><button onClick={() => setOpen(true)}>{t("Connect GitLab")}</button></div>
      </article>
    );
  }
  return (
    <form className="forja-tarjeta" onSubmit={connect}>
      <div className="forja-cabeza">
        <span className="forja-sello gitlab" aria-hidden="true">GL</span>
        <strong>GitLab</strong>
      </div>
      <p className="small" style={{ margin: 0 }}>{t("gitlab.com or your own. It uses an OAuth application of your group; rendimiento creates the push notices for you.")}</p>
      <label className="field">
        <span>{t("Address")}</span>
        <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://gitlab.com" inputMode="url" autoComplete="off" />
        <span className="small muted">{t("Your own GitLab? Write its address, for example https://git.example.com")}</span>
      </label>
      <details className="small">
        <summary>{t("How to create the OAuth application")}</summary>
        <ol className="forja-pasos">
          <li>{t("On GitLab, open your group's Settings → Applications (or your profile's Preferences → Applications).")}</li>
          <li>{t("Redirect URI:")} <code>{callback}</code></li>
          <li>{t("Scopes: api and read_user. Leave it confidential.")}</li>
          <li>{t("Save it, and copy its application ID and its secret here.")}</li>
        </ol>
      </details>
      <label className="field">
        <span>{t("Application ID")}</span>
        <input value={appId} onChange={(e) => setAppId(e.target.value)} autoComplete="off" required />
      </label>
      <label className="field">
        <span>{t("Secret")}</span>
        <input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} autoComplete="new-password" required />
        <span className="small muted">{t("It is kept with the platform's other secrets, never shown again.")}</span>
      </label>
      <ErrorBox error={err} />
      <div><button className="primary" type="submit" {...apagado(conexion, busy || !appId || !secret)}>{busy ? t("Connecting…") : t("Connect GitLab")}</button></div>
    </form>
  );
}

/** A Bitbucket workspace connected here: who rendimiento acts as, and its apps. */
function BitbucketTarjeta({ b, onChange }: { b: BitbucketConnection; onChange: () => void }) {
  const conexion = useConexion();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const authorize = () => {
    setBusy(true); setErr(undefined);
    forjasApi.authorizeBitbucket(b.id).then((r) => { location.href = r.authorizeUrl; }, (e) => { setBusy(false); setErr(e); });
  };
  const remove = () => {
    setBusy(true); setErr(undefined);
    forjasApi.accesoBitbucket(b.id).then((a) => {
      setBusy(false);
      if (a.admins.length > 0) return setErr(new Error(t("{who} administers here and signs in only with Bitbucket. Link another account to them (Your accounts) or take away their role first.", { who: a.admins.join(", ") })));
      if (!confirm(t("Forget the Bitbucket workspace {workspace}? Its webhooks stop being heard.", { workspace: b.workspace }) + pierden(a.members))) return;
      setBusy(true);
      forjasApi.removeBitbucket(b.id).then(onChange, (e) => { setBusy(false); setErr(e); });
    }, (e) => { setBusy(false); setErr(e); });
  };
  return (
    <article className="forja-tarjeta">
      <div className="forja-cabeza">
        <span className="forja-sello bitbucket" aria-hidden="true">BB</span>
        <strong>Bitbucket</strong>
        {b.authorized ? <span className="badge ok">{t("Connected")}</span> : <span className="badge warn">{t("Waiting for authorization")}</span>}
      </div>
      <p className="small mono" style={{ margin: 0 }}>bitbucket.org/{b.workspace}</p>
      <p className="small" style={{ margin: 0 }}>
        {b.authorized ? t("rendimiento acts as @{user} there.", { user: b.username }) : t("Someone must authorize the OAuth consumer on Bitbucket before rendimiento can read the workspace's repositories.")}
      </p>
      <div className="forja-datos small">
        <span>{tn(b.apps, "{n} app", "{n} apps")}</span>
        <span className="muted">{t("Push notices signed (HMAC)")}</span>
      </div>
      <ErrorBox error={err} />
      {session.operator && (
        <div className="row">
          <button className={b.authorized ? "" : "primary"} {...apagado(conexion, busy)} onClick={authorize}>{b.authorized ? t("Authorize again") : t("Authorize on Bitbucket")}</button>
          {b.apps === 0 && <button {...apagado(conexion, busy)} onClick={remove}>{t("Remove")}</button>}
        </div>
      )}
    </article>
  );
}

/** Connecting a Bitbucket Cloud workspace: its name and an OAuth consumer of the workspace; then Bitbucket asks you to authorize it. */
function ConectarBitbucket({ callback, first }: { callback: string; first: boolean }) {
  const conexion = useConexion();
  const [open, setOpen] = useState(first);
  const [workspace, setWorkspace] = useState("");
  const [key, setKey] = useState("");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  if (!session.operator) return null;
  const connect = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setErr(undefined);
    forjasApi.connectBitbucket(workspace, key, secret).then((r) => { location.href = r.authorizeUrl; }, (e) => { setBusy(false); setErr(e); });
  };
  if (!open) {
    return (
      <article className="forja-tarjeta">
        <div className="forja-cabeza">
          <span className="forja-sello bitbucket" aria-hidden="true">BB</span>
          <strong>Bitbucket</strong>
        </div>
        <p className="small" style={{ margin: 0 }}>{t("Bitbucket Cloud, by workspace, with an OAuth consumer of the workspace.")}</p>
        <div><button onClick={() => setOpen(true)}>{t("Connect Bitbucket")}</button></div>
      </article>
    );
  }
  return (
    <form className="forja-tarjeta" onSubmit={connect}>
      <div className="forja-cabeza">
        <span className="forja-sello bitbucket" aria-hidden="true">BB</span>
        <strong>Bitbucket</strong>
      </div>
      <p className="small" style={{ margin: 0 }}>{t("Bitbucket Cloud, by workspace. It uses an OAuth consumer of the workspace; rendimiento creates the push notices for you.")}</p>
      <label className="field">
        <span>{t("Workspace")}</span>
        <input value={workspace} onChange={(e) => setWorkspace(e.target.value)} placeholder="my-team" autoComplete="off" required />
        <span className="small muted">{t("The name in its address: bitbucket.org/my-team")}</span>
      </label>
      <details className="small">
        <summary>{t("How to create the OAuth consumer")}</summary>
        <ol className="forja-pasos">
          <li>{t("On Bitbucket, open the workspace's Settings → OAuth consumers → Add consumer.")}</li>
          <li>{t("Callback URL:")} <code>{callback}</code></li>
          <li>{t("Leave «This is a private consumer» checked.")}</li>
          <li>{t("Permissions: Account (read), Repositories (write), Pull requests (write) and Webhooks (read and write).")}</li>
          <li>{t("Save it, and copy its key and its secret here.")}</li>
        </ol>
      </details>
      <label className="field">
        <span>{t("Key")}</span>
        <input value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" required />
      </label>
      <label className="field">
        <span>{t("Secret")}</span>
        <input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} autoComplete="new-password" required />
        <span className="small muted">{t("It is kept with the platform's other secrets, never shown again.")}</span>
      </label>
      <ErrorBox error={err} />
      <div><button className="primary" type="submit" {...apagado(conexion, busy || !workspace || !key || !secret)}>{busy ? t("Connecting…") : t("Connect Bitbucket")}</button></div>
    </form>
  );
}
