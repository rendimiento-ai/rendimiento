import { test, expect } from "./vigia";

// «How the coding agent is doing» on the AI page (Ruta 178): over a period, what it finished and gave back,
// why, what the person answered, spent against the estimate and the estimator's factor, and the warning of
// three releases in a row for one cause. The AI page and the counts are the test's own; the period is asked
// of the API (?days=).
const hace = (s: number) => new Date(Date.now() - s * 1000).toISOString();
const vista = {
  settings: {
    orgId: 1, provider: "platform", capMicros: 5_000_000, allowanceMicros: 5_000_000, autoDiagnose: true, escalateTo: "", escalateLimit: 10,
    encargoAutoMicros: 500_000, codeAgent: "asked", estimateLearn: true, autoEncargo: "off", updatedAt: hace(3600),
  },
  spentOwnMicros: 0, spentPlatformMicros: 900_000, platformAvailable: true, platformModel: "claude-sonnet-5-5", escalations: 0, usage: [],
  coder: {
    taken: 8, firstTry: 2, returned: 1, gaveBack: 4, spentMicros: 900_000, estimateMicros: 700_000, learnFrom: 5,
    learned: { byDifficulty: {}, all: { x: 1.4, n: 5 } }, recent: [],
  },
};
const cuentas = (days: number) => ({
  days, since: days ? hace(days * 86400) : undefined,
  encargos: 8, finished: 3, firstTry: 2, afterRetry: 1, gaveBack: 4, working: 1, accepted: 1, returned: 1, toReview: 1,
  causes: [
    { cause: "conflict", n: 3, encargos: [27, 28, 41], approx: 1 },
    { cause: "budget", n: 1, encargos: [25], approx: 0 },
  ],
  retried: [{ cause: "ai_error", n: 1, encargos: [9], approx: 1 }],
  ratios: [{ from: 1, to: 3, x: 1, n: 2 }, { from: 4, to: 6, x: 2.5, n: 1 }],
  all: { from: 0, to: 0, x: 1.2, n: 3 },
  racha: { cause: "conflict", n: 3, encargos: [27, 28, 41] },
  learned: { byDifficulty: {}, all: { x: 1.4, n: 5 } }, learnFrom: 5, learnOn: true, rachaFrom: 3,
});

test("how the coding agent is doing, on the AI page", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const pedidos: string[] = [];
  await page.route("**/api/ia", (r) => r.request().method() === "GET" ? r.fulfill({ json: vista }) : r.fallback());
  await page.route(/\/api\/ia\/cuentas\?/, (r) => {
    const days = Number(new URL(r.request().url()).searchParams.get("days"));
    pedidos.push(String(days));
    return r.fulfill({ json: cuentas(days) });
  });
  await page.goto("/ia");

  const card = page.locator(".cuentas-agente");
  await expect(card).toContainText(es ? "Cómo le va al agente" : "How the coding agent is doing");
  await expect(card).toContainText(es ? "2 a la primera, 1 al volver a intentar" : "2 at the first try, 1 after another try");
  await expect(card).toContainText(es ? "Por qué soltó los 4" : "Why it gave back the 4");
  await expect(card).toContainText(es ? "No pudo integrar la rama principal (conflictos)" : "It could not merge the default branch (conflicts)");
  await expect(card.locator(".cuentas-aviso")).toContainText(es ? "Soltó 3 seguidos por la misma causa:" : "It gave back 3 in a row for the same cause:");
  await expect(card).toContainText(es ? "El estimador aplica hoy ×1.4" : "The estimator applies ×1.4 today");
  await expect(card.getByRole("link", { name: "41" }).first()).toHaveAttribute("href", "/ruta?tab=encargos#encargo-41");
  expect(pedidos[0]).toBe("30");

  // All of it: asked again, without a date.
  await card.getByRole("button", { name: es ? "Todo" : "All of it" }).click();
  await expect(card).toContainText(es ? "Desde el principio · 8 encargos" : "All of it · 8 encargos");
  expect(pedidos).toContain("0");

  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});
