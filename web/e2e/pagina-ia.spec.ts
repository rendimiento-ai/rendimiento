import { test, expect } from "./vigia";

// The AI page in tabs (decision 193), on a phone (390 px): it opens on the Summary; the tabs are 44 px tall and
// reachable; «The agents» shows a stall for each agent, and pressing one opens its drawer (a sheet from below) with its
// own choices and its permissions; Escape closes it and the address follows; nothing is wider than the phone.

test("the AI page: tabs, the plaza of agents and an agent's drawer", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/ia");
  await expect(page.locator("main h1").first()).toHaveText(es ? "IA" : "AI");
  const pestanas = page.getByRole("navigation", { name: es ? "Partes de la página de IA" : "Parts of the AI page" });
  const resumen = pestanas.getByRole("link", { name: es ? "Resumen" : "Summary" });
  await expect(resumen).toHaveAttribute("aria-current", "page");
  for (const l of await pestanas.getByRole("link").all()) expect((await l.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  await expect(page.locator(".ia-summary")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);

  // The model: who answers and the cap, nothing of the agents.
  await pestanas.getByRole("link", { name: es ? "El modelo" : "The model" }).click();
  await expect(page).toHaveURL(/\/ia\?tab=modelo$/);
  await expect(page.locator(".ia-settings h2").first()).toHaveText(es ? "Quién contesta" : "Who answers");
  await expect(page.locator(".ia-settings h2", { hasText: es ? "Lo que hace solo" : "What it does by itself" })).toHaveCount(0);

  // The agents: a stall each; the coding agent's drawer.
  await pestanas.getByRole("link", { name: es ? "Los agentes" : "The agents" }).click();
  await expect(page.locator(".ia-puesto")).not.toHaveCount(0);
  expect(await page.locator(".ia-puesto").count()).toBeGreaterThanOrEqual(4);
  await page.locator("#agente-coder").click();
  await expect(page).toHaveURL(/\/ia\?tab=agentes&agente=coder$/);
  const cajon = page.getByRole("dialog", { name: es ? "El agente de código" : "The coding agent" });
  await expect(cajon).toBeVisible();
  await expect(cajon.locator(".ia-settings h2")).toHaveText(es ? "Lo que hace solo" : "What it does by itself");
  await expect(cajon.locator("section#permisos .permisos-fila:not(.cabeza)").first()).toBeVisible();
  expect((await cajon.getByRole("button", { name: es ? "Cerrar" : "Close" }).boundingBox())!.height).toBeGreaterThanOrEqual(44);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);

  await page.keyboard.press("Escape");
  await expect(cajon).toHaveCount(0);
  await expect(page).toHaveURL(/\/ia\?tab=agentes$/);

  // Put up to date has its stall too, and its drawer holds its card.
  await page.locator("#agente-al-dia").click();
  await expect(page.getByRole("dialog").locator("section#al-dia")).toBeVisible();
  expect(problemas).toEqual([]);
});
