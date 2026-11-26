import { test, expect } from "./vigia";

// The AI page with the coding agent's record (Ruta 77): what it took and what
// became of it, the factor the estimator learned, each encargo's bar of
// estimate against spent, the new mode, and the switch back to the fixed
// table. The page is the browser's own: the test server's coding agent has
// done nothing.
const hace = (s: number) => new Date(Date.now() - s * 1000).toISOString();
const hecho = (id: number, title: string, difficulty: number, estimate: number, spent: number) => ({
  id, title, status: "reviewed", difficulty, estimateMicros: estimate, spentMicros: spent, taken: 1, delivered: 1, returned: 0, released: 0, at: hace(id * 60),
});
const vista = {
  settings: {
    orgId: 1, provider: "platform", capMicros: 5_000_000, allowanceMicros: 5_000_000, autoDiagnose: true, escalateTo: "", escalateLimit: 10,
    encargoAutoMicros: 500_000, codeAgent: "system", estimateLearn: true, autoEncargo: "branches", updatedAt: hace(3600),
  },
  spentOwnMicros: 0, spentPlatformMicros: 1_430_000, platformAvailable: true, platformModel: "claude-sonnet-5-5", escalations: 0, usage: [],
  coder: {
    taken: 5, firstTry: 5, returned: 0, gaveBack: 0, spentMicros: 1_430_000, estimateMicros: 820_000, learnFrom: 5,
    learned: { byDifficulty: { "4": { x: 2.2, n: 2 }, "9": { x: 1.4, n: 3 } }, all: { x: 1.9, n: 5 } },
    recent: [
      hecho(42, "Choque de problemas-lista", 4, 50_000, 90_000),
      hecho(24, "Arreglar la ejecución 1162", 4, 50_000, 120_000),
      hecho(22, "YC 13: pruebas con IA", 9, 240_000, 550_000),
    ],
  },
};

test("the coding agent's record on the AI page", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.route("**/api/ia", (r) => r.request().method() === "GET" ? r.fulfill({ json: vista }) : r.fallback());
  await page.goto("/ia?tab=agentes&agente=coder"); // the coding agent's drawer, on the Agents tab

  await expect(page.getByRole("radio", { name: es ? /Lo que abre rendimiento, si es barato/ : /What rendimiento opens, if cheap/ })).toBeChecked();
  const boleta = page.locator(".boleta");
  await expect(boleta).toContainText(es ? "Su boleta" : "Its record");
  await expect(boleta).toContainText(es ? "entregados a la primera" : "delivered at the first try");
  await expect(boleta).toContainText(es
    ? "Lo que de verdad gasta es 1.9× la tabla de precios, según 5 encargos que terminó."
    : "What it really spends is 1.9× the price table, from 5 encargos it finished.");
  // Each encargo's bar: what it spent over what was estimated, past the brake in red.
  await expect(boleta.getByRole("img", { name: es ? "Gastó $0.12 de $0.05 estimados (2.4×)" : "Spent $0.12 of $0.05 estimated (2.4×)" })).toBeVisible();
  await expect(boleta.locator(".eg-gastado.pasado")).toHaveCount(2);

  // Back to the fixed table: a change to save, like any other.
  await boleta.getByRole("switch", { name: es ? "Corregir los estimados según lo que gasta" : "Correct the estimates by what it spends" }).click();
  await expect(page.locator(".ia-savebar")).toContainText(es ? "Estimados" : "Estimates");
  await expect(page.locator(".ia-savebar")).toContainText(es ? "la tabla fija" : "the fixed table");

  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});
