# The libreta: what was asked for each app

Each app has a **Libreta** tab, beside Roads: the list of what was asked for that app, and whether it is done. It is a view, not a second memory. Every line comes from somewhere that already exists, and the [Ruta](ruta.md) stays the one place where things are written and decided.

## Where each line comes from {#where-each-line-comes-from}

| Source | What |
|---|---|
| **The Ruta** | to-dos and decisions whose `app` field names this app ([entries about an app](ruta.md#entries-about-an-app)) |
| **Encargos** | every encargo about this app, open or finished |
| **Renovate** | its pull requests in the app's repository (and Dependabot's), unless an entry or an encargo already speaks for that branch. *Include Renovate's* hides them |
| **The app's own tests** | while some of its [browser tests](reliability.md#the-apps-own-tests) only report, a line says how many clean releases they still need before they may roll a release back |

**Note a request** writes a to-do in the Ruta with the app already set, signed by you. Agents do the same with `ruta_anotar` and its `app` field.

## How a request moves {#how-a-request-moves}

Nobody sets a state by hand: rendimiento computes it each time from what it already knows.

| State | When |
|---|---|
| **Asked** | it is written down, and nothing else is known yet |
| **In progress** | an agent took its encargo, or a branch is linked to it (its run is going, failed, or it conflicts) |
| **Ready to merge** | its branch's last run passed |
| **Integrated** | its branch is in the default branch: its pull request was merged, rendimiento merged it, or its commit is already there |
| **Shipped** | a release made after the merge passed its [verification](reliability.md#verifying-each-release) |

And two apart:

- **Waiting**: it depends on something that has not happened yet. A delivered encargo waits for your review; a branch that passed waits while GitHub will not merge it (a review or a required check); the app's own tests wait for clean releases in a row.
- **Discarded**: set aside, with its reason. A cancelled encargo, with the note it was cancelled with; a Ruta entry set aside with its reason (**Discard** on its card, `descartado` in `ruta_actualizar`, or `discarded` from the API); a pull request of Renovate's closed without merging.

A to-do ticked in the Ruta, a decision, or an accepted encargo, with no branch to say more, is **Finished**.

### Which branches belong to a request {#which-branches-belong-to-a-request}

- A Ruta entry: the branches linked to it. A branch is linked in three ways, and each new one is added:
    - **A commit's trailer**: a line `Ruta: 150` in any commit pushed to the branch (also `Ruta: #150`, or several numbers, comma-separated: `Ruta: 150, 151`). When GitHub's push arrives, the branch is linked to those entries if they exist, belong to the organization and are about the app of the repository (or about no app; the trailer never sets the app). The default branch is never linked this way.
    - **By hand**: `rama` in `ruta_anotar` or `ruta_actualizar`.
    - **By an agent's cargo**: a branch an agent runs with `ejecucion_lanzar` is linked to the to-dos and decisions about that app it wrote or changed in the same cargo, before or after the run (not the discarded ones). That is the only way rendimiento knows which branch an agent pushed: a `git push` carries no agent key.
- An encargo: the branch the coding agent names for it (`feature/encargo-<n>-…`), its run's branch, and the branches and pull requests in its links. A branch counts once rendimiento has seen it (a run or a pull request).

When a request has several branches, the one furthest along speaks for it.

### What is kept {#what-is-kept}

When rendimiento sees a linked branch merged (it merged it, or GitHub's webhook says its pull request was merged), it writes down on the link the commit that was merged and when. When a release passes its verification (or needs none), it writes down that release on the merged links of the app that no release carried yet, if its run came after the merge. A merge or a release the libreta works out from the runs and GitHub, and that was not kept yet, is written down too the first time it is shown. What is kept speaks first, so a request shipped long ago still says where it went once its runs are past the 60 days the libreta reads.

## What you see {#what-you-see}

- **Open**, **Done** and **All**: the open requests with how far each has gone (asked, its branch, integrated, shipped) and its links (its Ruta entry or encargo, cargo, branch, pull request and run); what was done this week with its merge and the release it shipped in; and what was discarded, with its reason.
- **This week**: how many were done, how many are open, and how many were set aside.
- **Discard**, on each open card that comes from the Ruta (for collaborators): it asks for the reason in the card itself and sets the entry aside in the Ruta, keeping the version before in its history. A discarded one has **Take it up again**. Encargos are cancelled from their own page, and Renovate's pull requests on GitHub.
- On a phone it is one column, and each request's way is a bar of four steps.

## For agents {#for-agents}

`app_estado` brings the libreta in its `libreta` field: `abiertos` (open), `hechos_semana` (done in the last seven days) and `descartados_semana`, each line with its `fuente` (ruta, encargo, renovate, pruebas), `estado` (pedido, en_curso, listo, integrado, publicado, esperando, descartado, hecho), `porque`, and its branch, pull request, run and release. An agent working on an app sees at a glance what is left there. It changes a state by doing the work (a branch, a run, a merge), never by setting it. To tie its branch to the entry it answers, it adds the trailer `Ruta: N` to a commit, or passes `rama`.

## Where the data comes from {#where-the-data-comes-from}

- The Ruta's entries, their linked branches and the encargos come from rendimiento's database. Up to 300 of each are read.
- What was kept of each branch (its merge and its release) comes first.
- Runs come from the last 60 days, and releases from the latest 100 with their verification; the release a merge shipped in is the first one made from a run of the default branch created after the merge.
- Pull requests and whether a branch's commit is already in the default branch come from GitHub, as for the [work board](../start/tour.md#the-app-page), remembered for two minutes. If GitHub does not answer, merges are known only from the runs (those rendimiento merged), and Renovate's pull requests are missing; the tab says so.

## In the code {#in-the-code}

| What | Where |
|---|---|
| The states, computed from the facts | `internal/libreta` (`Build`) |
| Reading the facts | `platform.Libreta` |
| Entries about an app, their branches, an app's encargos | `store.RutaOfApp`, `store.RutaBranches`, `store.EncargosOfApp` (migration `0057_libreta.sql`) |
| Linking branches by themselves | `platform.RutaRefs` (the trailer, on the push webhook), `store.LinkCargoEntries` and `store.LinkCargoLaunches` (the cargo) |
| What is kept of a branch | `store.MarkRutaMerged`, `store.MarkRutaReleased`, `store.KeepRutaBranch` (migration `0058_libreta_firme.sql`) |
| The tab | `web/src/components/libreta.tsx` |
| The API | `GET` and `POST /api/apps/{app}/libreta`; `POST /api/apps/{app}/libreta/{id}/discard` with `{"reason": "…"}` (empty to take it up again) |
