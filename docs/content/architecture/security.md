# Security model

A deployment platform can run anyone's code and change anything in the cluster, so its security model deserves a chapter. This one says what is protected, how, and what is not yet.

## Identities

| Identity | Can | Used by |
|---|---|---|
| **People** ([members](../guide/members.md), by GitHub login) | by role: guests read; collaborators also run CI, write the Ruta and ask agents; administrators also deploy, roll back, and manage secrets, keys, add-ons and members | the dashboard |
| **The GitHub App** (installation tokens, 1 hour) | read code, push branches, open PRs, write checks, manage issues (Renovate), in installed repos only | the platform, build pods (clone), Renovate |
| **`rendimiento`** service account | manage App/Addon objects; namespaces, Deployments, Services, Ingresses, CronJobs, PVCs, Secrets cluster-wide; build pods in `rendimiento-builds`; read nodes, metrics, CRDs; **impersonate `rendimiento-addons` only** | the platform process |
| **`rendimiento-addons`** service account | `cluster-admin`; no pod runs as it | the add-on controller, by impersonation |
| **Build pods** | nothing in Kubernetes (no token mounted) | CI steps, Renovate |

## Isolation of untrusted code

Code from repositories runs in CI: tests and Dockerfile `RUN` lines. It is contained by layers:

1. **No credentials**: build pods get no Kubernetes service-account token; the only secret is a clone token for that one run.
2. **Network**: the `isolate-builds` NetworkPolicy allows DNS, the BuildKit daemons and the public internet only. Other apps, databases, the Kubernetes API, the node network and the home LAN (`192.168.0.0/16`) are unreachable. This was verified from test pods on every node.
3. **Nodes**: builds never run on the control plane or on nodes listed in `BUILD_EXCLUDE_NODES` (for example, nodes that cannot enforce NetworkPolicy).
4. **Limits**: a deadline per step, memory limits per test pod.
5. **Forks**: pull requests from forks are never built.

BuildKit daemons themselves run **privileged** (they create containers); build pods talk to them over TCP. A malicious `RUN` executes inside BuildKit's sandbox, not in the daemon's own container, but a BuildKit escape would reach that node. Rootless BuildKit is on the [roadmap](../future/roadmap.md).

## Organizations in the cluster {#organizations-in-the-cluster}

On a cluster, each organization's apps get a wall made for the teams of one company: it stops accidents and curiosity, not a determined attacker. Its network part and its quota are **off by default**, so an update changes nothing until the operator turns them on.

- **Labels, always.** Each App object carries its organization's address in the label `rendimiento.ai/org`, and so does the namespace rendimiento makes for it. An App made before carries none and counts as the platform's (`principal`) until its next release writes the label. The namespace of an app with `sharedNamespace` belongs to someone else and is never labeled.
- **A network policy, when turned on** (`ORG_NETWORK_POLICY=true`; Helm `orgBoundary.networkPolicy`). Each app namespace rendimiento owns gets the NetworkPolicy `rendimiento-org`, which lets traffic in only from:
    - its own pods (a service and its `needs:` database, deploy tasks);
    - the namespaces of the same organization (one app calling another's service);
    - the platform's namespace: uptime checks and release verification call each service at its cluster address;
    - the ingress controller's namespace (`INGRESS_NAMESPACE`): everything that comes by a domain, including public uptime checks and cert-manager's challenges;
    - the namespaces and address ranges the operator lists (`ORG_NETWORK_POLICY_NAMESPACES`, `ORG_NETWORK_POLICY_CIDRS`): Prometheus or another scraper, or the nodes' network when the ingress controller runs on the host network.

    A service with `lan:` gets a second policy, `<service>-lan`, that opens its own port to everyone, since the LAN reaches it through the load balancer from addresses no namespace matches. Egress stays open: apps still reach the internet, the traces collector and services of other organizations that let them in. A namespace with `sharedNamespace` gets no policy.
- **A quota, when set** (`ORG_QUOTA_CPU`, `ORG_QUOTA_MEMORY`; Helm `orgBoundary.quota`). A Kubernetes ResourceQuota counts one namespace, and an organization has many, so the platform counts instead: before it makes a release of an app of any organization but the platform's, it adds that release's requests (each service and each `needs:` database or cache, times its replicas) to those of the latest release of each other app of the organization, and refuses the release when the total passes the quota. The run says why. Cron jobs and deploy tasks come and go and are not counted, rollbacks are not checked, and nothing stops someone with `kubectl` from going past it.

What the wall is not:

- **One service account.** The `rendimiento` service account still manages every organization's namespaces; what keeps them apart is the platform's code, not Kubernetes permissions. A service account per organization is not built.
- **Shared builds.** Builds are not per organization: every CI step runs in `rendimiento-builds`, on the same privileged BuildKit daemons, and pushes to the same registry, which every node pulls from without credentials.
- **Only where the network enforces it.** A NetworkPolicy holds only where the cluster's network plugin enforces it (Calico, Cilium, k3s's own network policy controller). Plain flannel, or k3s started with `--disable-network-policy`, accepts the object and enforces nothing.

### Turning it on {#turning-the-boundary-on}

