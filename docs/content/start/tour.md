# A tour of the UI

The dashboard is at **https://rendimiento.example.com**. You sign in with GitHub; only [members](../guide/members.md) get in, each with a role. The top bar has two rows: the search, the bell, **New app** and your account above; every page below ([The top bar](#top-bar)).

```mermaid
flowchart TB
    top[Top bar] --> apps[Apps /]
    top --> svc[Services /services]
    top --> add[Add-ons /addons]
    top --> env[Environment /environment]
    top --> new[New app /new]
    apps --> app[App page /apps/:name]
    app --> gl[At a glance]
    app --> mesa[Work board /apps/:name/mesa]
    app --> ov[Overview /apps/:name/overview]
    app --> runs[Runs] --> run[Run page /apps/:name/runs/:id]
    app --> rel[Releases]
    app --> set[Settings]
    add --> det[Add-on page /addons/:name]
```

The band at the foot of every page says which rendimiento answers, for anyone signed in: its version (or the commit it was built from), the system its program is for, and whether it runs on a computer or on a Kubernetes cluster, as in `v0.2.1 · Linux arm64 · on a computer`. Give that line when you report something.

## The top bar {#top-bar}

On a wide screen, the first row has the brand, the **search**, the bell, **New app** (administrators) and your **account** (your initial and login). The second row has every page you can open, each with its drawing: Apps, Universe, Services and Add-ons (operators, on a cluster), Environment, Ruta, Bitácora (not guests), AI, Problems with how many are open (operators) and Comments.

