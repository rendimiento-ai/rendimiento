# Connecting Bitbucket

rendimiento builds apps whose code lives on **Bitbucket Cloud** (bitbucket.org) as well as on GitHub and GitLab. You connect each Bitbucket **workspace** once, on the **Environment** page, with an OAuth consumer of that workspace; then **New app** offers its repositories beside the others, and each app says which forja it lives on. Bitbucket Data Center (your own server) comes later.

## What it does there {#what-it-does-there}

The same as on GitHub, said in Bitbucket's words:

| rendimiento needs to | On GitHub | On Bitbucket |
|---|---|---|
| Connect | The GitHub App (manifest) | An OAuth consumer of the workspace |
| Hear of each push | Webhook signed with HMAC | Repository webhook signed with HMAC-SHA256 (`X-Hub-Signature`), with a secret rendimiento sets |
| Clone | The installation's token | The OAuth token (as the user `x-token-auth`) |
| Say whether a commit passed | Check run | Build status: INPROGRESS, SUCCESSFUL, FAILED, STOPPED |
| "A person must merge it" | `action_required` | Status **INPROGRESS**, with a description that says so |
| Open and merge changes | Pull request | Pull request (`#7`) |
| Sign people in | GitHub's OAuth | Bitbucket's OAuth |

Where an app [merges by itself](apps.md), rendimiento merges the pull request once its run passes. Bitbucket's merge takes no commit, so rendimiento reads the pull request first and merges only while its head is still the commit that was tested; a newer commit waits for its own run.

## Creating the OAuth consumer {#creating-the-oauth-consumer}

1. On Bitbucket, open the **workspace's Settings → OAuth consumers** and press **Add consumer**.
2. **Callback URL**: rendimiento's address followed by `/api/forjas/bitbucket/callback`, for example `https://rendimiento.example.com/api/forjas/bitbucket/callback`. The Environment page shows yours.
3. Leave **This is a private consumer** checked.
4. **Permissions**: Account **Read**; Repositories **Write**; Pull requests **Write**; Webhooks **Read and write**.
5. Save it, and keep its **Key** and its **Secret** at hand.

Bitbucket asks for the consumer's permissions on every authorization, signing in too: it takes no narrower scope per request. rendimiento keeps the tokens only of the connection's authorization; a sign-in's are used once, to know who you are, and dropped.

## Connecting it {#connecting-it}

On **Environment → Where your code lives**, the Bitbucket card asks for:

- **Workspace**: its name, as in its address `bitbucket.org/my-team` (the address itself works too).
- **Key** and **Secret** of the consumer above.

**Connect Bitbucket** sends you to Bitbucket to authorize it. From then on rendimiento acts there as the account that authorized it: it lists the workspace's repositories that account **administers** (it must be able to add webhooks), and keeps its tokens. Bitbucket's access tokens last two hours, so rendimiento refreshes them before they run out and keeps the new ones each time. **Authorize again** on the card does it once more, as another person or after Bitbucket revoked the token.

The consumer's secret, the webhooks' secret and the tokens are kept with the platform's other secrets (a Kubernetes Secret named `rendimiento-bitbucket-<id>`, or sealed in the database on one computer), never in a plain table and never shown again. The database keeps only the workspace, the consumer's key and who authorized it.

## What rendimiento creates {#what-rendimiento-creates}

When you create an app from a Bitbucket repository, rendimiento:

- adds the repository's **webhook** to `https://rendimiento.example.com/api/webhooks/bitbucket`, for `repo:push` and the pull request events (`created`, `updated`, `fulfilled`, `rejected`), with the connection's secret, so each delivery comes signed (once per repository: creating it again updates it);
- opens the **pull request** that adds `rendimiento.yaml`, as on GitHub (or builds at once when the repository has one);
- writes a **build status** named `rendimiento: <app>` on every commit it runs, linking to the run.

A Bitbucket app's repository is written with `bitbucket.org` in front, `bitbucket.org/my-team/my-repo`, so it never mixes with a GitHub or GitLab one. A delivery signed with one workspace's secret is only acted on for that workspace's repositories.

## Signing in with Bitbucket {#signing-in-with-bitbucket}

Once a workspace is connected, the welcome page offers **Sign in with Bitbucket** beside GitHub. Who comes in:

- If the Bitbucket account is **linked** to a person, it is that person, with their roles and their Ruta. Anyone links their own account from **Your accounts**, in the account's menu: they sign in as they always do, press **Link my Bitbucket account**, and authorize it on Bitbucket.
- An account linked to no one comes in as `nickname@bitbucket.org` (for example `ana@bitbucket.org`), and only when an administrator added that name on [Members](members.md).

## Renovate, fixes and the coding agent {#renovate-fixes-and-the-coding-agent}

These work on Bitbucket apps too, through the connection:

- **[Renovate](renovate.md)** runs for them in a pod of their workspace's, with its OAuth token (refreshed first when it would not last the run), and opens pull requests as the account that authorized the connection. Its branches are built and checked like any other.
- **[Declared fixes](fixes.md)** commit on the branch through Bitbucket's API, and **declaring a fix** opens a branch whose pull request you open from the link it gives.
- **The coding agent** works on a branch of the repository and delivers a link to open its pull request (rendimiento opens it when the encargo is delivered).
- The diagnostician lists the **files changed** since the last good run (Bitbucket's diffstat).

A push of several branches at once runs each of them. A merged pull request is written down in the libreta with its whole commit, though Bitbucket's webhook gives only 12 characters of it.

## Not yet {#not-yet}

rendimiento does not join a conflict by itself on Bitbucket (Bitbucket's API writes commits with one parent): a person does. A Bitbucket connection is used by the platform's organization. Bitbucket Data Center comes later.
