# Preview environments {#preview-environments}

**Status: approved (decision 136), slices 1 to 3 built.** `previews:` is in [rendimiento.yaml](../guide/spec.md#top-level). When a branch's run passes and the branch has an open pull request from the app's own repository, its copy is written as the App object `<app>-pr-<number>` with the images that run built (nothing is built twice), and a **preview** check on the commit links to the copy's address (`https://pr-<number>.<domain>`, or `<name>-pr-<number>.<zone>` for a subdomain, so a proxy's wildcard certificate covers it). The copy goes when the pull request is closed or merged (the webhook), and a sweep every 10 minutes deletes copies of closed pull requests and those idle past `keep`; over `max`, the copy idle longest goes first. One comment on the pull request, edited on each push, gives the address, the commit and the day the copy goes, and says when and why it was deleted. Copies run on a cluster, not on a computer. The agent (slice 4) comes next. p0dxD answered the open questions: the tips app if it has browser tests (else ping-demo), up to 10% of the cluster with 3 copies per app, and **no `copy`** of production's data for now. This page is decision 95, step 12 of the plan: each branch or pull request gets its own live copy at its own address, and an agent walks through it and comments on the pull request before a person merges. Nothing here runs today; it says what to build, in what order, and what it costs. The [roadmap](roadmap.md) points here.

## What a person sees {#what-a-person-sees}

On a pull request of an app that asked for previews (`previews:` in `rendimiento.yaml`, off by default):

1. A check on the pull request, **preview**, whose link is the copy's address (`https://pr-42.<app host>`).
2. One comment, **kept and edited** (never a new one per push): the address, the commit it shows, when it will be deleted, and what the agent found.
3. When the person merges or closes the pull request, the copy goes away and the comment says so.

## One copy per pull request {#one-copy}

- **Where it lives.** A namespace `<app>-pr-<number>`, not containers beside the app's: `render.Render` already takes the namespace apart from the app's name (`Input.Namespace`), so the same release objects are rendered for the copy with its own namespace, a `pr-<number>` host prefix, one replica per service and the label `rendimiento.ai/preview=<number>`. A namespace gives the NetworkPolicy, quota and deletion for free: deleting it deletes everything.
- **Which pull requests.** The ones from the app's own repository. A fork's pull request is never built (its code would run with the clone token), so it never gets a copy.
- **The image.** The one the branch's run already built and passed. No second build: the preview deploys after the run's steps pass, like a release, but never touches the app's real `App` object, its history, its verification or its rollbacks.
- **The trigger.** The GitHub webhook handler (`internal/api/server.go`) ignores `pull_request` events today because a push already runs the branch. It must subscribe to `pull_request` (opened, reopened, synchronize, closed) and hand them to `platform`, which finds the run of the head commit. The GitHub App already has the *pull requests: write* permission it needs for comments.
- **The address.** `pr-<number>.<app host>`, under the DNS zones the app already has, checked like any address (it must not collide with another app's). Without a wildcard zone, no preview: the page of the app says why.

## Data {#data}

Three choices, in this order, one per app in `previews.data`:

| `data:` | What the copy has | When to use it |
|---|---|---|
| `empty` (the default) | A new empty database made by the app's `needs: [postgres]`, its migrations run by the pre-deploy tasks | Almost always: nothing real can leak and it costs no space |
| `seed` | The same, then a task of the app (`previews.seed`) that loads sample data | Apps whose pages are empty without data (a tips app needs people, venues and a few tips) |
| `copy` | A copy of the app's last nightly backup, with the columns the app lists in `previews.scrub` emptied | Only when an empty or seeded database cannot show the change; never without `scrub`, and the person turns it on in Settings |

Secrets of the copy are the app's **preview** secrets, never production's: an app that needs a payment key uses its test key there. Redis (`needs: [redis]`) is always empty. Volumes are not copied.

## When they are deleted {#when-deleted}

- On **closing or merging** the pull request, at once (the namespace and the DNS record).
- After **7 days with no push** (`previews.keep`, up to 30), so an abandoned pull request does not hold a copy for ever.
- At most **3 live copies per app** (`previews.max`): the next one replaces the one idle longest, and says so in its comment.
- A loop in `platform`, like `mergeLoop`, deletes any namespace with the preview label whose pull request is closed or gone, so a missed webhook does not leave a copy behind.

## What the agent does {#what-the-agent-does}

When the copy answers, a **verification agent** runs, as one of the platform's agents (so on the AI gateway, within the organization's cap, recorded in the Ruta):

1. **With the app's browser tests.** If the app has them (the `web/e2e` kind, Playwright), they run as a task against the copy's address, in the Playwright image, with the copy's own test user. They are the first choice because they are deterministic and cost no AI.
2. **With generated tests (step 13).** When the app has none, or the diff touches pages they do not cover, the agent generates a walk-through from the diff and the pages it can reach, runs it with the same Playwright image, and keeps the script in the comment, so a person can add it to the repository.
3. **Reading the result.** It looks at failed steps, screenshots, console errors, and the copy's traces and logs (the app's `observe:` data, in the copy's namespace), and writes what it did, what passed, what failed and what it could not check.

The agent **comments; it never approves, merges or deploys**: the merge stays a person's, as everywhere else in rendimiento. If it cannot reach the copy or the tests are flaky, it says so rather than say it passed.

## What it costs {#cost}

- **Cluster:** one replica per service, requests at a quarter of the app's, a `ResourceQuota` per preview namespace, and the cap of 3 copies. On a Raspberry Pi cluster that is the number that matters: the page of the app shows the memory its live copies hold, and the Environment checks warn when previews take more than a share of the cluster (10% by default).
- **Database:** an empty database is free. `copy` costs the size of the backup per copy, which is why it is opt-in and capped.
- **AI:** each verification counts on the organization's monthly cap; generated tests cost more than the app's own. The app's page shows what the agent spent per pull request, and the verification can be set to *only the app's tests*.
- **Builds:** none extra: it reuses the branch's run.

## Order of work {#order-of-work}

Each slice is merged on its own and useful alone:

1. **The spec and the render:** `previews:` in `internal/spec` (validate, CRD markers, `web/src/api.ts`, an example in `internal/spec/ejemplos`) and the preview namespace in `internal/render`, with envtests.
2. **The copy:** the `pull_request` webhook, deploying the passed run, the address, the check with its link, and deleting on close, idle and the cap.
3. **The comment:** one edited comment with the link and the deadline.
4. **The agent, with the app's tests**, then **with generated tests** (step 13).
5. **Data:** `seed`, then `copy` with `scrub`.

The goal is met when a pull request of the tips app shows the link to its preview and the agent's comment. Until slice 4 the comment is only the link and the deadline.

## Open questions {#open-questions}

- Which tips app, and does it have browser tests today? Slice 4 depends on it.
- Is the 10% cluster share right for the Pi cluster, or should previews be off there and only run on the larger nodes?
- Should `copy` exist at all, or is `seed` enough? It is the only slice that puts real data outside production.