- **The search** finds, as you type, apps by their name or repository, runs by their number or branch (among the last ones of your apps, or any number you type), and pages by their name, in your language and with or without accents. Press **/** on any page to go to it (not while writing in a box), the arrows to move, **Enter** to open the first result, **Escape** to empty it and close it. The search on the Apps page still filters the mosaic.
- **Your account's menu** has who you are (it opens Members), the **organization** (a list when you belong to more than one), the **theme** (day or night, the family of colors and the movement), the **language** and the flag, **My comments** (what you wrote to the team, and its answers) and **Sign out**. Escape or a click outside closes it; the arrows move between its rows.

On a phone the bar is one short row: the brand, a search button that opens the box over the top of the screen, the bell and your initial. The pages and the account are in the sheet that peeks from the bottom.

## Apps

The home page lists every app with its phase (*Healthy*, *Progressing*, *Degraded*, *Waiting for first build*…), its latest run and its public addresses. Phases update live.

At the top, **Running now** has a card for each run queued or running: its app (it opens the app), its branch, its steps as small bars (passed, running, failed, still to come), the step at hand and how long it has run, and **See run N →**. It updates by itself. When nothing runs it is one line, **All quiet**, with the last run that ended.

## New app

One screen, in three moves:

1. **Repository**: on the left, every repository the GitHub App can see, with a filter. Picking one runs detection on it. With nothing of your own to deploy yet, the page (and the empty Apps page) links to **the example**, [rendimiento-ai/hola](https://github.com/rendimiento-ai/hola): one page served by nginx, which GitHub copies to your account.
2. **Configure**: on the right, the app's name and one row per detected service: public URL (subdomain plus zone), size, instances and **Needs** (PostgreSQL, Redis). **More options** unfolds the full form of each service: port, health check, storage, secrets, tests, environment variables and needs (also another service picked from the catalog; pre-ticked when the code uses a client library for them). If the repository has no Dockerfile, you choose between **building from source** (Railpack, recommended) and **adding a generated Dockerfile**. If something is already running in the target namespace (deployed by ArgoCD or by hand), a migration panel offers to **take it over** in place.
3. **Deploy**: for a new repository, rendimiento opens a pull request adding `rendimiento.yaml` (and a Dockerfile if you chose one); the PR is built and checked, and **merging it deploys**. For a repository that already has `rendimiento.yaml`, the first build starts immediately.

## The app page

**At a glance**
:   What most people need, opened first: a cempasúchil that is open and breathing while the app runs, draws its petals in when it is slow or fails now and then, and withers when it does not answer (a green bud before its first release). A hummingbird flies by while an agent works on the app. Beside it, a sentence from [the day's summary](../guide/ai.md#the-days-summary), the share of the week the app was in line (a ring), one dot per day of the week (green, amber when it was slow, red when it did not answer for a while), its visits of the last seven days as bars and against the week before, how fast it answers and how many changes the week had. Under the flower, the addresses where the app answers, each a link. While its numbers are missing (no uptime checks yet, visits off, no preview copies), cards with the steps that bring them stand in their place, each leaving when it is done. **The latest** is its diary: runs, releases and encargos with the time, and the day it was born. Branch names here and on the other tabs are links to the branch on GitHub. **For you** shows only when something needs a person: a publication that failed, work of agents waiting for review. **See the details** goes to the work board; **Show me the technical side first** makes the work board open first on this device. With movement off, the flower, the bird and the bars hold still.

**Work board**
:   Everything open in the app, in one list: each line of work is a branch with its pull request, its last run and the encargos about it (or an encargo with no branch), with what it needs and one link to do it. **Your turn**: a branch that passed and can be merged (or that GitHub will not merge without a review), one that conflicts with the main branch, one that failed with nobody on it, an open pull request never tested here, and work an agent delivered for your review. **Working**: a run going on, or an agent that has the encargo. **Spare: can be closed**: an open pull request with the same commit as another (the older one, or the merged one, is the original), and a branch that failed more than a week ago with no pull request. **Merged today**: what went in during the last day. A spare line has **Close** (its pull request is closed without merging, and its branch deleted) or **Delete the branch**; it asks first, and rendimiento never does it by itself. The home page says how many things wait for you in each app, with a link to its board. At the top, **What shall we change?** turns what you write into an encargo about the app (its first line is the title), with ideas a tap away; an agent takes it from there. Agents read the same board with `mesa_leer` before opening a branch.

**Overview**
:   One card per service: ready replicas, public URL, DNS and certificate status, the running image. **Provided for this app** shows the database and cache from `needs:`, which services use them and how to connect from your machine. **Dependency updates** is the per-app Renovate switch. **Resources** is a live tree of the Kubernetes objects (Deployments, pods, Services, Ingresses) with their health.

**Runs**
:   Every CI run: commit, branch, status, duration. **Run** starts a build of the default branch by hand. Above the list, filters by [kind of branch](../guide/builds.md#kinds-of-branch): the main branch, with a pull request, fixes, features, releases, dependencies, other branches.

**Run page**
:   The run's steps, two ways (the switch on the steps remembers your choice in this browser): **the race**, every step a runner in its lane across the run's time, with the place it arrived in and how long it took, a dotted stretch before the ones that waited; or **the threads**, the run as a weaving, where a thread leaves from the change for every step that waits for nobody, each as long as its step took, and a step that waits for others starts at the knot of their threads. While the run goes, the bars and threads of the running steps grow and shine, a finished one stops where it arrived, and a faint dashed mark shows where each step finished the last time a run of the same kind passed. The page catches up by itself when the connection drops or the phone puts it to sleep. Tap a step for its live log, its duration and, for builds, the image digest and the BuildKit daemon that built it. Reused steps show which release they came from.

**Releases**
:   Every release with its commit and images; **Roll back** to any of them.

**Settings**
:   Set the values of the secrets the app declares (they never go to git), and the **Danger zone**: *Disconnect* (stop managing, leave everything running) or *Delete* (after showing exactly what would be removed: namespace, volumes, secrets).

## Services

Every Service in the cluster, as a list on the left in three groups:

- **Your apps & add-ons**: what rendimiento deploys;
- **Elsewhere in the cluster**: deployed by ArgoCD, Helm or `kubectl`;
- **Cluster internals**: ingress, certificates, storage, CI.

Within each group, services are sorted by what they do: Databases, Caches & queues, AI, Storage, Monitoring & analytics, Web & APIs, Developer tools, Platform, each with a dot for whether it is ready. The search looks across every group. Choose a service and the right side shows **where it comes from** (app, repository folder, image, ArgoCD app or Helm release), **what it exposes** (ports, public URLs, LAN address, health check, endpoints), **how to connect** (addresses and a `rendimiento.yaml` snippet to copy) and **who uses it** (found from other workloads' environment variables).

## Add-ons

A list on the left, with a search box across all of it, and the add-on chosen on the right.

- **Needs attention** and **Installed**: each installed add-on with its phase (*In sync*, *Changes waiting*, *Review needed*, *Error*, *Suspended*) and source (those in *Error*, *Review needed* or *Changes waiting* come first, under *Needs attention*). Choosing one shows what a sync would change, object by object with diffs; its Helm hooks and their recent runs; and buttons to **Sync**, **Suspend**, **Edit definition**, **Stop managing** or **Uninstall**.
- **Add from the catalog**: Longhorn, Hajimari, Uptime Kuma, CloudNativePG, *any Helm chart* or *manifests from a git folder*; choosing one opens its short form, with a YAML box to override any chart value. Installing commits a file to the gitops repository.
- **Built in**: Renovate, with its schedule, repositories, configuration and run history.

## Environment

Everything rendimiento depends on, checked live, drawn as a town of Cotija:

- **The bando**: the version of rendimiento that runs and, when a newer one is ready, the **Update** stamp.
- **The street**: each node is a house (the control plane is the presidencia municipal, with its clock tower and the flag each person chooses with the flag button in the top bar, next to the language; it starts as the browser's country). Lit windows are how busy its processor is, the red skirting rises with its memory, and a note on the door says something is wrong. Tap a house for its CPU, memory, conditions and roles.
- **The signs**: the providers (cluster, DNS, source, registry) and a few requirements (email, storage, the database) hang over the square. A sign opens its *ventanilla*: the provider in use and the ones coming, and for DNS the public IP, when it last changed and an **Update now** button; for email, **Send test email**; and the details and **fix** when something is wrong.
- **Notices in the square**: a cork board with every requirement that needs attention and every failing pod or certificate.
- **What the town needs**: the grouped checks (cluster, networking, TLS, build, integrations), each OK, *Attention*, *Missing* or *Error* with details and a **fix** when something is wrong. For example: the ingress controller, cert-manager and its issuer, storage classes, the BuildKit pool (how many daemons are ready), the registry, the build namespace's isolation, the GitHub App.

## Setup

The very first visit, before a GitHub App exists, shows the one-click setup: rendimiento sends GitHub an App *manifest*, GitHub creates the App and redirects back with its credentials, which are stored in a Kubernetes secret. See [How the platform is deployed](../environment/deploy.md#the-github-app).
