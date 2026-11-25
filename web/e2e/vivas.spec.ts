import type { Page } from "@playwright/test";
import { test, expect } from "./vigia";

// An app's live copies on its Caminos tab, on a phone (390 px): the section
// with its figures and a card per copy, the «live copy» badge of the branch
// that has one, the house on the map, the «⋯» menu staying on the screen,
// and deleting a copy after a yes on the card itself. The test server makes
// no copies, so the app's previews, its roads and its copies are the
// browser's own; the app is one nueva-app.spec.ts made in this language.
const hace = (s: number) => new Date(Date.now() - s * 1000).toISOString();
const dentro = (s: number) => new Date(Date.now() + s * 1000).toISOString();
const corrida = (id: number, sha: string, at: number) => ({ id, sha, status: "succeeded", at: hace(at), end: hace(at - 120) });

const caminos = {
  defaultBranch: "main",
  from: hace(14 * 86400),
  to: hace(0),
  main: [corrida(990201, "aaaaaaa1", 5 * 86400), corrida(990202, "aaaaaaa2", 3 * 86400)],
  branches: [
    { name: "reservas", pr: 31, title: "Reservas con recordatorio", state: "open", fork: { sha: "aaaaaaa1", at: hace(4 * 86400) }, runs: [corrida(990211, "bbbbbbb1", 2 * 86400), corrida(990212, "a7d18d3f", 86400)] },
    { name: "citas", state: "open", fork: { sha: "aaaaaaa2", at: hace(2 * 86400) }, runs: [corrida(990221, "ccccccc1", 3600)] },
  ],
};

const copias = {
  previews: { keep: 3, max: 2, data: "empty", maxKeep: 30, extend: 3 },
  where: "cluster",
  live: [
    {
      name: "app-pr-31", pull: 31, title: "Reservas con recordatorio", author: "p0dxD", url: "https://app-pr-31.example.com", branch: "reservas",
      sha: "a7d18d3f00000000000000000000000000000000", run: 990212, health: "Healthy", tests: { passed: 3, total: 4, failed: ["reserva una cita"] },
      dry: { passed: 1, total: 1 }, memoryMiB: 205, memoryFrom: "usage", madeAt: hace(2 * 86400), pushedAt: hace(86400), deletesOn: dentro(2 * 86400), canExtend: true,
      missingSecrets: ["app-correo"],
    },
    {
      name: "app-seco-990221", short: true, run: 990221, branch: "citas", url: "https://app-seco-990221.example.com", health: "Progressing",
      memoryMiB: 205, memoryFrom: "usage", madeAt: hace(120), pushedAt: hace(120), deletesOn: dentro(7000),
    },
  ],
  gone: [{ pull: 29, run: 990100, madeAt: hace(6 * 86400), goneAt: hace(2 * 86400), why: "merged" }],
  memoryMiB: 410,
  memoryFrom: "usage",
  cluster: { copiesMiB: 820, clusterMiB: 20480, percent: 4, cap: 10, from: "usage" },
  missingSecrets: ["app-correo"],
};

async function unaApp(page: Page, idioma: string): Promise<string> {
  const r = await page.request.get("/api/apps");
  expect(r.ok()).toBeTruthy();
  const app = ((await r.json()) as { name: string }[]).find((a) => a.name.endsWith(`-${idioma}`));
  expect(app, "an app made by nueva-app.spec.ts").toBeTruthy();
  return app!.name;
}

