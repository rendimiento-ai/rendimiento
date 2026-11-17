# Without a connection

rendimiento's page opens even when there is no connection, or while the platform restarts in a new version: it shows the last it knew, says how old that is, and catches up by itself when the connection is back. It reads; it does not send.

## What you see {#what-you-see}

The browser keeps the page itself and, as you use it, a copy of what rendimiento answered (your apps, runs, releases, the Ruta…). Without a connection the page opens from that, and a notice under the top bar says so:

> **Without a connection.** You see what there was 3m ago. It catches up by itself when it is back.

What is shown from the copy is dimmed. The notice goes once rendimiento answers again, and the pages load what is new at once. A page you never opened on this device has nothing kept: it says *rendimiento does not answer*, and comes back by itself when it does.

Never kept: live logs and event streams, sign-in, `/mcp`, `/healthz`, anything that is not a plain read. The copies are at most 200 answers and 7 days old.

## What is switched off, and why {#what-is-switched-off-and-why}

Without a connection, every action that changes something is switched off and says why: Build now, testing a branch, Retry, Cancel, Merge this branch, Roll back, Update, Dismiss, saving settings, adding to the Ruta, sending a comment. Anything else that would send something fails at once with *Without a connection: nothing was sent*.

They are **never kept to be sent later**. An order to deploy that leaves by itself twenty minutes on, when everything has changed, is dangerous: you give it again when the connection is back, knowing what is there then.

## Drafts {#drafts}

What you are writing is the one thing kept for later: a comment to the team, the team's answer to a comment, a Ruta note. It is kept on this device as you type (*Draft kept on this device*), per box and per person signed in, comes back in its box when you open it again, and is cleared once it is sent. It is never sent by itself.

## A new version {#a-new-version}

When rendimiento was updated, the page says **There is a new version of rendimiento** with a **Reload** button, and loads it when you choose, never in the middle of what you are doing. After **Environment → Update** the page reloads into the new version by itself, as before.

## Installing it on a phone {#installing-it-on-a-phone}

rendimiento can be installed as an app, with its icon: on Android, Chrome's menu → **Install app** (or **Add to Home screen**); on an iPhone, Safari's **Share** → **Add to Home Screen**. It opens in a window of its own, without the browser's bar.

## Shared computers {#shared-computers}

What the browser kept is thrown away when you sign out, when someone else signs in, or when you change organization: one person's data is never shown to the next.

## Turning it off {#turning-it-off}

An operator can turn it all off without a new build: set `SERVICE_WORKER=off` ([settings](../reference/config.md#core)) and restart. Each browser then throws away what it kept and stops keeping anything at its next visit; the page opens only with a connection, as before.

It works only on `https://` addresses and on this computer (`localhost`), which includes rendimiento on one computer and its desktop window. The page's code is in `web/src/trabajador/` (the worker) and `web/src/conexion.ts` (the notice).
