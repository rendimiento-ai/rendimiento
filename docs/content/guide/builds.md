# Builds and tests

## What triggers a build

| Event | Builds | Deploys | GitHub check |
|---|---|---|---|
| push to the default branch | changed services (see below) | yes, if everything succeeds | yes |
| push to any other branch (including PR branches and Renovate's) | every service | no | yes, shown on the PR |
| a new branch at a commit the default branch already ran (an agent's branch for an [encargo](agents.md), before its first commit) | nothing: that commit's check is already there | — | — |
| push to another branch whose last commit says `[skip ci]`, `[ci skip]` or `[no ci]` (the unfinished work an agent leaves when it gives an encargo back) | nothing | — | — |
| **Build now** button (default branch) | every service | yes | yes |
| **▾ Test a branch**, beside it: one that ran lately, or any by name | every service | no | yes |
| pull request from a fork | nothing | — | — |

## Merging by itself {#merging-by-itself}

An app can merge its own pull requests: **Settings → Merging** (an administrator sets it; a person merges them until then). It has two ways of taking your yes:

- **rendimiento merges what a person approves here** (the one to start with). The yes is given in rendimiento, by a collaborator or an administrator: **Merge this branch** on the run's page, or accepting the [encargo](agents.md) whose links name the branch (`…/pull/new/<branch>`). rendimiento opens the pull request if the branch has none, with the commit's first line as its title and `cambios/<branch>.md` as its text. A run still going can be approved: it is merged when it passes. A pull request someone opened in GitHub is tested and waits for that yes.
- **rendimiento merges any open pull request.** Opening it is the yes, whoever opens it: anyone who can push a branch to the repository can have it merged. A draft waits until it is marked ready, and a dependency update that Renovate or Dependabot left open still waits for a person's yes (they merge by themselves what their rules allow).

Either way:

- What is merged is the commit that was tested and approved: if someone pushes in between, the new commit waits for its own run, and its own yes. The merge is a push to the default branch, so the deploy follows as always. rendimiento then deletes the merged branch.
- A pull request from a fork is never merged by rendimiento.
- In a repository with several apps, the commit must pass in every one that ran it.
- What GitHub asks of the pull request still holds: when a rule of the branch wants a review or a check, the run says it waits for that, and merges when it is given.
- **A conflict where both sides only added lines is joined by rendimiento.** Two branches that each add a line after the same one (a page to the book's menu, a phrase to the translations) conflict for git, though nothing needs deciding. rendimiento merges the default branch into the branch with a three-way merge: when, in every file both changed, the two sides only added lines at the same places, it keeps both (the default branch's first), commits the merge on the branch through GitHub, and tests it. The run says which files it joined. The yes given to the branch carries over to that commit, since it holds nothing new: it is merged when it passes. If anything else conflicts (a line both changed, a file one deleted and the other changed), nothing is committed.
- **Any other conflict is never merged.** The run names the files, and its page offers **Ask an agent to resolve it**: that makes an [encargo](agents.md), estimated like any other: a simple one goes to the platform's [coding agent](agents.md#the-coding-agent), which merges without git (`merge_default_branch`), by itself in the mode *what rendimiento opens, if cheap*; a hard one to your own agent. The new commit runs, and is merged if it passes.

### Just opening the pull request {#just-opening-the-pull-request}

Beside **Merge this branch**, the run's page offers **Just open the pull request** (on GitLab, the merge request). It opens the branch's pull request into the default branch with the same title and text (the commit's first line and `cambios/<branch>.md`), and nothing more: nobody's yes is written, and nothing is merged. If the branch already has one open, the page says so and links to it. Where the app has [`previews:`](../future/previews.md), opening it is what gives the branch its live copy, as for any pull request, so you can try it before deciding. It is shown wherever **Merge this branch** is, also on a run that *Passed, with a warning*, and taken by the same people (a collaborator or an administrator); an agent's key cannot press it. In an app that merges any open pull request, opening one is the yes, so it is offered only on a run that waits for a person anyway (what no step tests, a check that failed en seco).

### What no step tests {#what-no-step-tests}

A run that passed vouches only for what its steps read. When a branch also changes a folder no step builds or tests (a `mobile/` app beside services in `api/` and `web/`, say), its run still passes, but:

- rendimiento does **not** merge it by itself: an open pull request is not the yes for it, only a person's **Merge this branch** (or an accepted encargo) is.
- Its GitHub check concludes **Action required** instead of success, titled *No step builds or tests `mobile/`: a person must merge this*. Tools that wait for green checks wait too: Renovate does not automerge it, nor does GitHub when the branch's rules require the check.
- The run's page names those paths. A person's yes there turns the check green and merges it.

The same holds for a branch whose checks failed [en seco](tasks.md#dry-runs-on-branches), against the published release or a copy of the branch: the button then says **Merge anyway**, and the yes is noted in the Ruta.

What counts as tested, on a branch's run: the folder of each built service, build and job, with its `watch` paths and its Dockerfile; and the folder and `watch` paths of each task with `when: always` (a task that runs only on deploys, or before or after one, tests no branch). A step whose folder is the root (`path: .`, or none for a service) reads the whole repository, so then everything is tested, root lockfiles included. `rendimiento.yaml` always counts, since the run reads it, and so do files no build reads: Markdown, licences, and the settings of git, editors and Renovate (`renovate.json`). To have a folder tested, give it a step:

```yaml
tasks:
  - name: mobile
    image: node:22
    path: mobile
    command: npm ci && npm test
    when: always
```

## Dockerfile or Railpack

| The service's folder has… | rendimiento uses… |
|---|---|
| a `Dockerfile` (or `build.dockerfile` points to one) | the Dockerfile, with `GIT_SHA` as a build argument |
| no Dockerfile | **Railpack**, which detects the language and builds from source |
| either, with `build.builder: dockerfile` or `railpack` | what you asked for |

Railpack is the quickest start; a Dockerfile gives smaller, hardened images and full control. [Details](../architecture/builds.md#railpack-no-dockerfile-needed). The first line of a build's log says which builder was used; the next says which BuildKit daemon.

## Tests

```yaml
test:
  image: python:3.12-slim
  command: pip install -r requirements.txt && pytest -q
```

The test runs in its own pod, in the service's folder of a fresh clone, at the same time as the build. A failure stops the build (*stopped: … failed*) and fails the run, nothing is released, and GitHub shows a red check. Test pods have the same network isolation as builds (the internet for dependencies, nothing inside the cluster).

A larger test suite can ask for more:

```yaml
test:
  image: golang:1.27
  command: go test ./...
  resources: { cpu: "1", memory: 2Gi, memoryLimit: 5Gi }   # or size: large
  timeout: 1800
  env: { CGO_ENABLED: "0" }
  postgres: true    # a throwaway database: DATABASE_URL
  cache: true       # /cache is kept between runs
```

- **Resources.** Without `size` or `resources`, a test requests 250m CPU and 256 MiB and may use up to 2 GiB; `size` and `resources` work as for services.
- **`postgres: true`** starts an empty PostgreSQL next to the tests, in the same pod; the tests start once it accepts connections, and it is gone when they end. Its address is in `DATABASE_URL` (`postgres://postgres:test@127.0.0.1:5432/test`).
- **`cache: true`** keeps a volume at `/cache` from one run to the next, so dependencies are not downloaded and compiled every time. Go, npm and pip are pointed there (`GOCACHE`, `GOMODCACHE`, `npm_config_cache`, `PIP_CACHE_DIR`, `XDG_CACHE_HOME`); other tools can use `/cache` themselves. There are two volumes per app and test (the Environment page's **Caches of each test** changes how many), deleted with the app; a run takes the first that no other run's test is using, so the second is the one that fills when two runs go at once. Branch and pull request runs share it, so a cache only speeds tests up: never let a test trust what it finds there. The images that are deployed are built by BuildKit and never read it. With a node-local storage class the test runs on the node that keeps its cache; when that node has no room for three minutes, the cache is deleted and the test starts with an empty one on another node, instead of waiting. If the node is only busy (its CPU or memory taken by other runs' steps, which end in minutes, as when runs of several branches go at once), the test waits up to ten minutes more first, since starting cold costs more. And when other runs' tests are using every cache right then, the waiting test runs without one that once, rather than delete a cache in use. With a network storage class instead (Longhorn), the cache goes wherever the test runs and is never deleted for room; when other runs' tests have every cache, a test waits up to ten minutes for one of them to let go (a test with its cache takes minutes, one without many times longer), and only then goes without it. A Longhorn class with one replica suits it: a lost cache only costs one slow run.

## Images that are not services {#images-that-are-not-services}

`builds:` lists images the repository produces that no service of the app runs: an image other apps or tools use, or, in rendimiento's own repository, rendimiento itself. Each is tested and built like a service (same `path`, `watch`, `build` and `test` fields), and its digest is kept with the release, but nothing is deployed from it.

```yaml
builds:
  - name: platform
    path: .
    build: { dockerfile: Dockerfile }
    test: { image: golang:1.27, command: go test ./..., postgres: true, cache: true }
```

The image is pushed as `<registry>/<app>-<name>`; the run page shows `platform:test` and `platform:build`. [How rendimiento builds itself](../environment/deploy.md#rendimiento-builds-itself).

## Only what changed is rebuilt

On a push to the default branch, a service whose folder did not change since the last release keeps its image; its steps show *reused*. List extra folders it depends on with `watch:`:

```yaml
services:
  - name: api
    path: api
    watch: [shared]        # changes under shared/ also rebuild api
```

Everything is rebuilt when `rendimiento.yaml` changes, for manual runs, for services at the repository root, or when the comparison is uncertain.

## How long builds take

The first build of a service is the slowest: base images and dependencies are downloaded, and native packages (Python wheels, for example) may compile. After that, each image builds on the same BuildKit daemon every time and reuses its cache; a code-only change typically takes seconds to a couple of minutes. Several apps building at once use different nodes. A step may run up to 45 minutes (`STEP_TIMEOUT`); at most 4 run at once across the cluster (`MAX_PARALLEL_STEPS`).

## Reading a failed build

The run page shows each step's log. For builds, BuildKit's output shows each Dockerfile (or Railpack) step, its cache status (`CACHED` or the time it took) and, on failure, the command's output. The GitHub check links to the run.

## Retrying what failed {#retrying-what-failed}

Sometimes a step fails for the cluster, not for the code: a test environment that timed out under load, a browser test that ran out of time. **Retry what failed**, on the page of a failed run, runs again in the **same run** only the steps that failed and those skipped because of them; the steps that passed keep their result, and a build keeps its image, so nothing is rebuilt. When it passes, the run ends as if it had passed the first time: on a branch its GitHub check turns green and merging by itself takes it; on the main branch the release is made with the images already built.

- **Who:** collaborators on a branch. A run of the main branch deploys, so only the app's administrators retry it, and only the branch's latest run (an older one would deploy an older commit). Agents retry runs of branches with `ejecucion_reintentar`, never the main branch's. Each retry is in the bitácora.
- **At most twice** per run; after that, push a commit or build it again.
- **The log** of a retried step keeps the earlier attempt, under a line `=== retry 1: running again what did not pass ===`, and the step says which try it is.
- **Flaky steps:** a step that failed and then passed on the same commit stumbled. Three stumbles in 30 days make it **flaky**: the run page marks it, and it shows as a problem. Retrying hides a flaky test; fixing it does not.

## Kinds of branch {#kinds-of-branch}

The app's **Runs** tab sorts its last 50 runs by the kind of their branch, with how many of each: **All**, **Main branch** (the runs that deploy), **With a pull request** (an open one), **Fixes**, **Features**, **Releases**, **Dependencies** and **Other branches**. Only the kinds that have runs show. The filter stays in the address (`?tipo=fix`), so a link opens it.

A branch's kind comes first from the labels of its pull request (the open one, or else the latest), and when none of them says, from the start of its name:

| Kind | Pull request labels | Branch names starting with |
|---|---|---|
| Fixes | `bug`, `bugfix`, `fix`, `hotfix`, `defect`, `regression`, `arreglo`, `error` | `fix/`, `fix-`, `bug/`, `bugfix/`, `hotfix/`, `arreglo/`, `arreglo-` |
| Features | `enhancement`, `feature`, `new feature`, `función`, `mejora` | `feat/`, `feat-`, `feature/`, `funcion/`, `funcion-` |
| Releases | `release`, `version`, `versión` | `release/`, `release-`, `version-`, `changelog-v` |
| Dependencies | `dependencies`, `deps`, `renovate`, `dependabot` | `renovate/`, `dependabot/` |

Labels are read without case and without a `type:` or `kind/` before them (`Type: Bug` is a fix). A label says more than the name: `fix/login` with the label `enhancement` is a feature. The default branch is always the main one. Each run's branch carries its kind and its pull request's number beside its name. What GitHub says of the pull requests is kept two minutes; when GitHub does not answer, the kinds come from the names only, and the tab says so.
