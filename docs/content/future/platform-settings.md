# Platform settings from the Environment page (design)

**Status: proposed, waiting for approval.** Nothing in this page is built yet. It answers encargo 25: today, turning on the boundary between organizations means editing the private ConfigMap by hand, running `make deploy` on the cluster's machine and restarting the pods. It should be done from rendimiento. Approve this page (or change it) and the work follows in the phases at the end.

The page builds on what already exists: the boundary between organizations (`ORG_NETWORK_POLICY` and friends, see [Security](../architecture/security.md)) is merged, and the capacity page (`GET/PUT /api/environment/capacity`, saved in `platform_settings`) is the model.

## What can change live, and what cannot

A setting is **live** when the platform can read it again while it runs, and nothing it already built depends on it at boot. The rest stay in the ConfigMap, because the platform needs them to start (or to know where to read the live ones from).

| Setting | Live? | Why |
|---|---|---|
| `ORG_NETWORK_POLICY` | yes | the controller renders the policy at each reconcile |
| `INGRESS_NAMESPACE` | yes | goes into the same policy |
| `ORG_NETWORK_POLICY_NAMESPACES` | yes | same |
| `ORG_NETWORK_POLICY_CIDRS` | yes | same |
| `ORG_QUOTA_CPU`, `ORG_QUOTA_MEMORY` | yes | read when a release is checked |
| `BASE_URL`, `DATABASE_URL`, `SECRETS_KEY_FILE`, tokens, `GITHUB_*` | no | needed to start, or to open the database where the live values are kept; secrets never go to the page |
| `MODE`, `ROLE`, `INGRESS_CLASS`, `STORAGE_CLASS`, `REGISTRY`, `DNS_*`, `CLUSTER_ISSUER` | no | they change what already exists (ingresses, volumes, records), so they need a deploy and a look at the result |
| capacity (`PARALLEL_RUNS`, …) | already | its own section of the page, unchanged |

The new page shows **every** setting with its explanation, where its value comes from, and whether it can be changed there. The ones that cannot say why and where to change them. Only the live ones have an edit control. The list lives in the code as one table (name, group, validator, explanation key), so a setting is made live by adding a row, never by a new page.

## Where they are kept, and which value wins

- In **Postgres**, in the `platform_settings` table the capacity already uses: one row per setting (`key = 'env:ORG_QUOTA_CPU'`, the value as a JSON string, who and when). No new table and no new column: the migration has nothing to add, and the previous version simply ignores those rows, so an update running both versions at once is safe.
- The **ConfigMap is the initial value**. A process starts with it (as today) and then reads the saved rows.
- **The saved value wins, setting by setting.** A setting with no row follows the ConfigMap (and the built-in default under it), so changing the ConfigMap still works for everything nobody changed on the page. A setting with a row ignores the ConfigMap until the row is removed with **Back to the ConfigMap's value**.
- When the two differ, the page says so, showing both: "saved: `true` by ana on 12 Nov; the ConfigMap says `false` and is ignored". This is the one surprise of the design, which is why it is on the page and in the [configuration reference](../reference/config.md), not hidden.
- If the database cannot be read, each process keeps the last value it had; at boot, with nothing read yet, it uses the ConfigMap. A saved value that no longer validates (say, a namespace deleted since) is ignored with a problem recorded, and the ConfigMap's value is used.

## How it takes effect, with no restart

Every process (`api`, `control`, `worker`) holds the live settings in a holder like the capacity's, filled at boot from the ConfigMap and refreshed from Postgres every 15 seconds; the process that saves refreshes at once.

- **Quota:** `platform.Config.OrgQuota` is read through the holder when a release is checked. Nothing else to do.
- **Boundary:** the controller reads `render.Options.OrgNetwork` through the holder at each reconcile (today it is fixed at boot). When the saved value changes, it queues every app, so policies appear (or go away) within about a minute instead of the five-minute resync.
- Turning the boundary off removes the policies at the next reconcile, as the book already says.

## Who can change them, and the bitácora

