# Roads: an app's branches as caminos

The **Roads** tab of each app tells the last two weeks of its builds as a story. The default branch is the *camino real*, a dirt road running left to right from oldest to newest. Every other branch is a *vereda*, a path that leaves the camino real at the commit it started from and, once merged, comes back to it. Each run is a stop along the way.

## What you see {#what-you-see}

| On the map | What it is |
|---|---|
| The **camino real** | the default branch; the stone markers (*mojoneras*) are days |
| A **vereda** | a branch. It leaves the camino real where it started (the merge base) and rejoins it where its pull request was merged. A stacked pull request, whose base is another branch, leaves and rejoins that branch instead |
| A whole **huacal** of maize | a run that passed |
| A broken huacal, maize spilled | a run that failed; the panel says at which step (`platform: tests`, `web: image`…) |
| A grey huacal | a cancelled run |
| A **market stall** on the camino real | a release that was deployed, with its number |
| A vereda that ends at a **sign** | a branch that was not merged: *waiting at the gate* while it ran in the last 48 hours or its pull request is open, *left on the way* after that |
| A dashed vereda | not merged (yet) |
| A **little house** at the end of a vereda, with a number | its pull request's [live copy](#live-copies); a dashed one is a short copy en seco. Choose it (or Tab to it) to go to its card |

Quiet stretches of time are shortened, so a busy afternoon gets more room than an empty weekend.

Tap a stop or a vereda to see its story in the panel: when it left and from where, its trips, how many times it stumbled, how long it took from leaving to merging, and a link to each run. With nothing selected, the panel sums up the two weeks: how many trips arrived whole, the typical life of a branch, how many branches passed on their first trip, the branches not merged, and the latest stumbles.

## Live copies {#live-copies}

An app with [`previews:`](../future/previews.md) has a section above the map, **Live copies · N of max**: one live copy per pull request whose branch passed, and the short copies its runs make only to run their checks [en seco](tasks.md#dry-runs-against-a-copy) (they count toward `max`). The glance (*At a glance*) has a chip, **N live copies**, that leads here.

- **Four figures:** how many copies live of the app's `max`; the memory they hold (what their pods use, from the metrics server, or what they ask for when it does not say; the figure says which); the share of the cluster's memory that **all apps'** copies hold, against the 10% cap (on a computer there is no cluster to share); and the app's `keep` and `max`, which change in `rendimiento.yaml`.
- **A card per copy:** its pull request's number, title and author, its address, the branch, commit and run it shows, its health, its own tests' result (how many passed, and the names of those that failed) and its checks en seco, the memory it holds, and the day it is deleted (`keep` days after its last push, or when the pull request is merged or closed). A short copy's card is dashed and links to its run.
- **Deleted lately:** the copies of the last 7 days and why each went: merged, closed, no push for `keep` days, to make room under `max`, or by a person.
- **Secrets:** a copy reads the Secrets the app's services name in its own namespace, and nothing copies them there yet, so the section names those a copy does not have (names only, never values): it starts, but what needs them does not work. On a computer a copy reads its app's own secrets.

A person who may merge (a collaborator or an administrator of the app) has a **⋯** menu on each pull request's copy: **Make it again with the last commit** (the branch's last run that passed; when the copy already runs it, its services restart), **Keep it 3 more days** (up to 30 days after its last push; the pull request's comment says the new day) and **Delete it now**, which asks on the card itself. A deleted copy's comment says who deleted it; the sweep does not make it again, the branch's next push does. Each of them is in the bitácora; an agent's key cannot do them. Each branch with a live copy has a **live copy** badge in the panel that leads to its card.

## Telling the story {#telling-the-story}

**Tell the story** walks the two weeks: a burro with its load goes down every vereda that is on its way, stumbles (dust, spilled maize) where a run failed, every merge rings the camino real, and the caravan with its *arriero* reaches the market with confetti (or fireflies at night) at each release. Choose the pace (*stroll*, 3 minutes; *trot*; *gallop*, 45 seconds), pause it, or tap the strip under the map to jump to a date.

Drag to travel the road; zoom with the **+ / −** buttons or Ctrl and the wheel. The map keeps its shape at any zoom. It follows the day or night theme of the top bar, and holds still when motion is off.

## Where the data comes from {#where-the-data-comes-from}

- **Runs** come from rendimiento's own database: every run of the last 14 days, with the steps that failed and the release it made.
- **Branches** come from GitHub, through the app's installation: the 100 most recently updated pull requests (merged or not, their base branch) and, for each branch, the commit it started from. For a merged branch that is found against the base just before the merge (the first parent of its merge or squash commit), since afterwards the base already contains it.
- What GitHub said about commits never changes, so it is remembered; the whole history is rebuilt when a newer run appears, or after two minutes. At most 60 branches, the most recently active, are looked up.
- If GitHub cannot answer, the tab still draws what it knows and says some branches may lack their pull request or starting point.

## In the code {#in-the-code}

| What | Where |
|---|---|
| Runs with their failed steps and release | `store.RunHistory` |
| Branches, forks and merges from the runs and GitHub | `internal/caminos` (`Build`) |
| GitHub calls and the cache | `platform/caminos.go` (`Platform.Caminos`), `github` (`RecentPullRequests`, `MergeBase`, `FirstParent`) |
| The API | `GET /api/apps/{app}/caminos` |
| The canvas and the panel | `web/src/components/caminos.ts`, `caminos-tab.tsx` |
| The live copies: what each runs, its memory, those deleted and why | `platform/copias_vista.go` (`CopiasOf`, `RemakeCopy`, `ExtendCopy`, `DeleteCopy`), `store.Copias` (table `copias`) |
| Their API | `GET /api/apps/{app}/copias`; `POST …/copias/{n}/rehacer`, `POST …/copias/{n}/extender`, `DELETE …/copias/{n}` |
| Their section | `web/src/components/copias.tsx` |
