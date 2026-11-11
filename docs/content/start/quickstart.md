# Quickstart

From a computer that has never seen rendimiento to an app of yours with an address, in under ten minutes. Most of that time is yours, on GitHub's pages; the machine's part takes under a minute.

You need a computer with **Linux** (amd64 or arm64, with systemd and `curl`) and a **GitHub account**. A container engine is not needed: the installer uses containerd or Docker when the computer has one, and brings containerd when it has none. On Windows, use [the app](../environment/deploy.md#on-windows) (a preview); for a Kubernetes cluster, see [Installing with Helm](../environment/deploy.md#installing-with-helm).

## 1. Install {#install}

```sh
curl -fsSL https://rendimiento.ai/instalar | sh
```

It asks for your GitHub login (the first administrator) and, only when it has to install containerd, for `sudo` once. It leaves rendimiento running as a service of your user at **<http://localhost:8080>**, and it ends by printing how to read the **setup token**:

```sh
cat ~/.config/rendimiento/ficha-configuracion
```

## 2. Copy the example {#copy-the-example}

[`rendimiento-ai/hola`](https://github.com/rendimiento-ai/hola) is the smallest app rendimiento can deploy: one page, served by nginx, with its `rendimiento.yaml`. Open **[Use this template](https://github.com/rendimiento-ai/hola/generate)** and create the copy in your account (public or private, either works).

With an app of your own, skip this step: rendimiento reads the repository and works out how to build it.

## 3. Connect GitHub {#connect-github}

Open <http://localhost:8080>. The setup page does this once:

1. Paste the **setup token**.
2. Press **Create GitHub App**. GitHub opens with the name and the permissions already filled in; confirm.
3. GitHub asks which repositories the app can see: choose your copy of `hola` (or all of them; you can change it later).
4. Back in rendimiento, **Sign in with GitHub**.

The GitHub App is yours, in your account: rendimiento's code never passes through anyone else's servers.

## 4. Deploy {#deploy}

Press **New app**, choose your copy of `hola` in the list (**Missing a repo?** takes you to GitHub to give the app access to it), and press **Deploy**. The run page shows the build as it happens; when it ends, the app answers at

**<http://hola.localhost:8000>**

## 5. Change it {#change-it}

Edit the greeting in `index.html` and push to `main`. Within a minute rendimiento sees the push, builds it and replaces the running version without a gap. (GitHub cannot reach `localhost` with a webhook, so on one computer rendimiento asks GitHub every minute.)

## How long it takes {#how-long-it-takes}

Each release of rendimiento, and each change to the installer, is installed on a new amd64 machine (GitHub's, with a fast connection), deploys `hola` and is uninstalled; the run fails when the install and the deploy together pass ten minutes ([the quickstart test](../develop/testing.md)). In November 2026 it measured:

| Step | The computer had Docker | No container engine |
|---|---|---|
| The installer, until rendimiento answers | 25 s | 31 s |
| From **Deploy** until the address answers | 15 s | 13 s |
| Both | 40 s | 44 s |

Steps 2 and 3 are a person on GitHub's pages, and the test does not count them: a stand-in answers for GitHub there. Expect a few minutes the first time.

## Take it off again {#take-it-off-again}

```sh
curl -fsSL https://rendimiento.ai/instalar | sh -s -- --desinstalar
```

It stops and removes rendimiento, your apps' containers and the program, and asks before deleting your data. The GitHub App stays in your account (Settings → Developer settings → GitHub Apps), where you can delete it.

## Where to go next {#where-to-go-next}

- [A tour of the UI](tour.md): every page and what it is for.
- [rendimiento.yaml reference](../guide/spec.md): services, databases with `needs: [postgres]`, tests, sizes.
- [Built for agents](../guide/agents.md): give your coding agent a key, over MCP.
- [On one computer](../environment/deploy.md#on-one-computer): what the installer set up, and what is not there yet.
