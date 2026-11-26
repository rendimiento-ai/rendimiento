# Use of the interface

rendimiento notes how its own pages are used, so that clicks can be taken away where they cost the most: which pages people open, from which, how long they take to show their data, and which main buttons are used. It is kept in rendimiento's own database, as totals per day, and never says who. The platform's administrators see it on the **Use** page (**Members → Use of the interface**, or `/uso`), and turn it off there.

## What is noted {#what-is-noted}

On every move inside the interface, the browser queues one line and sends the queue every 15 seconds, and when the tab is hidden or closed:

- **The page opened, by its kind:** its route pattern, like `/apps/:name/runs/:id` ("a run"), never its address. No app's name, no run's number, no search.
- **The page it came from**, the same way (nothing on the first page of a visit).
- **How long the page took to show its data:** from the moment the address changes until the page puts its data on screen. Only the pages that say when their data is in are measured: Apps, an app, a run, the Universe, a request, Environment and the Use page. The others count their visits only, rather than a wrong time. A time over 60 seconds, or in a hidden tab, is not noted.
- **The main buttons used**, by a short fixed name: *Build now*, *Retry a run*, *Cancel a run*, *Merge this branch*, *Roll back*, *Dismiss a problem*, *Ask the agent why it failed*, *Open a step*, *Switch day and night*, *Switch the language*, and which section of an app's page was opened (*Runs*, *Roads*, *Visits*…, never which app).

The server accepts only the interface's own route patterns and that closed list of actions, at most 100 lines at once, and refuses anything else. It keeps **totals per day** (UTC): how many times each page was opened, each move from one page to another and each action, and for the times a count, a sum and five buckets (under 0.5 s, 1 s, 2 s, 4 s, and 4 s or more). Days older than 90 are deleted.

## What is never noted {#what-is-never-noted}

- **Who:** no account, no session, no address of the computer. A batch is not written in the [bitácora](../architecture/security.md#the-bitacora) either, which would tie it to who sent it.
- Anything people type, and what the pages show.
- Anything from a read-only demo link.
- Nothing leaves the installation: the use is not part of the [señales de vida](senales.md).

## Turning it off {#turning-it-off}

It is on by default. An administrator of the platform turns it off with the switch at the top of the **Use** page; the interface learns it when it opens and then sends nothing. What was already counted stays until its 90 days pass.

## Reading the Use page {#reading-the-use-page}

Choose the last 7, 14 or 30 days.

- **Most opened pages:** where people spend their visits.
- **Slowest to show their data:** the average time of each measured page; hold the pointer over a row for its share of views that took 2 seconds or more. The table below has the same numbers.
- **Most common moves:** from one page to the next. A move that is made many times between two pages is a candidate for a shortcut.
- **Actions used most:** which buttons, on which page.
- **The longest common path:** an estimate. From the most common first page it follows, at each page, the most common next one, while that move is still common. The same people need not have walked all of it; it shows how many clicks a frequent task costs.

The numbers start appearing as people use the interface.
