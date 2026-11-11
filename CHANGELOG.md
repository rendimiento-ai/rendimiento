# Changelog

What changed in each release, for people who run rendimiento. Every change is in the [merged pull requests](https://github.com/rendimiento-ai/rendimiento/pulls?q=is%3Apr+is%3Amerged); a release's notes on GitHub list them all.

What is not released yet is in `cambios/`, one file per change; each release gathers them here.

## Unreleased

## v0.1.11 (2026-11-11)

- **A cluster on your own computer:** when rendimiento answers at a `.localhost` address (k3d, kind), each app's first service answers at `http://<app>.localhost` on the same port, with no certificate or DNS record, and rendimiento asks GitHub for new commits every minute (`POLL_INTERVAL`), since a webhook cannot reach it. The quickstart has the commands for k3d, and a test in GitHub Actions times them.
- **Helm chart:** `ingress.clusterIssuer: ""` now means what it says: apps are published over plain HTTP, with no certificate to wait for. Until now the apps asked an issuer named `letsencrypt-prod` anyway; set `ingress.clusterIssuer` if you relied on that.
- **Helm chart:** a `baseURL` with a port (`http://rendimiento.localhost:8080`) installs; before, the chart put the port in the Ingress's host and the cluster refused it.
- **Quickstart:** the book opens with a page that takes a Linux computer to a deployed example app, step by step, with the measured time of each step; the README links to it.
- **Quickstart:** the test on a clean machine now goes on from the installer to the example app (`rendimiento-ai/hola`) answering at its address, timed, with a stand-in for GitHub's pages; it fails when the install and the deploy take more than ten minutes.
- **Settings:** `GITHUB_API_URL` and `GITHUB_URL` say where GitHub's API and sign-in exchange are, for tests against a stand-in. Leave them unset otherwise.
- **Visits:** the dashboard's visitors panel shows rendimiento's own visits (the website of `UMAMI_WEBSITE_ID`) in a line of their own, only to administrators of the platform's organization.
- **Merging by itself:** an app that merges by itself now merges only what a person approved in rendimiento: **Merge this branch** on a run's page, or accepting the encargo that delivered the branch. rendimiento opens the pull request when the branch has none. Apps that had it on move to this; **any open pull request** (how it was) is a second choice in Settings → Merging.
- **Merging by itself:** a pull request from a fork is never merged, and a dependency update that Renovate or Dependabot left open waits for a person's yes even where any open pull request is merged.
- **Merging by itself:** after rendimiento merges a pull request, it deletes the merged branch (never a fork's).
- **An example to deploy:** with no apps yet, the Apps page and New app link to [rendimiento-ai/hola](https://github.com/rendimiento-ai/hola), a one-page app GitHub copies to your account, so a first deploy does not need a repository of your own.
- **Installer:** with Docker too, it turns on lingering for your user, so rendimiento keeps running after you sign out of a server and starts with the computer. Before, only the containerd path did.
- **Quickstart test:** each change to the installer and each release is installed, timed and uninstalled on a new amd64 machine (GitHub Actions, `inicio-rapido`), with no container engine and with Docker.
- **Comments on an agent's work:** a person can comment on a cargo's handover or on an encargo without sending it back, to say something is wrong or they would like it otherwise. Agents find the unanswered ones when they start (and an encargo's while they wait on it) and answer them; the answer reaches the person in the bell.
- **Runs by kind of branch:** the Runs tab of an app has filters with counts: the main branch, with a pull request, fixes, features, releases, dependencies and other branches. A branch's kind comes from its pull request's labels (`bug`, `enhancement`…) or else from the start of its name (`fix/`, `feat/`, `release/`…), and shows beside it with its pull request's number.
- **Merging by itself:** an app can merge its own pull requests (Settings → Merging, off until an administrator turns it on). A branch with an open pull request into the default branch is merged once its run passes, with the commit that was tested; a branch without one is only tested. A conflict is never merged: the run says so and offers to ask an agent to resolve it, as an encargo that is estimated first.
- **Visits to rendimiento itself:** with `UMAMI_WEBSITE_ID` (the id Umami gives a website) and `UMAMI_PUBLIC_URL`, every page of rendimiento loads Umami's script, so its own visits are counted like any app's.
- **Delivery and visitors on the start page:** the numbers are easier to read at a glance: each in its own tile with a larger figure, whether it got better or worse in a colored label that also says it in words, and a soft wash under its twelve-week line. Below the delivery numbers, the deploys of each of the last 30 days as bars. The Visits tab of an app has the same look, and its bars grow when they appear. The mark at the top beats: a pulse runs along its line (and faster under the pointer).
- **The welcome page:** changing between Windows and Linux in "On your own computer" no longer leaves a copy of the previous system's block behind each time.
- **More than one cache per test:** a test with `cache: true` now keeps two volumes instead of one, so two runs (a branch and `main`, say) test at once without one waiting for the other's cache. **Caches of each test**, in the Environment page's Capacity, changes how many (up to 6); each is a volume of `TEST_CACHE_SIZE`, and the second one starts empty.

## v0.1.9 (2026-11-10)

- **Libraries:** an app with only tasks (a library) no longer shows as Pending: it says Library, and its page says there is nothing to deploy or check.
- **rendimiento for Windows:** the icon's menu has **Back to the start**, for a window left on a page with no way back (GitHub's sign-in, for example).
- **The README and the welcome page** now say what agents cannot do (deploy, roll back: they ask and a person decides), that every action is in the bitácora, and how to get rendimiento for Windows.
- **The welcome page:** installing has its own place, for Windows (a download button) and for Linux (the command to copy), each with its three steps and a small animation of what happens. It opens on the visitor's system.

## v0.1.8 (2026-11-10)

- **Connecting GitHub:** after you choose the repositories, GitHub brings you back to rendimiento instead of leaving you on its page. It applies to a GitHub App created from now on; one that already exists can get it in its settings on GitHub (Setup URL).
- **rendimiento for Windows (preview):** each release publishes `rendimiento-windows-amd64.exe`, an app that turns WSL2 on, downloads, checks and imports the distribution, asks who signs in first, and shows rendimiento in a window of its own, with an icon next to the clock to put it to sleep and wake it up. It is not signed yet (SmartScreen asks before running it). See [the book](docs/content/environment/deploy.md#on-windows).
- **A clock that is off:** when GitHub answers "Bad credentials" and the computer's clock is more than a minute away from GitHub's, the error says by how much and how to fix it.
- **Redis on localhost:** a service whose Redis client (ioredis) is refused without saying where now gets the same hint: use `REDIS_URL`.
- **Windows:** the book says what to do when WSL2 cannot start (restart, virtualization in the BIOS) or GitHub refuses the clock.
- **On one computer:** right after the computer (or the WSL distribution) starts, rendimiento no longer fails once while containerd brings its Postgres back up; it tries the start again, and a start that keeps failing says what nerdctl or Docker said.

## v0.1.7 (2026-11-09)

- **Windows (preview):** each release publishes `rendimiento-wsl-amd64.tar.gz`, a WSL2 distribution with rendimiento and its container engine ready, to import with `wsl --import`. See [the book](docs/content/environment/deploy.md#on-windows).

## v0.1.6 (2026-11-08)

- **Uninstalling:** `curl -fsSL https://rendimiento.ai/instalar | sh -s -- --desinstalar` takes rendimiento off a computer (the service, its containers and your apps', their networks, the program), and deletes your data only if you say yes.

## v0.1.5 (2026-11-07)

- **Señales de vida:** once a week, each installation sends rendimiento.ai a few anonymous counts (apps, runs, agents; its mode, engine and version), never a name or a secret. The operators see the next one exactly as it would go, under Members, and turn them off there or with `SENALES=off`. See [the book](docs/content/guide/senales.md). (#150)
- **A bell for what waits for you:** agents' requests, encargos delivered for review and open problems, and what just happened; it fits a phone's screen. The bitácora says what each action was in words and what was decided, and an agent's reason reaches you in your language. (#148, #149)
- **Copying says it copied,** and a new agent key's panel closes from its top. (#147)

## v0.1.4 (2026-11-07)

- **The bitácora:** every action, by people (each call that changes something) and by agents (each tool call), in a hash chain per organization, so a row changed or removed in the database shows; a page with filters and an export. (#144, #145)
- **Agents ask, people decide:** an agent can ask to roll an app back, never do it; an administrator approves or rejects, and the platform rolls back in their name. (#146)

## v0.1.3 (2026-11-06)

- **A release that doesn't answer fails, and says why.** On one computer, a published port accepted connections even when nothing listened behind it, so a broken release looked healthy. Now it fails, saying where the service listens inside its container, whether it looks for Redis or Postgres on `localhost`, and its last log lines. (#136)
- **Build tasks can have a throwaway Postgres and a kept cache** (`postgres: true`, `cache: true`), as tests can, for a task such as a browser test that drives the app with its database. (#135)
- **Browser tests of the UI on every push**, in English and Spanish, with every kind of app. They found two bugs, fixed here: the activity stream failed on every Spanish page, and without a cluster the UI asked for cluster-only data. (#137)
- **Each release publishes its Helm chart** at `oci://ghcr.io/rendimiento-ai/charts/rendimiento`, with the release's version, so it installs that release's image. (#138)
- **Agents get the Ruta in English** with `/mcp?lang=en`; every API route is checked to stay in its organization; pull requests from forks are checked by GitHub Actions; SECURITY, CODE_OF_CONDUCT and templates. (#139, #140, #141, #142)

## v0.1.2 (2026-11-05)

- **Docker:** BuildKit runs on the computer's network, so builds can push to the local registry (`localhost:5000`). (#134)

## v0.1.1 (2026-11-05)

- **The installer** sets up a containerd that is installed but not yet set up for this user, and gives the service a `PATH` with `sbin` (where `iptables` lives). A busy Postgres port no longer stops rendimiento from starting. (#131, #133)
- **On one computer**, a service that stops fails its release with its last log lines; each service answers to its name on its app's network; the app's address includes the proxy's port. (#132)
- **The welcome page** shows the install command. (#130)
- **Libraries and builds-only apps**: an app may have only builds or tasks, with nothing to deploy; the New app wizard handles a repository with no services. Every layer is tested with the same examples (`internal/spec/ejemplos`). (#127, #128, #129)

## v0.1.0 (2026-11-05)

The first release: the program for Linux (amd64 and arm64), installable on one computer with `curl -fsSL https://rendimiento.ai/instalar | sh`, and the image for amd64 and arm64 at `ghcr.io/rendimiento-ai/rendimiento`.
