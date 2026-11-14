# Changelog

What changed in each release, for people who run rendimiento. Every change is in the [merged pull requests](https://github.com/rendimiento-ai/rendimiento/pulls?q=is%3Apr+is%3Amerged); a release's notes on GitHub list them all.

What is not released yet is in `cambios/`, one file per change; each release gathers them here.

## Unreleased

## v0.2.0 (2026-11-14)

- **Preview environments:** a pull request opened (or reopened) on a branch whose last run already passed gets its copy at once, from that run's images, instead of waiting for the next push.
- **CI:** a test whose kept cache is 85% full empties Go's build cache before it starts (modules and tools stay), instead of failing every step with "no space left on device".
- **Preview environments:** each pull request with a copy gets one comment, edited on every push, with its address, the commit it shows and the day it will be deleted; when the copy goes, the comment says why.
- **Preview environments:** a copy of an app on a subdomain answers one level under the zone (`tienda.example.com` → `tienda-pr-7.example.com`), so a proxy's wildcard certificate such as Cloudflare's covers it; a domain's own host keeps `pr-7.tienda.com`.
- **Preview environments:** an app with `previews:` gets a live copy of each open pull request from its own repository once the branch's run passes, at `pr-<number>.<domain>`, with a **preview** check linking to it. Copies are deleted when the pull request closes, after `keep` days without a push, or to stay under `max`. Clusters only.
- **Tests after each release:** `tests:` in rendimiento.yaml runs the app's own Playwright tests (from an image of `builds:`) once each release is live. Every test starts only reporting; after 5 clean releases in a row a failure of it rolls the release back; one that fails and then passes on its retry 3 times is set aside. The Releases tab lists each release's tests. Migration 0043 only adds tables.
- **Post-deploy tasks:** `image: build:<name>` runs a task in the release's image of that build.
- **rendimiento.yaml:** a `previews:` block asks for a live copy of the app per pull request (`data: empty` or `seed`, `keep`, `max`); the copy's spec and its labeled namespace are rendered. The copies themselves come next (preview environments, slice 2).
- **The day's summary:** the newest day is a scoreboard: numbers that count up, badges, the runs by hour, a ring per app with its line, and the day's lotería card for the most notable thing. The numbers are counted from what was recorded, without AI.
- **rendimiento.yaml:** a CPU or memory request over its limit (the size's, or one you set) is refused when the file is read, with what to change, instead of failing later when a run makes its pod.
- **Work board:** a spare line (a copy of another pull request, or a branch forgotten for a week) has **Close**: the pull request is closed without merging and its branch deleted, after asking. rendimiento never closes anything by itself.
- **Encargos:** one open encargo per branch. A new failure on a branch that has one, by itself or with **Ask an agent**, is added to that encargo; a delivered one whose branch fails again goes back to pending with the note.
- **Coding agent:** it fixes an encargo's branch on that same branch instead of a copy of it, and leaves a person's branch to the person's own agent.
- **Work board:** branches that are gone, or whose commit is already in the main branch (merged with another built on them), no longer show as waiting to be merged; GitHub is asked which branches exist and which commits are in main, and an answer of "in main" is kept for good.
- **At a glance:** on a wide screen the glance is two columns (the flower and its words beside the week and the visits), with a bigger flower, instead of a narrow strip with empty space beside it.
- **At a glance:** a day is amber only when it was really slow (more than 1 in 200 checks failed, or a tenth of the day answered in a second or more), not for a single missed check: each release restarts the app and could turn every day of a busy week amber, and close the flower.
- **Merging:** **Merge this branch** on a branch whose commits are already in the main branch (another branch built on it brought them in) now says so, instead of GitHub's bare "Validation Failed"; other refusals from GitHub say what they were about.
- **Encargos:** each encargo shows its number, and a link to `#encargo-N` on the Encargos tab brings it into view (the work board's encargo links use it).
- **Capacity:** a busy node is now only avoided, not forbidden. A step's pod made while most nodes were busy could wait forever (its log said "No output yet") for the one node left, though the others were free a minute later; now it goes to whichever node has room first. Nodes turned off on the Environment page still take nothing.
- **Work board:** each app has a Work board tab (after At a glance): every branch with its pull request, its last run and its encargos, grouped by who it waits for (you, working, spare, merged today). The home page says how many things wait for you in each app, and agents read the board with the `mesa_leer` tool.
- **Apps:** an app's page opens **At a glance**: a cempasúchil that blooms while the app runs and withers when it does not answer, a hummingbird while an agent works on it, the day's summary, the week in line and day by day, its visits, how fast it answers, and what needs a person, only when something does. The overview moved to `/apps/<app>/overview`; **Show me the technical side first** opens it first.
- **AI:** the day's summary gives the model room to answer (its answer was being cut, and then only the numbers showed), keeps the lines that came whole if an answer is cut anyway, and writes the numbers properly ("7 releases, 1 failed run", only what is not zero).
- **Builds:** the browser tests (the `interfaz` task) ask for two cores and wait longer for each page, so a busy cluster no longer fails them when the code is right.
- **AI:** the home page opens with the day's summary: a few lines the platform's AI writes from what rendimiento recorded each day since your last visit (up to three days), each about an app you may see. It goes through the AI gateway and its caps, falls back to the local model and then to the numbers, and a quiet day is one line with no model. **Redo** writes it again.
- **Merging by itself:** a pull request that conflicts with the default branch only where both sides added lines (two pages added to the book's menu, two phrases to the translations) is joined by rendimiento: it merges the default branch into the branch keeping both, commits it through GitHub and tests it; the yes given carries over, and it is merged when it passes. Other conflicts name their files, and **Ask an agent to resolve it** sends the encargo to your own agent, which has git.
- **Builds:** **Retry what failed**, on the page of a failed run, runs again in the same run only the steps that did not pass; those that passed keep their result and their image. At most twice per run; on the main branch, administrators only and only its latest run. Agents retry branches' runs with `ejecucion_reintentar`. A step that fails and then passes on the same commit three times in 30 days is marked flaky and shows as a problem.
- **Organizations:** the operators' card on Members that gives GitHub accounts to organizations is now called *Which GitHub accounts each organization can use*, and says who can use what in three short lines.
- **Organizations:** the Universe draws the boundary between organizations as a wall around your barrios: zaguanes for what it lets in, red trancas for namespaces that call your apps from outside (with **Let it in**), a fence around each app it covers and a broken one around those it leaves out, postigos for `lan:` ports, and a xoloitzcuintle that barks when something is stopped. Each app's Overview and Environment → Platform settings say the same in words. Mark a node whose network does not enforce policies with `kubectl label node <node> rendimiento.ai/network-policy=no` so its apps show outside.
- **Organizations:** the boundary between organizations and their quota (`ORG_NETWORK_POLICY`, `INGRESS_NAMESPACE`, `ORG_NETWORK_POLICY_NAMESPACES`, `ORG_NETWORK_POLICY_CIDRS`, `ORG_QUOTA_CPU`, `ORG_QUOTA_MEMORY`) change on **Environment → Platform settings**, with no restart and no `make deploy`. A value saved there wins over the ConfigMap's until it is taken back; each change is checked as at boot and goes to the bitácora. Before turning the network boundary on, the page lists the namespaces that point at apps from outside their organization, with **Let it in**. When the boundary changes, every app is reconciled at once.
- **Builds:** a branch made at a commit the default branch already ran (an agent's new branch for an encargo) no longer runs that commit again, and a push to another branch whose last commit says `[skip ci]` does not run. The coding agent's "unfinished work" commit, left when it gives an encargo back, carries `[skip ci]`. A push to the default branch always runs.
- **Encargos:** the coding agent's branch carries the encargo's title as well as its number (`encargo-25-cambiar-la-configuracion-de-la`), so a list of branches says what each one is about.
- **The book:** a design page for AI-generated tests (Growing it → AI-generated tests): how rendimiento would discover an app with a Playwright walk and OpenAPI, generate tests, deliver them as a pull request and run them after the deploy with a rollback on failure, what it would cost per case, and the decisions p0dxD has to make. It is design only; nothing changes in the platform.
- **Preview environments:** the book has the design for a live copy of each pull request, with its address and an agent's comment (where it lives, its data, when it is deleted, what it costs, and the order to build it). Nothing runs yet.
- **Destinations:** the book has a design page for the destination agent (the hosted panel with an outbound-only agent on each computer or cluster): protocol, credentials per agent, `org_id`, version skew and what stays on the agent. It is a design only; the prototype is still to do.
- **Members:** an app can have its own list of who enters it, kept by the organization's administrators on the app's Settings tab (**Who enters**). Without a list, nothing changes: everyone in the organization enters with their role. With one, only the people on it enter, each with the role the list gives them for that app; for everyone else the app and everything of it (runs, releases, encargos, requests, problems, the Universe, the bitácora, activity notices) does not exist. The organization's administrators always enter. Someone taken out of the organization is taken off its apps' lists.
- **Agents:** a key limited to some apps can no longer wait on a request about another app (`produccion_esperar`), nor list, read or change the Mercado stalls of apps outside its list.
- **Universe:** the cluster's catalog and traced calls of apps a person cannot see (another organization's, or one whose list leaves them out) no longer show on their map.
- **Organizations:** an administrator of any organization now creates its apps with New app, not only the platform's operators. An organization other than the platform's uses only the GitHub installations it was given: the App installed on its administrator's own GitHub account, which they connect themselves in New app (their GitHub sign-in proves the account is theirs), or a GitHub organization's, which an operator gives under Members → Organizations. It never sees or uses another's. For people to install the App on their own account, the GitHub App must be public, and the account must be in `GITHUB_ACCOUNTS` when that is set.
- **Organizations:** an app of an organization other than the platform's cannot use `sharedNamespace` nor the platform's own address, and no app takes an address that an app of another organization claims in its `rendimiento.yaml`, even before that app is live. Checked when the app is created and on every push (a push that breaks one is not built, and its run says why).
- **Organizations:** each app's App object and namespace carry its organization (label `rendimiento.ai/org`); an existing App gets it on its next release, and until then counts as the platform's. Namespaces of `sharedNamespace` apps are not labeled.
- **Organizations:** a network boundary between organizations, off by default: with `ORG_NETWORK_POLICY=true` (Helm `orgBoundary.networkPolicy`) each app namespace lets in only its organization's namespaces, the platform's, the ingress controller's (`INGRESS_NAMESPACE`) and the ones listed in `ORG_NETWORK_POLICY_NAMESPACES` / `ORG_NETWORK_POLICY_CIDRS`; `lan:` ports stay open. It holds only where the network plugin enforces NetworkPolicy. Turning it off removes the policies.
- **Organizations:** an optional CPU and memory quota per organization (`ORG_QUOTA_CPU`, `ORG_QUOTA_MEMORY`; Helm `orgBoundary.quota`), checked by the platform before each release of an app outside the platform's organization; unset, no limit.
- **Capacity:** this release changes `deploy/` (the platform may now write network policies): update with `make deploy` (or `helm upgrade`) before turning the boundary on. Without it, the boundary stays off and nothing else changes.
- **Organizations:** two organizations can each have an app of the same name. In any organization other than the platform's, an app's name carries the organization's address in front («tienda» in `almacen` is `almacen-tienda`, also its namespace), and the New app page shows it before you confirm. Apps that exist keep their names. A new organization's address can no longer begin another's, nor the name of an existing app, and the platform's organization cannot take a name with another organization's address in front.
- **Organizations:** two tests now hold the wall between them on every push: each agent tool (MCP) is called with another organization's key and must find nothing of the first, and two people of two organizations walk the interface without seeing each other's apps, notes or bitácora. No leak was found.
- **Problems:** a new install no longer lists "GitHub App not configured yet" as an open problem. It is the next step of the setup, and the welcome page already leads there; the line stays in the log.
- **Signing in:** pressing **Cancel** on GitHub's page, where it asks to authorize rendimiento, brings you back to the welcome page. Until now rendimiento answered with an error (502), which a proxy such as Cloudflare shows as its own "Bad gateway" page.
- **Welcome page:** the section to install rendimiento has a third tab, **Kubernetes**, with the `helm install` command to copy and what the cluster needs, beside Windows and Linux.
- **rendimiento for Windows:** the window has a **Navigate** menu with **Back** and **Back to the start**, for pages with no way back (choosing to sign in with Apple on GitHub's page, for example). Until now the only way out was in the menu of the icon next to the clock.
- **Connecting GitHub:** after you choose the repositories on GitHub, rendimiento signs you in by itself. Until now it showed the welcome page, where you had to press "Sign in with GitHub" once more.
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
