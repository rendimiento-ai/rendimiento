import type { Page, Route } from "@playwright/test";
import { test, expect } from "./vigia";

// A failed run of a branch whose log says the lockfile is stale: the run
// page offers the app's declared fix, which regenerates it (here the fix
// executor is the browser's own: the test server runs no pods), or a branch
// that declares one. The run and its app are the browser's own too.
const hace = (s: number) => new Date(Date.now() - s * 1000).toISOString();
const linea = "npm error `npm ci` can only install packages when your package.json and package-lock.json or npm-shrinkwrap.json are in sync. Please update your lock file with `npm install` before continuing.";
const ejecucion = (id: number) => ({
  id, appId: 1, sha: "0123456789abcdef", branch: "renovate/react", event: "push", deploy: false, status: "failed", message: "web:build failed",
  createdAt: hace(400), startedAt: hace(400), finishedAt: hace(300),
  steps: [{ id: "web:build", service: "web", kind: "build", dependsOn: [], status: "failed", message: "exited with code 1", startedAt: hace(400), finishedAt: hace(300) }],
});
const app = { name: "candados", repo: "ejemplos/candados", defaultBranch: "main", role: "admin", autoMerge: false, spec: { services: [{ name: "web", path: "web" }] } };
const sinDeclarar = { step: "web:build", folder: "web", stale: { recipe: "npm", file: "package-lock.json", line: linea }, runs: [], available: true };

async function abrir(page: Page, id: number, oferta: (r: Route) => Promise<void> | void) {
  await page.route(`**/api/runs/${id}`, (r) => r.fulfill({ json: ejecucion(id) }));
  await page.route(`**/api/apps/candados`, (r) => r.fulfill({ json: app }));
  await page.route(`**/api/runs/${id}/events`, (r) => r.fulfill({ status: 200, contentType: "text/event-stream", body: ": nada\n\n" }));
  await page.route(`**/api/runs/${id}/steps/*/log`, (r) => r.fulfill({ contentType: "text/plain", body: `#9 4.1 ${linea}\n` }));
  await page.route(`**/api/runs/${id}/diagnosis*`, (r) => r.fulfill({ status: 404, json: { error: "this run has no diagnosis" } }));
  await page.route(`**/api/runs/${id}/fix`, oferta);
  await page.goto(`/apps/candados/runs/${id}`);
}

test("a stale lockfile: the declared fix regenerates it, and its run shows", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const id = 990101;
  let pedido = false;
  const arreglo = {
    id: 7, branch: "renovate/react", sha: "0123456789abcdef", fix: "candado-web", askedKind: "person", askedBy: "prueba",
    image: "node:20-bookworm-slim", imageFrom: ".nvmrc", files: [] as string[], ignored: [] as string[], startedAt: hace(5),
  };
  await abrir(page, id, async (r) => {
    if (r.request().method() === "POST") {
      pedido = true;
      return r.fulfill({ status: 202, json: { fix: "candado-web" } });
    }
    const runs = pedido ? [{ ...arreglo, status: "succeeded", files: ["web/package-lock.json"], ignored: ["web/package.json"], commitSha: "abcdef1234567", finishedAt: hace(1) }] : [];
    return r.fulfill({ json: { ...sinDeclarar, declared: { name: "candado-web", recipe: "npm", path: "web" }, writes: ["package-lock.json"], runs } });
  });
  await page.route(`**/api/runs/${id}/fixes/7/log`, (r) => r.fulfill({ contentType: "text/plain", body: "rendimiento: fix candado-web on renovate/react\nadded 1 package\n" }));

  const tarjeta = page.getByRole("region", { name: es ? "Archivo de bloqueo desactualizado" : "Stale lockfile" });
  await expect(tarjeta).toContainText(es ? "Parece un archivo de bloqueo desactualizado" : "It looks like a stale lockfile");
  await expect(tarjeta).toContainText("are in sync");
  await expect(tarjeta).toContainText(es ? "Esta aplicación declara candado-web (npm, en web/)." : "This app declares candado-web (npm, in web/).");
  await expect(tarjeta.getByRole("button", { name: es ? "Pedírselo al agente" : "Ask the agent" })).toBeVisible();

  await tarjeta.getByRole("button", { name: es ? "Regenerar package-lock.json" : "Regenerate package-lock.json" }).click();
  await expect(tarjeta).toContainText(es
    ? "El arreglo n.º 7 (candado-web) escribió web/package-lock.json y guardó la confirmación abcdef1 en renovate/react: una ejecución nueva lo prueba."
    : "Fix #7 (candado-web) wrote web/package-lock.json and committed abcdef1 to renovate/react: a new run tests it.", { timeout: 15_000 });
  await expect(tarjeta).toContainText(es ? "se dejaron fuera: web/package.json" : "left out: web/package.json");
  await tarjeta.getByRole("button", { name: es ? "Registro" : "Log" }).click();
  await expect(tarjeta).toContainText("added 1 package");

  // On a phone the card's long log line wraps, and nothing scrolls sideways.
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});

test("a stale lockfile without a declared fix: a branch that declares the recipe", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const id = 990102;
  await abrir(page, id, (r) => r.request().method() === "POST"
    ? r.fulfill({ json: { branch: "fix/declare-npm-web", link: "https://github.com/ejemplos/candados/pull/new/fix/declare-npm-web" } })
    : r.fulfill({ json: { ...sinDeclarar, add: { name: "npm-web", recipe: "npm", path: "web" } } }));
  await page.route(`**/api/runs/${id}/fix/add`, (r) => r.fulfill({ json: { branch: "fix/declare-npm-web", link: "https://github.com/ejemplos/candados/pull/new/fix/declare-npm-web" } }));

  const tarjeta = page.getByRole("region", { name: es ? "Archivo de bloqueo desactualizado" : "Stale lockfile" });
  await expect(tarjeta).toContainText(es ? "Esta aplicación no declara un arreglo para package-lock.json en web/." : "This app declares no fix for package-lock.json in web/.");
  await expect(tarjeta.getByRole("button", { name: es ? "Regenerar package-lock.json" : "Regenerate package-lock.json" })).toHaveCount(0);
  await tarjeta.getByRole("button", { name: es ? "Agregar el arreglo npm a rendimiento.yaml" : "Add npm fix to rendimiento.yaml" }).click();
  await expect(tarjeta).toContainText(es ? "La rama fix/declare-npm-web lo declara." : "Branch fix/declare-npm-web declares it.");
  await expect(tarjeta.getByRole("link", { name: es ? "Abrir la solicitud" : "Open the pull request" }))
    .toHaveAttribute("href", "https://github.com/ejemplos/candados/pull/new/fix/declare-npm-web");
  expect(problemas).toEqual([]);
});
