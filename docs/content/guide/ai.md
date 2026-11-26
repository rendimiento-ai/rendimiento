# AI: the gateway, providers and caps

Every call the platform makes to a model goes through one **gateway** (`internal/ia`): the [agents inside the platform](agents.md#agents-inside-the-platform), such as the diagnostician that explains failed runs. It decides which model answers for each organization, keeps everyone under their monthly caps, and records every call.

## Who answers {#who-answers}

Each organization chooses, on its **AI** page (administrators):

| Provider | Who pays | Notes |
|---|---|---|
| **The local model** | nobody: it runs on the platform's own hardware (`AI_LOCAL_URL`, an Ollama) | the default, and where every call falls back |
| **Your own key** | the organization, to its provider | Anthropic, or any OpenAI-compatible service (its https address and a model). The key is kept in a Kubernetes Secret, never in the database, and the page shows only its last four characters |
| **The platform's** | the platform's operators | only up to the **allowance** the operators give that organization each month, and never past the platform's total (`AI_PLATFORM_CAP`, changed on the AI page) |

## Caps {#caps}

- The organization sets its **monthly cap** in dollars. With its own key, the gateway stops calling the provider once the month's spending reaches it.
- With the platform's key, the limit is the smaller of the organization's cap and the operators' **allowance**, and all organizations together stay under the platform's **total**: `AI_PLATFORM_CAP` (5 dollars by default) until an operator changes it on the AI page. An allowance that does not fit in the total raises it to the sum of every allowance, so the number an operator gives an organization is the one that limits it; the total can still be set lower by hand, as a ceiling, and the page says so.
- When a cap is reached, a key is missing, or the provider fails, **the local model answers instead**, and the call says why. Nothing stops working; it just gets simpler.
- A single answer is at most 4,096 tokens, so a call can pass a cap only by one answer's worth.
- A [conversation about a Ruta entry](ruta.md#talking-an-entry-over) has its own **cap per conversation** (*Talking entries over* on the AI page, 50 cents by default): past it, it asks a person before going on, and each yes allows one cap more.

Spending is measured from the tokens each provider reports and the model's list price (`ia.DefaultPrices`; a model not listed counts as free, so give it a price before relying on its cap). Caps reset on the first of each month (UTC).

## Escalating new failures {#escalating-new-failures}

The cheapest setup is the local model plus the Ruta's known failures, which the diagnostician reads first. For failures the Ruta knows nothing like, an organization may escalate to a stronger model (its own key, or the platform's within its allowance), at most a number of times a month (10 by default); it is off unless turned on. What the stronger model answers becomes an unconfirmed known failure, so the next time the local model knows it. See [Agents inside the platform](agents.md#agents-inside-the-platform).

## The day's summary {#the-days-summary}

On the home page, folded into one line of the greeting (*Yesterday: 68 runs, 13 of them failed ▸*; touch it to open it, or turn it on unfolded in [Customize home](../start/tour.md#customize-home)), a few lines tell what happened each day since your last visit (yesterday, and up to three days back): new releases and whether they were healthy, rollbacks, failures and fixes, what people and agents did, and the encargos waiting for review. Each line is about one app, linked to it, or about the whole organization.

The newest day is a **scoreboard**: four numbers that count up as the page opens (releases, runs that passed and failed, pull requests rendimiento merged, problems that started), badges for releases that stayed healthy and encargos waiting for review, the runs by hour, and each app with a ring of the share of its runs that passed and its line. The numbers are counted from what was recorded, without AI, and the hours and the totals come from the same count, so they always agree. Beside them, the **day's lotería card** shows the one most notable thing, in this order: *The cloud* (problems or rollbacks), *The stumble* (15% or more of the runs failed), *The flower* (releases, all healthy), *The moon* (releases at night), *The hummingbird* (agents' work waiting for review); a quiet day has none. Touch the card to turn it over and read what it was. With movement off, nothing counts or moves.

- **How it is written.** Each day, rendimiento gathers what it recorded (releases, runs, problems, the bitácora, encargos) and the platform's AI tells it, once per organization, day and language, through the same gateway and [caps](#caps) as everything else. When the platform's share is spent, the local model writes it; with no model at all, it shows only the numbers. A day without anything new is one line, written without a model. Commit messages and logs never go to the model; what does is data, never instructions, and the model has no tools.
- **Who sees what.** A line about an app shows only to people who may see that app ([who enters](members.md#who-enters-an-app)).
- **Redo** writes the latest day again (collaborators and administrators); **Hide for today** hides it on this device until tomorrow. Days begin and end in `PUBLIC_STATS_TZ` (UTC when unset).

## The AI page {#the-ai-page}

The page is in tabs, each one a single question; the address follows the tab (`/ia?tab=agentes`).

- **Summary**, what opens first: who answers now (and, if the chosen one cannot, why the local model answers instead), whether failed runs are explained and how many new failures went to a stronger model this month, what was spent of each cap, what the coding agent does by itself, and who changed it last. Under it, **What it asks of you**: permissions an agent asks for and questions the coding agent waits on (answered in the Ruta), or that the coding agent is off. Then **Try it**: ask a question and see who answered, the tokens, the cost and, if it fell back, why (collaborators).
- **The model:** who answers, who answers new failures (and how many times a month), and the monthly cap. Each choice is a card that says what it costs and whether it can be used now (a missing key, a $0 allowance).
- **The agents:** the plaza, a stall for each of the platform's agents (with its Cotija drawing, what it does and how it goes) and one for each person's MCP key. Pressing one opens its drawer beside the page (a sheet from below on a phone), with what it does by itself and its [permissions](#permissions):
    - **The coding agent** ([how it works](agents.md#the-coding-agent)): off, only the encargos you send it, also the cheap ones rendimiento opens (fixing a failed run, resolving a conflict), or every cheap one; how many encargos it does at once (1 by default, up to 3, never two of the same app); whether it may run commands in [its sandbox](agents.md#the-sandbox) is its `sandbox` permission. Under it, its record (what it took, delivered at the first try, had sent back and gave back, what it spent against the estimates) and the factor the estimator learned from it, with a switch to go back to the fixed table, and [how it is doing](#how-the-coding-agent-is-doing).
    - **The diagnostician:** whether every failed run is explained, and when an encargo opens for one.
    - **The estimator:** up to how much an encargo is estimated to cost for the platform's AI to take it (above it, it is for your own agent; see [Encargos](agents.md#encargos)).
    - **Put up to date:** [the reports on dependency updates](al-dia.md#the-choices).
    - **Conversations:** the cap per conversation. **The lookout** (operators): its weekly review.

    Administrators change them; before saving, a bar lists every change from what is saved, to save or discard. The address opens a drawer: `/ia?tab=agentes&agente=coder`.
- **Spending and calls:** what was spent of each cap, and every call: who asked (a feature or an agent), the model, tokens, cost, and any error.
- **Platform**, for operators: every organization's allowance of the platform's AI, what each spent of it, and the platform's bill against its total; they change the total and each allowance there, each with its own Save. Saved, the total wins over `AI_PLATFORM_CAP` in every process within seconds, without a restart. Where an organization's own monthly cap is below its allowance, its row says that cap stops it first. The local model is connected there too, and the lookout's settings.

### Permissions {#permissions}

Each agent's drawer ends with **What it may do** ([how they work](agents.md#permissions)): a row per permission, with what it covers, its mode (**Off**, **Ask**, **Always**; reading the code and the Ruta is always on, and an action with effect never goes *always*) and its apps (**All**, or **choose** some). Three cards say what each mode means. Under them, the permissions people gave that still hold (once, while an encargo or a key's cargo lasts, always for an app), each with **Take it back**. A change shows in the bar with what is saved and what it becomes, to **Save** or **Discard**; each agent saves on its own. Members see it; only administrators change it. Another page links to one agent's with `/ia#permisos-<id>` (`#permisos-coder`, `#permisos-mcp:12`), which opens its drawer.

The API: `GET /api/permisos` (every subject with its permissions, the grants and the apps, in one call), `PUT /api/permisos/{subject}` with `{"permisos":[{"permiso":"infra_read","modo":"ask","apps":[]}]}` (administrators), `DELETE /api/permisos/dados/{id}`, and `POST /api/permisos/pedidos/{id}` with `{"answer":"once"|"encargo"|"always"|"no"}` to answer a request (a *no* from a collaborator; the rest from an administrator).

### How the coding agent is doing {#how-the-coding-agent-is-doing}

Counted from the encargos' history, without AI, over the encargos [the coding agent](agents.md#the-coding-agent) took in the period (its own, signed with its name: not your own agent's):

- **Finished by it**, at the first try or after it gave the encargo back and took it again; **given back to your agent** (and did not finish); and, of the ones it finished, how many **you accepted the first time** and how many you sent back with a comment (what you answered to its first delivery).
- **Why it gave back**, cause by cause, with the encargos: it reached twice the estimate (its brake), it needed a decision of yours, it lacked a tool it needs, the run did not end in time, the AI failed, the forja answered with an error, it could not merge the default branch, a person stopped it, the platform restarted, or something else. The cause is recorded when it lets the encargo go: its brake, a person's stop, a restart and an error set their own, and the model says one when it gives up. Releases from before the cause was recorded are read from their note by its words, which is approximate; they are marked ≈. Under them, the encargos it gave back before taking them again.
- **Spent against the estimate:** the middle of what it spent over what was estimated, on the encargos it finished, overall and by difficulty (1–3, 4–6, 7–10); and the factor the estimator applies today, the same one as its record above ([what it learns from](agents.md#the-coding-agent)), not counted again.
- **A warning** when its latest releases are three or more in a row for the same cause (a delivery ends the run; a person's stop and a restart do not count): look into it before sending it more like those.

## Setting it up {#setting-it-up}

1. A local model: an Ollama the platform can reach (on the same computer, `http://localhost:11434`; on another one of your network; or in the cluster, such as the `ai` app's `ollama` service). An operator connects it on the AI page, under **The local model**: its address and model, **Try it** to ask the Ollama which models it holds (it says when the model is missing, and offers the ones it has), and **Save**. It answers within seconds, without a restart, on a cluster and on a computer alike. `AI_LOCAL_URL` and `AI_LOCAL_MODEL` give the starting value; what is saved on the page wins.
2. Optionally, the platform's key: an Anthropic key in the `rendimiento-ia` Secret as `AI_PLATFORM_KEY`. Set the same monthly limit in Anthropic's console as a second safety net.
3. Give allowances to the organizations that may use it, on the AI page. The platform's total (`AI_PLATFORM_CAP` to start) goes up with them when they do not fit in it; change it there too, and keep the limit in Anthropic's console in step with it.

See [Settings](../reference/config.md#ai).
