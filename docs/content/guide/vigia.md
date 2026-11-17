# The lookout

Once a week, **the lookout** (*el vigía*) reads what the platform recorded and tells its administrators the few things worth their time. It is one more of the [agents inside the platform](agents.md), and it works like them: through the [AI gateway](ai.md), within a cap, with every call on the AI page. It proposes; a person decides.

Its findings are at the top of the [Log](registro.md) page, under **What the lookout saw**, for the platform's administrators. When a new week's findings arrive, the bell says so.

## What it reads {#what-it-reads}

The week, and the three weeks before it to compare:

- **Steps:** how long each step of each app takes, how often it is the last of its run to finish (so it decides how long runs take), how often it fails, runs more than once, or fails and then passes on the same commit (flaky).
- **Apps:** each app's runs, the failed and the cancelled ones; an app whose runs all passed is good news, and it says so too.
- **The log:** its warnings and errors, grouped by message, with how many times, on how many days, and whether they are new.
- **Slow requests:** the routes of the API that answered in more than a second.
- **Problems** still open.
- **The interface's use** (when it is measured, see [Use of the interface](uso.md)): pages that take long to show their data, and pages nobody opened in two weeks.

All of it is added up first, in the database, into a short list of signals, each with a stable key: the same thing gives the same key next week. The model reads only that list.

## What it never sends {#what-it-never-sends}

The model reads counts, times, step and route names and the log's messages (the line's template, such as *could not reach the registry*). Never a line's attributes, a repository's contents, a run's log, or what people wrote (comments, encargos, the Ruta). A message that looks like a secret is left out entirely, and long tokens (IDs, hashes) are taken out of the others.

## A finding {#a-finding}

Each finding is a title and a line of evidence: how many times, where, since when, and, when it calls for work, what would help.

> **Step interfaz-es takes 11 min and decides how long each run takes**
> 42 runs · always the last to finish · splitting it in two would bring it to about 6 min

It keeps at most five, fewer when fewer matter. Each has three buttons:

- **Turn it into an encargo:** an [encargo](ruta.md) with the finding as its title and the evidence in its body, made by you.
- **Note it in the Ruta:** a to-do in the Ruta (or a note, for a fact), tagged `vigia`, written by you.
- **Not now:** it goes away, and is not proposed again.

A finding that is only a fact, or good news (*Nobody opened Mercado in 14 days · usage fact · no action suggested*), suggests no work: it offers only **Note it in the Ruta** and **Not now**.

Nothing becomes an encargo or enters the Ruta without a person's yes.

## What it remembers {#what-it-remembers}

What people decided is not proposed again: a finding set aside, never; one turned into an encargo or a note, not for 90 days. It remembers by the signal's key, so the signal is not even shown to the model; and it names what was decided to the model, so it does not propose the same thing in other words.

## When it runs, and what it costs {#when-and-cost}

- **Every week**, by itself, while it is on. After a review that failed (no model answered, the cap was spent) it tries again a day later.
- **Review now** on the Log page asks for one at once; one at a time.
- A week with nothing worth telling asks no model at all.
- It costs AI money each time: about a cent with Claude Haiku, nothing with the local model. Its calls are signed *agent: lookout* on the [AI page](ai.md), with the others.
- On the AI page, **The lookout** has its switch (**Review every week**) and its **monthly cap** (50 cents unless you change it). When the month's cap is spent it waits for the next month; a review asked for by hand counts too. The organization's own AI cap still applies.

It writes in the platform's language (`NOTIFY_LANG`), like the other agents.
