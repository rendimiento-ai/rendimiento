import type { Page } from "@playwright/test";
import { test, expect } from "./vigia";

// An app's glance: its head (the state and the two buttons) and three
// columns below, side by side on a wide screen and stacked on a phone. The
// test server runs nothing, so the running run is the browser's own. By now
// nueva-app.spec.ts has made an app of each kind; any of this language's
// serves.
const id = 990101;
const hace = (s: number) => new Date(Date.now() - s * 1000).toISOString();
const paso = (id: string, status: string, desde?: number, hasta?: number) => ({
  id, service: id.split(":")[0], kind: id.endsWith("test") ? "test" : "build", dependsOn: [], status,
  ...(desde !== undefined ? { startedAt: hace(desde) } : {}),
  ...(hasta !== undefined ? { finishedAt: hace(hasta) } : {}),
});
const ejecucion = {
  id, appId: 1, sha: "0123456789abcdef", branch: "glance-in-columns", event: "push", deploy: false, status: "running", message: "",
  createdAt: hace(300), startedAt: hace(300),
  steps: [paso("api:build", "succeeded", 300, 200), paso("api:test", "succeeded", 300, 120), paso("web:test", "running", 120), paso("web:build", "pending")],
};

async function unaApp(page: Page, idioma: string): Promise<string> {
  const r = await page.request.get("/api/apps");
  expect(r.ok()).toBeTruthy();
  const app = ((await r.json()) as { name: string }[]).find((a) => a.name.endsWith(`-${idioma}`));
  expect(app, "an app made by nueva-app.spec.ts").toBeTruthy();
  return app!.name;
}

test("the glance: its head, and three columns that stack on a phone", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const app = await unaApp(page, info.project.name);
  await page.route(`**/api/apps/${app}/runs`, (r) => r.fulfill({ json: [ejecucion] }));
  await page.route(`**/api/runs/${id}`, (r) => r.fulfill({ json: ejecucion }));
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`/apps/${app}`);
  const vistazo = page.locator("main section.vistazo");

  // The head: the state in a word, Build now and See runs; the page's own head carries no second Build now.
  const cabeza = vistazo.locator(".vistazo-cabeza");
  await expect(cabeza.locator(".vistazo-palabra")).toBeVisible();
  await expect(cabeza.getByRole("button", { name: es ? "Construir ahora" : "Build now" })).toBeVisible();
  await expect(cabeza.getByRole("link", { name: es ? "Ver ejecuciones" : "See runs" })).toHaveAttribute("href", `/apps/${app}/runs`);
  await expect(page.getByRole("button", { name: es ? "Construir ahora" : "Build now" })).toHaveCount(1);

  // Three columns, side by side.
  const columnas = vistazo.locator(".vistazo-col");
  await expect(columnas).toHaveCount(3);
  await expect(columnas.locator("h2")).toHaveText(es ? ["Ahora", "Esta semana", "Lo último"] : ["Now", "This week", "The latest"]);
  const cajas = async () => Promise.all([0, 1, 2].map(async (i) => (await columnas.nth(i).boundingBox())!));
  let [a, b, c] = await cajas();
  expect(Math.abs(a.y - b.y)).toBeLessThan(2);
  expect(Math.abs(b.y - c.y)).toBeLessThan(2);
  expect(a.x + a.width).toBeLessThanOrEqual(b.x);
  expect(b.x + b.width).toBeLessThanOrEqual(c.x);

  // Now: the running run, its branch, its steps as bars, the step it is on, and the way to it.
  const ahora = vistazo.locator(".vistazo-ahora .vistazo-corre");
  await expect(ahora).toHaveCount(1);
  await expect(ahora.getByRole("link", { name: es ? `Ejecución n.º ${id}` : `Run #${id}` })).toHaveAttribute("href", `/apps/${app}/runs/${id}`);
  await expect(ahora).toContainText("glance-in-columns");
  await expect(ahora.locator(".vistazo-barras i")).toHaveCount(4);
  await expect(ahora.locator(".vistazo-barras i.va")).toHaveCount(1);
  await expect(ahora).toContainText("web:test");

  // On a phone the columns stack, and nothing is wider than the screen.
  await page.setViewportSize({ width: 390, height: 844 });
  await expect.poll(async () => { [a, b, c] = await cajas(); return a.y + a.height <= b.y && b.y + b.height <= c.y; }).toBe(true);
  expect(Math.abs(a.x - b.x)).toBeLessThan(2);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});

// Nothing running: one quiet line. A guest may not start runs: no Build now, anywhere on the page; See runs stays.
test("the glance for a guest, with nothing running", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const app = await unaApp(page, info.project.name);
  await page.route(`**/api/apps/${app}`, async (r) => {
    const res = await r.fetch();
    await r.fulfill({ response: res, json: { ...(await res.json()), role: "guest" } });
  });
  await page.route(`**/api/apps/${app}/runs`, (r) => r.fulfill({ json: [{ ...ejecucion, status: "succeeded", steps: undefined }] }));
  await page.goto(`/apps/${app}`);
  const vistazo = page.locator("main section.vistazo");
  await expect(vistazo.locator(".vistazo-ahora .vistazo-tranquilo").first()).toHaveText(es ? "Ahora no corre nada para ella." : "Nothing is running for it now.");
  await expect(vistazo.getByRole("link", { name: es ? "Ver ejecuciones" : "See runs" })).toBeVisible();
  await expect(page.getByRole("button", { name: es ? "Construir ahora" : "Build now" })).toHaveCount(0);
  // Not on the other tabs either.
  await page.goto(`/apps/${app}/overview`);
  await expect(page.locator("main h1").first()).toHaveText(app);
  await expect(page.getByRole("button", { name: es ? "Construir ahora" : "Build now" })).toHaveCount(0);
  expect(problemas).toEqual([]);
});
