# The destination agent

Architecture A of decision 95: rendimiento hosts the panel and the API, and on every cluster or computer an **agent** runs that only connects *out*. The agent is one more implementation of [`Destino`](../reference/code-map.md) (`internal/destino`), like Argo CD managed by Akuity: the platform builds, tests and decides; the agent makes it run and says how it went. Nothing opens a port on the destination's side.

This page is the design (step 11 of the plan). The prototype, with a computer of the house as the destination of the hosted panel, is the last section; what is written here is what the prototype must hold.

## The shape {#the-shape}

```text
  rendimiento (hosted)                       a computer of the house
 ┌──────────────────────┐    wss, out only   ┌─────────────────────────┐
 │ panel + API          │◀───────────────────│ rendimiento agente      │
 │ destino.Remoto       │   commands ──────▶ │  └─ destino.Local       │
 │  (one per agent)     │   ◀── results      │     (engine, Caja, proxy)│
 └──────────────────────┘                    └─────────────────────────┘
```

- **`destino.Remoto`** lives in the platform. It implements `Destino`; each method becomes a command sent to the agent that belongs to the app's destination, and waits for the result.
- **The agent** is the same binary (`rendimiento agente`). It dials the platform, and runs each command on a *local* `Destino`: `destino.Local` on a computer with a container engine; a Kubernetes one later, for clusters.
- The agent is the only side that dials. If the connection drops, it retries with backoff; the platform never tries to reach the agent.

## Protocol {#protocol}

**WebSocket over HTTPS (`wss`), one long-lived connection per agent**, on the platform's own HTTPS port (`/agente/v1/conectar`). Chosen over gRPC because it goes through whatever a home network, a corporate proxy or a CDN lets through (plain 443, HTTP/1.1 upgrade), needs no new listener or certificate in the platform, and the messages are small.

Messages are JSON frames with an `id`, a `tipo` and a body:

| Direction | Frame | What |
|---|---|---|
| agent → platform | `hola` | the first frame: agent version, protocol range, capabilities (`local`, `kubernetes`), the engine's version, a clock reading |
| platform → agent | `orden` | `id`, `metodo` (`crear`, `aplicar`, `estado`, `tarea`, `quitar`, `situacion`, `recursos`, `reiniciar`, `impacto`, `desconectar`, `llaves`, `guardar_secreto`), `app`, the body |
| agent → platform | `resultado` | the `id` of the order, `ok` or an `error` with a text for people |
| agent → platform | `evento` | what changes without being asked: a release becomes healthy or fails, a service dies, task log lines |
| both | `latido` | every 20 s; three missed and the connection is dead |

The methods of `Destino` map one to one on the orders, so a later move to gRPC changes the transport and not the design. The platform keeps what the agent last said (`Situacion`, `Estado`) so the Apps page answers while the agent is offline, marked as *last seen at …*.

