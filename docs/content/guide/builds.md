# Builds and tests

## What triggers a build

| Event | Builds | Deploys | GitHub check |
|---|---|---|---|
| push to the default branch | changed services (see below) | yes, if everything succeeds | yes |
| push to any other branch (including PR branches and Renovate's) | every service | no | yes, shown on the PR |
| **Build now** button (default branch) | every service | yes | yes |
| **▾ Test a branch**, beside it: one that ran lately, or any by name | every service | no | yes |
| pull request from a fork | nothing | — | — |

## Merging by itself {#merging-by-itself}

An app can merge its own pull requests: **Settings → Merging → rendimiento merges them when the branch passes** (an administrator sets it; it is off until then). With it on:

- A branch with an open pull request into the default branch is merged once its run passes, with the commit that was tested: if someone pushes in between, the new commit waits for its own run. The merge is a push to the default branch, so the deploy follows as always.
- **Opening the pull request is the yes.** A branch without one is only tested, and a draft waits until it is marked ready. A pull request opened after the branch passed is merged within a minute.
- In a repository with several apps, the commit must pass in every one that ran it.
- What GitHub asks of the pull request still holds: when a rule of the branch wants a review or a check, the run says it waits for that, and merges when it is given.
- **A conflict is never merged.** The run says so, and its page offers **Ask an agent to resolve it**: that makes an [encargo](agents.md), which says how hard it is and about what it would cost with the platform's AI before anyone does it. An agent of yours, working in a clone with git, resolves it well; the platform's own agent edits through GitHub without merging the two sides, so it suits small conflicts only. The new commit runs, and is merged if it passes.

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
