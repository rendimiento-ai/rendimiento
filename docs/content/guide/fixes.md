# Declared fixes

Some failures are not in the code but in a file a tool writes: a `package-lock.json` that no longer matches `package.json`, a `go.sum` without the entry for a new module. Nobody should edit those by hand, and the coding agent cannot run `npm`. A **declared fix** is the command that regenerates such a file, written in `rendimiento.yaml` under `fixes:`. rendimiento runs it on the branch that needs it, in a pod of its build namespace, and keeps only the files the fix may write, as a commit on that same branch, whose normal run tests it.

```yaml
fixes:
  - name: web-lockfile          # a recipe: its command, image and files
    recipe: npm
    path: web
  - name: mobile-lockfile       # a command of the app's own
    path: mobile
    image: node:22-bookworm
    command: npm install --package-lock-only --ignore-scripts --no-audit --no-fund
    writes: [package-lock.json]
    timeout: 600
```

The fields are in the [reference](spec.md#fixes).

## Recipes {#recipes}

A recipe names a known fix. It brings its command, the files it may write, and its image, whose version comes from what the app already uses in that folder.

| Recipe | Command | May write | Image's version from |
|---|---|---|---|
| `npm` | `npm install --package-lock-only --ignore-scripts --no-audit --no-fund` (with the npm that `packageManager` names, if it does) | `package-lock.json` | `.nvmrc`, `.node-version`, `engines.node`, the folder's `Dockerfile` (`FROM node:…`); else Node 22 |
| `pnpm` | `pnpm install --lockfile-only --ignore-scripts`, through corepack | `pnpm-lock.yaml` | as for npm |
| `yarn` | Yarn 2 and later: `yarn install --mode update-lockfile` without scripts; Yarn 1: `yarn install --ignore-scripts` | `yarn.lock` | as for npm |
| `go` | `go mod tidy` | `go.mod`, `go.sum` | `go.mod`'s `toolchain` or `go` line, the `Dockerfile` (`FROM golang:…`); else Go 1.24 |
| `uv` | `uv lock` | `uv.lock` | `.python-version`, `pyproject.toml`'s `requires-python`, the `Dockerfile`; else Python 3.12 |
| `poetry` | `poetry lock` | `poetry.lock` | as for uv |
| `pip-tools` | `pip-compile` | `requirements.txt` | as for uv |

The fix's log says which image it used and why ("version from .nvmrc", or "no version found in the folder: the default"). A command of the app's own (`image`, `command`, `writes`) runs as written.

## How a fix runs {#how-a-fix-runs}

1. **Someone asks**: the coding agent (`run_fix`), a person with the button on a failed run's page, or rendimiento itself on a Renovate branch.
2. **The definition comes from the default branch.** rendimiento reads `fixes:` from the default branch's `rendimiento.yaml`, never from the branch being fixed: a pull request cannot add a command and have it run. A fix never runs on the default branch either: it only gets what a person merges.
3. **A pod in the build namespace** (`rendimiento-builds`) clones the branch at the commit, lays the caller's uncommitted files over it (the coding agent's changes), and runs the command with `sh -c` in the fix's folder. It gets no secrets and none of the app's environment, only `CI=true`; the network is there, for the package registries. The recipes pass `--ignore-scripts` (or the tool's equivalent), so the packages' install scripts do not run.
4. **Only what it may write comes back**: the files that changed under its folder and match its patterns, up to 20 MB. Anything else it changed is left out, and the log lists it.
5. **Nothing is kept untested.** The files become a commit on the same branch (or join the coding agent's changes, which its next `commit_and_test` commits), and the branch's normal run tests them.

A fix stops after its `timeout` (five minutes unless it says otherwise, and never longer than the platform's step timeout). Every fix keeps its whole log, like a step, with who asked, the files it wrote and what it left out.

## Where you see it {#where-you-see-it}

**On a failed run's page.** When a failed step's log says a lockfile is stale (npm's *package.json and package-lock.json are not in sync* or *lock file … does not satisfy*, pnpm's `ERR_PNPM_OUTDATED_LOCKFILE`, yarn's *lockfile needs to be updated*, Go's *missing go.sum entry* or *updates to go.mod needed*, uv's and Poetry's lock mismatches) and the app declares a fix whose folder holds the step's, the page says so and offers:

- **Regenerate &lt;file&gt;**: runs the fix on the run's branch and commits what it wrote, with you as co-author; a new run follows. No AI is involved, so it costs no AI money. Anyone who can launch runs may press it; it never appears for the default branch.
- **Ask the agent**: an encargo for the coding agent, which runs the fix with `run_fix` and goes on from there.

When the app declares no fix for that folder, the page offers **Add &lt;recipe&gt; fix to rendimiento.yaml** instead: a branch from the default one that adds the recipe to `fixes:`, keeping the file's comments. You open its pull request and merge it; from then on the button regenerates the file.

**In an encargo's history.** Each `run_fix` of the coding agent is a line, with the files it wrote and how long it took.

**On a Renovate branch, by itself.** See [Renovate](renovate.md#lockfiles).

## Limits {#limits}

- Only what is declared runs. rendimiento recognizes the kind of project, but it does not offer recipes nobody declared.
- A fix of the app's own (a command, not a recipe) runs only when someone asks for it: Renovate's branches get only recipes.
- On a computer without Kubernetes (`MODE=local`) fixes do not run yet: there is no build namespace.
