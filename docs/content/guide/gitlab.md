# Connecting GitLab

rendimiento builds apps whose code lives on **GitLab** as well as on GitHub: gitlab.com, or a GitLab of your own (self-managed, at an address like `https://git.example.com`). You connect each GitLab once, on the **Environment** page; then **New app** offers its projects beside your GitHub repositories, and each app says which forja it lives on.

## What it does there {#what-it-does-there}

The same as on GitHub, said in GitLab's words:

| rendimiento needs to | On GitHub | On GitLab |
|---|---|---|
| Connect | The GitHub App (manifest) | An OAuth application of your group or your account |
| Hear of each push | Webhook signed with HMAC | Project webhook with a secret token (`X-Gitlab-Token`) |
| Clone | The installation's token | The OAuth token (as the user `oauth2`) |
| Say whether a commit passed | Check run | Commit status: pending, running, success, failed, canceled |
| "A person must merge it" | `action_required` | Status **pending**, with a description that says so |
| Open and merge changes | Pull request | Merge request (`!7`) |
| Sign people in | GitHub's OAuth | GitLab's OAuth |

Where an app [merges by itself](apps.md), rendimiento merges the merge request once its run passes; when GitLab will not merge yet (its own pipeline still runs), rendimiento sets it to **merge when pipeline succeeds**, and the run says so.

## Creating the OAuth application {#creating-the-oauth-application}

1. On your GitLab, open your **group's Settings → Applications** (or, for your own account, **Preferences → Applications**) and add a new application.
2. **Redirect URI**: rendimiento's address followed by `/api/forjas/gitlab/callback`, for example `https://rendimiento.example.com/api/forjas/gitlab/callback`. The Environment page shows yours.
3. **Confidential**: yes.
4. **Scopes**: `api` (to read the code, write commit statuses, open and merge merge requests and add webhooks) and `read_user` (to know who authorized it, and to sign people in).
5. Save it, and keep its **Application ID** and its **Secret** at hand.

## Connecting it {#connecting-it}

On **Environment → Where your code lives**, the GitLab card asks for:

- **Address**: empty for gitlab.com, or your GitLab's, such as `https://git.example.com`. Plain `http://` is only accepted for an address on your own network (`git.example.lan`, an IP).
- **Application ID** and **Secret** of the application above.

**Connect GitLab** sends you to GitLab to authorize it. From then on rendimiento acts there as the account that authorized it: it lists the projects that account **maintains** (it must be able to add webhooks), and keeps its tokens, refreshing them as GitLab expires them every two hours. **Authorize again** on the card does it once more, as another person or after GitLab revoked the token.

The application's secret, the webhooks' token and the tokens are kept with the platform's other secrets (a Kubernetes Secret named `rendimiento-gitlab-<id>`, or sealed in the database on one computer), never in a plain table and never shown again. The database keeps only the address, the application's id and who authorized it.

## What rendimiento creates {#what-rendimiento-creates}

When you create an app from a GitLab project, rendimiento:

- adds the project's **webhook** to `https://rendimiento.example.com/api/webhooks/gitlab`, for push and merge request events, with the connection's secret token (once per project: creating it again updates it);
- opens the **merge request** that adds `rendimiento.yaml`, as on GitHub (or builds at once when the project has one);
- writes a **commit status** named `rendimiento: <app>` on every commit it runs, linking to the run.

A GitLab app's repository is written with its GitLab's address in front, `git.example.com/group/project`, so two forjas never mix their projects.

## Signing in with GitLab {#signing-in-with-gitlab}

Once a GitLab is connected, the welcome page offers **Sign in with GitLab** beside GitHub. Who comes in:

- If the GitLab account is **linked** to a person, it is that person, with their roles and their Ruta. Anyone links their own account from **Your accounts**, in the account's menu: they sign in as they always do, press **Link my account on…**, and authorize it on GitLab.
- An account linked to no one comes in as `username@host` (for example `ana@gitlab.com`), and only when an administrator added that name on [Members](members.md). GitHub usernames have no `@`, so the two never mix.

## Renovate, fixes and the coding agent {#renovate-fixes-and-the-coding-agent}

These work on GitLab apps too, through the connection:

- **[Renovate](renovate.md)** runs for them in a pod of their connection's, with its OAuth token (refreshed first when it would not last the run), and opens merge requests as the account that authorized the connection. Its branches are built and checked like any other.
- **[Declared fixes](fixes.md)** commit on the branch through GitLab's API, and **declaring a fix** opens a branch whose merge request you open from the link it gives.
- **The coding agent** works on a branch of the project and delivers a link to open its merge request (rendimiento opens it when the encargo is delivered).
- The diagnostician lists the **files changed** since the last good run (GitLab's compare).

## Not yet {#not-yet}

rendimiento does not join a conflict by itself on GitLab (GitLab's API writes commits with one parent): a person does. A GitLab connection is used by the platform's organization. For Bitbucket, see [Connecting Bitbucket](bitbucket.md).