**Orders are idempotent and carry their release number.** `aplicar` for release *n* twice is the same as once. A command that finds the agent offline waits in a queue with a deadline (the deploy's own timeout); on reconnect the platform sends the app's **desired state** again (the current release of each app of that destination) and the agent converges, so a missed order is never lost and an old one never overwrites a newer release.

## Credentials per agent {#credentials}

- **A person enrols the agent** from the panel (administrators only): *Destinos → Add computer* shows a one-time enrolment code, valid for 15 minutes, with the command to run. The agent trades the code for its own **credential**: a random 256-bit token, shown to the agent once, kept by the platform only as a hash (as agent keys are), written by the agent in a file with mode `0600`.
- Each credential names **one agent of one organization**. Rotating issues a new token over the live connection and the old one dies after the swap; revoking cuts the connection at once and is recorded in the [bitácora](security.md#the-bitacora).
- The credential never reaches the browser, the logs or the Ruta. It authorizes the connection and nothing else: it cannot read the panel's API.
- Later: client certificates (mTLS) signed by the platform, so a stolen file is not enough without the key's host. The token is the first step, not the end.

## `org_id` from the first day {#org-id}

Organizations already wall the data (see [Security model](security.md)); an agent must not be the hole.

- The table of destinations has `org_id` from its first migration, and each app points to a destination of **its own organization**. A destination belongs to exactly one organization; to use one computer for two organizations, run two agents.
- The platform takes `org_id` **from the credential**, never from a frame. The agent cannot say who it works for, and cannot receive an order about an app that is not of its organization and destination; `destino.Remoto` checks both before writing the frame, and the connection handler checks again when reading results and events.
- Results and events are written to that organization's rows only; the bitácora entry for each order carries the `org_id` and the agent.
- Tests that go with it: the same two-organization wall tests as for the MCP tools, with an agent of organization B asked to apply an app of A (it must find nothing), and an `evento` that names a foreign app (it must be dropped and noted).

## Version skew between panel and agent {#version-skew}

The platform is updated by one person at one time; the agents on computers of the house are not. They must be able to be older (or newer, after a rollback) for a while.

- Two numbers in `hola`: the agent's **version** (for people) and its **protocol range** `[min, max]` (for the machine). The panel supports the **current and the previous** protocol; it speaks the highest both know.
- Changes to the protocol only **add**: new optional fields, new orders. An order the agent does not know is answered with `error: unsupported`, and the platform says so on the app: *this computer's agent is too old for this; update it*. An unknown field is ignored.
- The **spec** (`rendimiento.yaml` as the platform understands it) is sent to the agent already resolved into the release; the agent does not read the repository. A release that uses a feature that the agent's `capabilities` lack is refused *before* it is applied, with a reason.
- The Destinations page shows each agent's version and a mark when it is behind the platform. Self-update is **off** by default (a person installs the agent, so a person updates it); when on, it is a signed release from the same channel as the platform's.
- Outside the supported range the platform refuses the connection with a text that says which version to install.

## What stays on the agent {#what-stays}

| Stays on the agent | Why |
|---|---|
| **Secret values** | `guardar_secreto` sends a value once over the channel, and the agent seals it in its own `Caja`. The platform keeps only the *names* of the keys (what `LlavesSecreto` answers) and never reads a value back. Restarting an app after a change is the `reiniciar` order. |
| **The engine's and the app's credentials** | the registry pull credential, the credentials of the app's Postgres and Redis (made once, on the agent, as `Local` does now). |
| **Images** | the platform never sends image bytes. A release names an image by digest; the agent **pulls it itself**, and verifies the digest. The builds stay in the platform's BuildKit pool. |
| **Volumes and data** | the app's data does not leave the computer; backups are a later order, to a target the organization chose. |

What this means for the registry: an outbound-only computer must be able to *reach* the registry. For the prototype that is the platform's registry over HTTPS with a pull-only credential per agent; a registry that is only on the platform's LAN does not work from outside and needs a public one or a pull-through mirror. That is a decision for the person.

Since the platform never holds the values, a lost agent means re-entering the app's secrets; the panel says which keys are missing.

## Reaching the apps {#reaching-the-apps}

"No open ports" is about the *control* path: no port on the router for rendimiento to reach in. The apps themselves:

- on the house network, the agent's proxy listens on the computer's LAN address and the apps are at `<app>.<computer>.lan`-style names, as `Local` does with `.localhost`;
- from the outside, an outbound tunnel (a later step, with its own design) would carry the traffic the same way.

## The prototype {#the-prototype}

A computer of the house (a machine of the private overlay, not named here) is the destination of the hosted panel. What it takes, in the order to build it:

1. `internal/agente`: the frames of the protocol, as Go types, with a test that old frames still decode.
2. The destinations table (`org_id`, name, kind, credential hash, version, last seen) and the enrolment endpoint; the migration only adds.
3. `destino.Remoto`, and `rendimiento agente` running `destino.Local` behind the connection; tested against a fake agent first.
4. The app's *destination* choice in the panel and in `spec`, with an example in `internal/spec/ejemplos`.
5. The proof: an app of the hosted panel deploys on the computer with **no port opened**, the agent is killed in the middle and reconnects, and the app of another organization stays out of reach.

**It is done when** an app of the hosted rendimiento.ai panel deploys on a computer of the house without opening ports. This page is the first step; steps 1–5 are still to do.

## Open decisions {#open-decisions}

- WebSocket or gRPC for good (this page says WebSocket for the prototype, with the door open).
- Where the registry lives so that an outbound-only computer reaches it.
- Whether a Kubernetes cluster's agent runs in the cluster with its own service account (least privilege, like Argo CD's) or from outside.
