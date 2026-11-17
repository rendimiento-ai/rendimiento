# The log

Each of rendimiento's processes (`api`, `control`, `worker`, `agents`, `traces`, or the single process on [one computer](../start/quickstart.md)) writes what it does as it goes. Those lines used to live only in each process's output, and were lost when it restarted. Now every line at **INFO and above** is also kept in the database, and the **Log** page shows them all together. It is for the platform's administrators: open it from the [Problems](problems.md) page (**The platform's whole log**), or at `/registro`.

## What is kept {#what-is-kept}

- Every line at INFO, WARN or ERROR: when, its level, the process that wrote it, its message, the app and the run it was about (when it says), and its other values.
- Two lines nobody wrote before, from the API: a request that took **longer than a second** (INFO, *slow request*) and one that **failed with a 5xx** (WARN, *request failed*). They say the route's pattern (`GET /api/apps/{app}`), never the address itself, its query, its body or its headers; live streams (a run's page as it runs) are never slow. A 500 also becomes a problem on the Problems page, one per route; the other 5xx (a service upstream that did not answer, a page this computer does not have) say so to the person and stay in the log only.
- Lines are kept for **30 days**, then deleted.
- Writing a line never holds up the work that wrote it. If the database is behind or down, lines are dropped and counted; once it answers again, a line says how many were not kept (*log: some lines were not kept*).

## What is removed {#what-is-removed}

Secrets never reach the log. Before a line is kept:

- a value or a message that looks like a credential is replaced by *[removed: looked like a secret]*. The check is the same one [la Ruta](ruta.md) uses to refuse secrets: GitHub, AWS, Slack and Anthropic-style tokens, rendimiento's own agent keys, signed tokens, private keys, and `password: …` with a random-looking value;
- the value of every attribute whose name says it holds a credential (`token`, `secret`, `password`, `authorization`, `cookie`, `key`, `private`…) is replaced, whatever it is;
- the user and password in an address (`postgres://user:password@…`) are taken out;
- a message is cut at 2,000 bytes, each value at 1,000, and a line keeps at most 40 values (8,000 bytes); what is left out is counted.

## Reading the page {#reading-the-page}

- The newest lines come first; **Show older lines** brings the next ones.
- Filter by **process**, **app**, **level** (everything, warnings and errors, or errors only) and **period** (the last hour, 24 hours, 7 or 30 days, **until** a moment you choose), and search the messages.
- A line with values opens with a click: its attributes, and a link to its run.
- Warnings and errors are also grouped, counted and followed until they are fixed on the [Problems](problems.md) page; the log is where to see what happened around them.