test("live copies on the Caminos tab, on a phone", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const app = await unaApp(page, info.project.name);
  let borrada = "";
  await page.route(`**/api/apps/${app}`, async (r) => {
    const res = await r.fetch();
    const a = await res.json();
    await r.fulfill({ response: res, json: { ...a, role: "admin", spec: { ...a.spec, previews: { keep: 3, max: 2 } } } });
  });
  await page.route(`**/api/apps/${app}/caminos`, (r) => r.fulfill({ json: caminos }));
  await page.route(`**/api/apps/${app}/copias`, (r) => r.fulfill({ json: borrada ? { ...copias, live: copias.live.slice(1), gone: [{ pull: 31, madeAt: hace(86400), goneAt: hace(1), why: "person", by: "p0dxD" }, ...copias.gone] } : copias }));
  await page.route(`**/api/apps/${app}/copias/31`, async (r) => {
    borrada = r.request().method();
    await r.fulfill({ status: 204, body: "" });
  });
  await page.setViewportSize({ width: 390, height: 844 });

  // The glance says how many live, and leads to them.
  await page.goto(`/apps/${app}`);
  const chip = page.locator(".vistazo-copias");
  await expect(chip).toHaveText(es ? "2 copias vivas" : "2 live copies");
  await chip.click();
  await expect(page).toHaveURL(new RegExp(`/apps/${app}/caminos#copias$`));

  // The section: its title, its four figures, a card per copy, those deleted and the Secret they lack.
  const seccion = page.locator("section#copias");
  await expect(seccion.locator("h2")).toHaveText(es ? "Copias vivas · 2 de 2" : "Live copies · 2 of 2");
  await expect(seccion.locator(".copias-cifra")).toHaveCount(4);
  await expect(seccion.locator(".copias-cifra").nth(2)).toContainText(es ? "4 % de 10 %" : "4% of 10%");
  const tarjeta = seccion.locator("#copia-31");
  await expect(tarjeta).toContainText("#31 · Reservas con recordatorio");
  await expect(tarjeta).toContainText(es ? "falló «reserva una cita»" : "failed «reserva una cita»");
  await expect(tarjeta).toContainText(es ? "en seco: pasó" : "en seco: passed");
  await expect(seccion.locator(".copias-card.corta")).toContainText(es ? "Copia corta en seco" : "Short copy en seco");
  await expect(seccion.locator(".copias-idas")).toContainText(es ? "#29 al integrar" : "#29 when merged");
  await expect(seccion.locator(".copias-secretos")).toContainText(es ? "la copia no tiene el secreto app-correo" : "the copy does not have the secret app-correo");

  // The branch with a copy: its badge leads to the card.
  const insignia = page.locator(".caminos-panel .badge.copia-viva").first();
  await expect(insignia).toHaveText(es ? "copia viva" : "live copy");
  await insignia.click();
  await expect(tarjeta).toBeFocused();

  // The map marks it: a house at the end of its road, a button a keyboard reaches; it leads to the card too.
  const casa = page.getByRole("button", { name: es ? "Copia viva de la solicitud #31, rama reservas" : "Live copy of pull request #31, branch reservas" });
  await expect(casa).toBeVisible();
  await expect(casa).toContainText("#31");
  await expect(page.locator(".caminos-casa.corta")).toHaveCount(1);
  await page.evaluate(() => window.scrollTo(0, 0));
  await casa.click();
  await expect(tarjeta).toBeFocused();

  // The «⋯» menu stays on the screen, 8 px from its edges.
  await tarjeta.getByRole("button", { name: es ? "Más para la copia de #31" : "More for the copy of #31" }).click();
  const menu = tarjeta.getByRole("menu");
  await expect(menu.getByRole("menuitem")).toHaveText(es
    ? ["Rehacerla con el último commit", "Dejarla 3 días más", "Borrarla ya"]
    : ["Make it again with the last commit", "Keep it 3 more days", "Delete it now"]);
  const caja = (await menu.boundingBox())!;
  expect(caja.x).toBeGreaterThanOrEqual(7.5);
  expect(caja.x + caja.width).toBeLessThanOrEqual(390 - 7.5);

  // Deleting asks on the card itself, then deletes.
  await menu.getByRole("menuitem", { name: es ? "Borrarla ya" : "Delete it now" }).click();
  const pregunta = tarjeta.getByRole("alertdialog");
  await expect(pregunta).toContainText(es ? "¿Borrar la copia de #31?" : "Delete the copy of #31?");
  expect(borrada).toBe("");
  await pregunta.getByRole("button", { name: es ? "Borrarla" : "Delete it", exact: true }).click();
  await expect.poll(() => borrada).toBe("DELETE");
  await expect(seccion.locator("#copia-31")).toHaveCount(0);
  await expect(seccion.locator(".copias-idas")).toContainText(es ? "#31 por una persona (p0dxD)" : "#31 by a person (p0dxD)");

  // Nothing is wider than the phone.
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});
