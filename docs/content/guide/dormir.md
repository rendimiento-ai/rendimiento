# Sleeping and shutting down (on a computer)

rendimiento [on one computer](../environment/deploy.md#on-one-computer) can go to sleep when you do not need it, and wake up as it was. Asleep, it stops your apps' containers and the CI; the data, the releases and the Ruta stay as they are. **Shut down completely** also stops rendimiento itself, until the computer starts again.

In a cluster none of this shows: a cluster serves other people and stays awake (the server answers *409* to these routes).

## Putting it to sleep {#putting-it-to-sleep}

The platform's administrators see a **Sleep** button with a moon in the top bar. It opens a dialog that says what sleep does, and asks two things:

- **When?**, only when a release is being published. **When the publish ends** (the default) waits for it, and the app wakes on the new release; branch runs stop at once. **Without waiting** cuts the publish as well: the app stays on the release it runs, and the new one is published on waking.
- **Wake up by itself at** an hour (off at first, 08:00 when turned on): the next time the clock reaches that hour after it fell asleep, it wakes by itself.

While it falls asleep, and then while it sleeps, a line under the top bar says since when, how many webhooks wait and when it wakes; an administrator wakes it from there with **Wake up now**.

What sleep does:

- **Runs.** Nothing more is taken from the queue. A run that was going is cut and goes back to the queue from its first step: it is **not marked failed**, its check on the forja says it waits, and it runs again on waking. New pushes are queued and wait too.
- **The apps.** Every container of your apps, their Postgres and Redis and the pull requests' copies is stopped (not removed): volumes and releases stay. rendimiento's own containers (its database, image registry and BuildKit) keep running.
- **Webhooks.** What GitHub, GitLab or Bitbucket send meanwhile is checked as always (its signature) and kept; on waking it is handled in the order it came. On a computer without a tunnel the forjas cannot send webhooks, and rendimiento asks them for new commits every minute instead: asleep it stops asking, and the first look after waking finds what moved.
- **Visitors.** rendimiento's proxy (`http://<app>.localhost:8000`, and whatever reaches it through a tunnel) answers *Back soon* (*Volvemos pronto* to a browser in Spanish) with *503* and `Retry-After`, so search engines and monitors know it is for a while.
- **The lookout.** The hours asleep are kept, and [the lookout](vigia.md) leaves out the log lines written in them: the apps were asleep, not broken.
- **A restart.** The state is kept in the database. If the computer restarts while rendimiento sleeps, the apps' containers start with it (they restart with the computer), and rendimiento, starting asleep, stops them again.

## Waking it {#waking-it}

**Wake up now** (or the hour it was given) starts the same containers, so the same apps at the same releases; handles the webhooks it kept, in order; and lets the queue go, so the runs it cut run again. Then the page shows what it did: the apps started, the publish it waited for, the webhooks handled and the runs repeating.

## Shutting down completely {#shutting-down-completely}

**Shut down completely** sleeps as above (waiting for a publish, or not) and then stops rendimiento: under the installer's service it asks systemd to stop it (`systemctl --user stop rendimiento`; in the Windows app's WSL distribution, its system service), so it is not started again until the computer starts; started by hand, the process ends. When it starts again with the computer it wakes by itself.

## On a laptop's lid {#on-a-laptops-lid}

*Sleep when the lid closes* belongs to the desktop app, which is the one that can see the lid or the system going to sleep; it is not there yet, so the option is not shown. rendimiento keeps the setting (`tapa`, off) for it. Until then a computer that suspends simply pauses everything, and rendimiento goes on where it was when it resumes.

## For agents and scripts {#for-agents-and-scripts}

Only the platform's administrators, signed in, can put it to sleep, wake it or change its options; agent keys (MCP) cannot. Every change is in the [bitácora](../architecture/security.md#the-bitacora).

| Route | Who | What |
| --- | --- | --- |
| `GET /api/dormir` | anyone signed in | where it stands: `estado` (`despierto`, `durmiendose`, `dormido`), since when, webhooks waiting, runs going, the publishes it would wait for, the next wake, and the last wake's summary |
| `POST /api/dormir` `{"ya": false, "apagar": false}` | administrators | sleep (`ya`: without waiting; `apagar`: shut down completely) |
| `POST /api/despertar` | administrators | wake now |
| `PUT /api/dormir/opciones` `{"despertarA": "08:00", "tapa": false}` | administrators | the hour to wake at (`""`: off) and the lid's setting |

The code is in `internal/platform/dormir.go` (the states), `internal/destino/local_dormir.go` (the containers and the proxy's page), `internal/api/dormir.go` and `web/src/components/dormir.tsx`.
