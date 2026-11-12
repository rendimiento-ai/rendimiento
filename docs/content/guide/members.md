# Members and roles

People sign in to rendimiento with GitHub, and only **members** get in. Each member has a **role** that says what they may do; every route of the API checks it, so the UI and the API agree.

| Role | May |
|---|---|
| **Guest** | see everything: apps, runs and their logs, releases, the [Universe](universe.md), the [Ruta](ruta.md), encargos |
| **Collaborator** | also: run CI, cancel runs, write the Ruta, [ask agents](agents.md#encargos) for things and review what they hand over, edit [Mercado](mercado.md) pages, turn Renovate on for an app |
| **Administrator** | also: deploy and roll back, read and change secrets, onboard and remove apps, manage add-ons, agent keys, members, and update the platform |

## Adding someone {#adding-someone}

On **Members** (your name in the top bar), people are listed by role on the left; choosing one shows their role and, line by line, **what they can do**. An administrator presses **Add someone**, writes the person's **GitHub username** and picks their role. They can sign in from then on, at the same address. Changing a role or removing someone takes effect on their next click; a removed person is signed out at once. An organization always keeps at least one administrator.

## The first administrators {#the-first-administrators}

A new platform has no members, so the `ALLOWED_USERS` setting names the first administrators. They are added once, while there are no members; after that, people are managed on the Members page and the setting no longer counts.

## Organizations {#organizations}

Everything belongs to an **organization**: its apps (with their runs, releases and requests), its Ruta, encargos, agent keys and members. People of one organization never see another's: lists leave them out, and their pages answer *not found*. A person can belong to several, with a role in each, and switches with the selector at the top; the role that counts is the one in the organization they are working in.

The **platform's organization** (the first one) runs the platform. Its administrators are the **operators**: they alone see the cluster's pages (Environment, Add-ons, Services, Problems, the Mercado), update the platform, and, for now, create organizations (under Members → Organizations). The administrators of every organization create its apps with **New app**, from the GitHub accounts their organization may use (below). An agent key belongs to the organization it was made in, and its tools see only that organization.

### GitHub accounts {#github-accounts}

An app is created from a repository the GitHub App can reach: an **installation** of the App on a GitHub account (a person's or a GitHub organization's). Which installations an organization may use:

- **The platform's organization** uses every installation the App allows (all of them, or those in `GITHUB_ACCOUNTS`). Its operators do too, in whichever organization they are working in.
- **Any other organization** uses only the installations it was given, and never sees the others: not in **New app**, not by typing an installation's number (the answer is the same whether it does not exist or another organization uses it). It gets one in two ways:
    - **The App on your own GitHub account.** An administrator installs the GitHub App on their own GitHub account (the link is in **New app**), reloads the page and presses **Connect**. rendimiento connects it only when the account's login is the one they signed in with, which GitHub's sign-in proves; nothing else is asked of GitHub.
    - **A GitHub organization's account**, given by an operator under Members → Organizations → *GitHub accounts of the organizations*. There is no proof of who administers a GitHub organization without asking GitHub for more permissions, so an operator decides. The same list takes an installation away again.

Taking one away stops new apps from it; the organization's apps keep building from it. A repository must be on the installation's own account. For anyone to install it on their account, the GitHub App must be public (a private App installs only on the account that owns it), and, when `GITHUB_ACCOUNTS` is set, the account must be in it.

### App names {#app-names}

An app's name is also its address in rendimiento and its namespace in the cluster, so it is one across the installation. The platform's organization keeps the names it gives. In any other organization the name carries the organization's address in front: «tienda» in the organization `almacen` is `almacen-tienda`, in rendimiento and in the cluster. Two organizations can each have their «tienda», and neither is told what the other has. The **New app** page shows the full name before you confirm.

Three rules keep the names apart:

- The platform's organization cannot name an app with another organization's address in front (`almacen-…`).
- A new organization's address cannot begin the address of another (`almacen` and `almacen-norte`), nor the name of an app that already exists.
- With the address in front, a name has at most 63 characters, the limit of a namespace. Keep an organization's address short.

Migrating an app that already runs in the cluster (taking over its namespace) is only for the platform's organization, and so is `sharedNamespace` (deploying into a namespace that rendimiento does not own): an app of another organization always has a namespace of its own. Two rules keep addresses apart too: no app takes an address that an app of another organization already claims in its `rendimiento.yaml`, even before that app is live, and no app of another organization takes the platform's own address. They are checked when the app is created and on every push; a push that breaks one is not built, and its run says why.

Today all organizations share the platform's cluster. Organizations in a dedicated virtual cluster, or on their own cluster in any cloud, come later.

## Demo links {#demo-links}

A **demo link** lets someone without a GitHub account here (a reviewer, an investor) look around one organization, read-only. An administrator makes it at the bottom of **Members**, with a note for themselves (who it is for) and how long it lasts, from 7 to 90 days. The address, `https://<platform>/demo/<code>`, is shown only once; rendimiento keeps only the code's hash.

Opening it signs the browser in as that link's own **guest**, with *Read-only demo* in the top bar. The guest sees the organization's apps, runs, logs, encargos and Ruta, and nothing else: it cannot act even if someone raised its role, never sees the cluster's pages, and cannot switch to another organization. **Turn off** ends the link and the sessions it opened at once; an expired link stops working by itself.

The platform's own organization has no demo links, since its guests see the cluster. Make an organization for the demo (Members → Organizations), switch to it, and make the link there.