1. Deploy the release that brings it (`make deploy`, or `helm upgrade`): the platform's service account gets the right to write network policies.
2. Find what calls apps from other namespaces: the ingress controller (`INGRESS_NAMESPACE`; empty, `ingress-nginx` for the `nginx` class and `kube-system` for `traefik`), a metrics scraper, an add-on. List them in `ORG_NETWORK_POLICY_NAMESPACES`. If the ingress controller uses the host network, add the nodes' range to `ORG_NETWORK_POLICY_CIDRS`.
3. Set `ORG_NETWORK_POLICY=true` and restart the platform's services (`kubectl rollout restart deployment -n rendimiento-system`). Each app gets its policy at its next reconcile, within five minutes, before its workloads are touched.
4. Check one app of each organization still answers at its address and on its Uptime tab, and that a pod in another organization's namespace cannot reach it.

To back out, set `ORG_NETWORK_POLICY=false` and restart again: each app's policies are removed at its next reconcile. In a hurry, after that, `kubectl delete networkpolicy -A -l rendimiento.ai/org` removes them all at once (the builds' policy does not carry that label).

## Secrets

- **Never in git in plain text.** Apps reference secrets by name in `rendimiento.yaml`; values are set in the UI (**Settings → Secrets**) or committed as **sealed secrets** (encrypted to the cluster's key).
- **Never in the platform database in plain text.** Postgres holds session hashes, not tokens. The platform's own secrets (the GitHub App's credentials, organizations' AI keys) are Kubernetes Secrets on a cluster. On a computer (`MODE=local`), or on a cluster with `SECRETS_KEY_FILE`, they are **sealed in Postgres** with AES-256-GCM, by a key kept outside the database: a file only rendimiento reads (`~/.config/rendimiento/llave-secretos` on a computer). Each value is bound to its name, so a sealed value copied under another name does not open (`internal/secretos`). On a computer, apps' secrets and the passwords of their Postgres and Redis are sealed the same way. Whoever holds both the database and the key file can open them: keep the key's copy apart from the database's backups.
- **Generated when possible.** `needs:` credentials are generated once, stored in a Secret, and injected by reference (`secretKeyRef`), so the password never appears in a spec or a log.
- **Short-lived** where possible: GitHub installation tokens last an hour and are minted per build and per Renovate run.
- **Not logged.** Renovate's log parser, build logs and error messages never include tokens.

## The web surface

- GitHub OAuth login; sessions are random tokens stored hashed, valid 7 days, checked on every request against the person's membership and the route's role.
- Cookies are `HttpOnly`, `Secure` (on HTTPS) and `SameSite=Lax`; state-changing requests from another origin are refused.
- Webhooks are verified by HMAC signature with a constant-time comparison.
- The GitHub App setup page needs a one-time setup token.
- Security headers: no MIME sniffing, no framing, same-origin referrers.

## The bitácora {#the-bitacora}

Every action is written to the **bitácora**, one row each, by people and by agents alike:

- **People**: every API route that changes something (all but `GET`) records who did it, the route (`POST /api/apps/{app}/rollback`), what it was on (its path values: the app, the run, a secret's *name*) and the HTTP status it ended with. Bodies are never kept, since they can hold secrets. A test walks every registered route, so a new one cannot escape it (`TestEveryChangeIsInTheBitacora`).
- **Agents**: every MCP tool call records the key, the tool, its arguments (hidden when they look like a secret) and how it ended.
- **A chain per organization**: each row carries the hash of the one before it. A row changed or removed in the database breaks the chain from there, and `GET /api/bitacora/verificar` says where.

Collaborators and administrators read it on the **Bitácora** page: whether the chain holds, filters by who, action or app, and an export of the rows shown as JSONL (`GET /api/bitacora`). Guests and demo links do not.

## Validation at the edges

`rendimiento.yaml` is validated before anything runs: names are DNS labels, paths cannot escape the repository, ingress annotations are limited to `nginx.ingress.kubernetes.io/*` and **snippets are refused** (they allow arbitrary nginx configuration), LAN addresses must be private, secret references are checked. The controller refuses namespaces and hostnames it does not own.

## Known gaps

These are the honest weak points, in rough order of importance for a multi-user or public deployment:

1. **Organizations are a boundary in the API, not yet below it.** People have roles (guest, collaborator, administrator) within organizations, and each app belongs to one. Every API route is called in a test by another organization's administrator with the first one's names and numbers, and finds nothing of it (`TestEveryRouteStaysInItsOrg`, which walks the routes as they are registered, so a new one is checked too). The MCP tools check the key's organization and its apps. What is missing: a role covers every app of its organization (no per-app owners or viewers), and on a cluster one service account manages every app's namespace, so the boundary is the platform's code, not Kubernetes. That is needed before strangers share a cluster. See [Becoming an open-source project](../future/open-source.md).
2. **Cluster-wide permissions** for the platform (to create namespaces for any app). A more restricted mode would give each app a namespace that an administrator pre-creates.
3. **Privileged BuildKit** daemons.
4. **Plain-HTTP registry** on the LAN.
5. **Backups stay in the cluster.** Volumes are backed up nightly to Garage, which runs on the same machines, so the backups would not survive losing the whole cluster. An off-site copy is on the roadmap.
6. **The bitácora is not streamed out yet** (to a SIEM, or a copy outside the database). Automatic actions of the platform itself (a rollback after a failed verification, Renovate) are not in it yet.