- **Reading and changing** need the **admin** role in the platform's own organization (a `plat(store.MemberAdmin, …)` route, like creating organizations): the ranges and namespaces show how the cluster is laid out, so the page is not shown to collaborators or guests. People of other organizations never see it. Agent keys cannot change settings; the route is for people with a session.
- Every change is a row in the **bitácora** of the platform's organization: action `ajuste.cambiar` (or `ajuste.restablecer`), on `{ajuste: ORG_QUOTA_CPU}`, who (`persona`, their login), through `interfaz`, and the old and new value in `detalle`. The value is the setting itself, never a secret, so it is safe to log in full. A change the validator refuses is not saved; it answers `400` with the reason and writes no row.
- The row is written in the same request as the change; the page links to the bitácora filtered by that action.

## Validation

Each change is validated in the API before saving, and the whole change as one (so `ORG_NETWORK_POLICY=true` with an empty ingress namespace is refused, as at boot today):

- booleans are `true` or `false`;
- CIDRs go through `net.ParseCIDR`, at most 50 of them;
- each namespace is a valid DNS label **and exists** (a `Get` on the namespace; the platform already may read them, so no new permission);
- quotas are positive Kubernetes quantities (`4`, `8Gi`), as at boot.

The validators are the ones `cmd/rendimiento/frontera.go` uses at boot, moved to a package both share, so the boot and the page cannot disagree.

## The warning before turning the policy on

Before saving `ORG_NETWORK_POLICY=true`, the page asks `GET /api/environment/settings/boundary-check` and shows the **namespaces that call the apps today and would be blocked**: those outside what the policy lets in (the platform's, the ingress controller's, the listed ones, and each organization's own).

rendimiento cannot see real traffic (it has no flow logs), so the list is of **likely callers**, from what the cluster declares:

- the namespaces of ingress controllers (by `IngressClass`);
- namespaces with a `ServiceMonitor`, `PodMonitor` or a scraper's pods that point at app namespaces;
- the namespaces of the add-ons rendimiento applies;
- namespaces whose pods or Services refer to an app's service by its cluster name (`<app>.<namespace>.svc`) in their environment.

Each is shown with why it is in the list and one button, **Let it in**, that adds it to `ORG_NETWORK_POLICY_NAMESPACES`. The page says plainly that it is a guide, not a proof, and keeps the rollback in view: turning the boundary off is one switch, now without a restart.

## Single computer (`MODE=local`) and Helm

- **Single computer:** there is no cluster, no NetworkPolicy and no ConfigMap. The environment variables are the initial values and the saved rows win, in the same way. The boundary settings are shown as "does not apply on one computer" and cannot be edited; the quota does apply (releases are checked here too) and can be changed. The cluster-only routes already answer `503` with that reason when there is no cluster, and the new ones follow it.
- **Helm:** the chart still renders the ConfigMap from its values (`orgBoundary.*`), as the initial values. A `helm upgrade` does not overwrite a saved row; the page shows the difference, as above. No chart change is needed for the permissions (the platform's role already reads namespaces and pods and manages NetworkPolicies). The chart's `values.yaml` comments will say the page is the way to change them afterwards.
- With the split roles (`ROLE=api`, `control`, `worker`) every role refreshes on its own, so the order of restarts does not matter.

## What is built, in order

1. **Server:** the settings table and validators (shared with boot), the holder, `GET/PUT /api/environment/settings`, `DELETE /api/environment/settings/{key}`, the boundary check, the bitácora rows; the controller and the platform read through the holder. Tests: validators, precedence (saved, ConfigMap, default), permissions per role, the bitácora row, the controller re-rendering with the policy on and off (envtest, with its own app and namespace names), the check's list.
2. **Interface:** the page in Environment, beside capacity: every setting with its explanation, source and difference, edit controls for the live ones, the warning before the policy, in English and Mexican Spanish. Tests of the page with the existing UI test setup; `npm run typecheck`.
3. **Book:** `reference/config.md` (which settings are live and the precedence), `architecture/security.md` (turning the boundary on from the page instead of the ConfigMap), both in English and Spanish, and `cambios/<branch>.md`.

## To decide

1. **The saved value wins over the ConfigMap** (above). The alternative, ConfigMap wins, would make the page useless after the first `make deploy`.
2. **Likely callers, not proven ones** for the warning. Proven ones need flow logs (Cilium Hubble or similar), which most clusters here do not have.
3. **Admins of the main organization only**, also to read. Say if collaborators should see the values read-only.
