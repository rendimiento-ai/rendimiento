// Where an app's code lives (Ruta 102): GitHub, a GitLab or a Bitbucket workspace connected on the Environment page. Each says
// things its own way (a merge request !7 on GitLab, a pull request #7 on GitHub and Bitbucket), and its pages are at other
// addresses; rendimiento follows each.
import type { App, Forja } from "./api";
import { t } from "./i18n";

/** The app's forja, GitHub when it says none (every app from before GitLab). */
export function forjaOf(app: Pick<App, "forja">): Forja {
  return app.forja ?? "github";
}

/** The forja's name, as people write it. */
export function forjaName(f: Forja): string {
  return f === "gitlab" ? "GitLab" : f === "bitbucket" ? "Bitbucket" : "GitHub";
}

/** The forja's two letters, on its seal. */
export function forjaSello(f: Forja): string {
  return f === "gitlab" ? "GL" : f === "bitbucket" ? "BB" : "GH";
}

/** The repository's page. A GitLab app's repo starts with its GitLab's address, a Bitbucket app's with bitbucket.org. */
export function repoURL(app: Pick<App, "forja" | "repo">): string {
  return forjaOf(app) === "github" ? `https://github.com/${app.repo}` : `https://${app.repo}`;
}

/** A branch's page, where its files and commits are. */
export function branchURL(app: Pick<App, "forja" | "repo">, branch: string): string {
  switch (forjaOf(app)) {
    case "gitlab": return `${repoURL(app)}/-/tree/${encodeURI(branch)}`;
    case "bitbucket": return `${repoURL(app)}/src/${encodeURI(branch)}`;
    default: return `${repoURL(app)}/tree/${encodeURI(branch)}`;
  }
}

/** A change's page: the pull request on GitHub and Bitbucket, the merge request on GitLab. */
export function pullURL(app: Pick<App, "forja" | "repo">, n: number | string): string {
  switch (forjaOf(app)) {
    case "gitlab": return `${repoURL(app)}/-/merge_requests/${n}`;
    case "bitbucket": return `${repoURL(app)}/pull-requests/${n}`;
    default: return `${repoURL(app)}/pull/${n}`;
  }
}

/** Where a person opens the change of a branch. */
export function newPullURL(app: Pick<App, "forja" | "repo">, branch: string): string {
  switch (forjaOf(app)) {
    case "gitlab": return `${repoURL(app)}/-/merge_requests/new?${new URLSearchParams({ "merge_request[source_branch]": branch })}`;
    case "bitbucket": return `${repoURL(app)}/pull-requests/new?${new URLSearchParams({ source: branch })}`;
    default: return `${repoURL(app)}/pull/new/${branch}`;
  }
}

/** A change by its number, in the forja's words: pull request #7, merge request !7. */
export function pullLabel(app: Pick<App, "forja">, n: number | string): string {
  return forjaOf(app) === "gitlab" ? t("merge request !{n}", { n }) : t("pull request #{n}", { n });
}

/** "pull request" or "merge request", the forja's word for a change. */
export function pullWord(app: Pick<App, "forja">): string {
  return forjaOf(app) === "gitlab" ? t("merge request") : t("pull request");
}

/** A forja a person can sign in with here: GitHub, one GitLab (by its address) or Bitbucket (any of its workspaces). */
export interface ForjaDeEntrada { forja: Forja; host: string; label: string }

/**
 * The forjas people sign in with here, from /api/me/identities: GitHub when its App is set up, each GitLab, and Bitbucket
 * once (an account signs in through any workspace).
 */
export function forjasDeEntrada(ids: { github?: boolean; gitlab: { host: string }[]; bitbucket?: { host: string }[] }): ForjaDeEntrada[] {
  const out: ForjaDeEntrada[] = [];
  if (ids.github) out.push({ forja: "github", host: "github.com", label: "GitHub" });
  for (const g of ids.gitlab) out.push({ forja: "gitlab", host: g.host.split("/")[0], label: `GitLab (${g.host})` });
  const bb = ids.bitbucket ?? [];
  if (bb.length > 0) out.push({ forja: "bitbucket", host: "bitbucket.org", label: `Bitbucket (${bb.map((b) => b.host).join(", ")})` });
  return out;
}

/**
 * The name a person signs in as, with an account on a forja no one linked to them: ana on GitHub, ana@gitlab.com (the
 * GitLab's host), ana@bitbucket.org. Members and ALLOWED_USERS take the same names.
 */
export function loginEn(f: Pick<ForjaDeEntrada, "forja" | "host">, user: string): string {
  const u = user.trim().replace(/^@/, "");
  if (!u) return "";
  return f.forja === "github" ? u : `${u}@${f.forja === "bitbucket" ? "bitbucket.org" : f.host.toLowerCase()}`;
}

/** The forja a member's own name signs in with (the server's claveDeLogin): no "@" is GitHub's. */
export function forjaDeLogin(login: string): Forja {
  const at = login.lastIndexOf("@");
  if (at < 0) return "github";
  return login.slice(at + 1).toLowerCase() === "bitbucket.org" ? "bitbucket" : "gitlab";
}
