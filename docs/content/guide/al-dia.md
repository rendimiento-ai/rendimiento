# Putting an app up to date (each dependency update, read) {#putting-an-app-up-to-date}

[Renovate](renovate.md) (or Dependabot) opens a pull request for each new version of a dependency. A small one passes and is merged; one that changes a lot fails, or passes while something the tests do not cover breaks, and waits until someone looks. rendimiento reads each one first, says what it asks of the app, and, when you say yes, has an agent adapt the app to the new version on the same branch.

## The report {#the-report}

When a run of a dependency update's pull request ends (a `renovate/` or `dependabot/` branch, or a `dependencies` label), the agents write its report with the organization's model (the local one first). They read the pull request's text, which carries the release notes, and, when the run failed, its failed steps, the end of their logs and the files changed. The report says:

- **What would fail**: the breaking changes in the notes that touch what the app likely uses, and the run's failures.
- **What would change**: the files, with about how many lines added and taken out.
- **Risk and cost**: low, medium or high, and what putting it up to date would cost with the platform's AI, priced as the [estimator](ai.md) prices an encargo.

When the run passed and the notes ask nothing of the app, it says **No changes**, and the pull request can be merged as it is. The reports show on the app's **Caminos** tab, above its live copies, the latest of each update. A run made by the work on an update (its encargo's) gets no new report: the encargo answers for it.

## Putting it up to date {#putting-it-up-to-date}

**Put the app up to date** (for whoever may merge in the app) opens an encargo on the update's run, so it is worked on the same branch, never another nor the default branch, with the report in its detail. If that branch already has an open encargo (the diagnostician's, say), the report is added to it instead. Then it goes to whoever the organization chose:

- **The platform's coding agent** (by default): it is sent to it, as **Do it now** does. While the coding agent is off (AI page) it waits, and the tab says so.
- **Your own agent**: the encargo is for your Claude, in the Ruta's queue.

The agent adapts the app, tests it with the branch's run and delivers it; you merge it, as always (or rendimiento does, in an app that merges by itself). The report then says what became of its encargo: waiting, being worked on, delivered, done.

Once the agent commits on the branch, Renovate sees someone else's commit and stops rebasing and updating it, as it does for any edited branch (and for a [lockfile fix](renovate.md#lockfiles)): it never writes over the agent's work, and a newer version for that branch waits until this one is merged or closed. Ticking the pull request's rebase box gives the branch back to Renovate, and drops the agent's changes.

## The choices {#the-choices}

On the AI page, the **Put up to date** card (an administrator changes it):

| Choice | What it does |
|---|---|
| On or off | Off, no reports are written. On by default. |
| Who does it | The platform's coding agent (default), or your own agent. |
| When | **With every update** (default), or **only when its run fails**. «Every» reads the ones that pass too: a test that does not exist cannot fail. |
| Without asking | **Always ask me** (default), or **by itself when the risk is low and it costs at most** an amount (25 cents at first, up to $5): such a report opens its encargo at once. |

The work is the coding agent's, with [its permissions](ai.md); the report reads runs and logs with the diagnostician's *See runs, step logs and slow requests*. Neither ever merges.

## In the code {#in-the-code}

| What | Where |
|---|---|
| Asking for a report when a run ends | `internal/platform/al_dia.go` |
| Writing it, and opening the encargo | `internal/agentes/al_dia.go` |
| The reports and the choices | `internal/store/al_dia.go`, migration `0071_al_dia.sql` |
| `GET /api/apps/{app}/al-dia`, `POST …/al-dia/{id}/poner`, `GET`/`PUT /api/ia/al-dia` | `internal/api/al_dia.go` |
| The Caminos tab's reports and the AI page's card | `web/src/components/al-dia.tsx` |
