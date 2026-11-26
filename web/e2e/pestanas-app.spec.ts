import type { Page } from "@playwright/test";
import { test, expect } from "./vigia";

// An app's page in five tabs (decision 194), on a phone (390 px): Today, Deliveries, Live, Notebook and Settings, each
// at least 44 px tall and in one row; Deliveries opens on Runs, shows its sections, and opens again on the one last
// seen; the old addresses (/releases, /overview) still open their section under the right tab; nothing is wider than
// the phone. The app is one nueva-app.spec.ts made in this language (this file runs after it).
async function unaApp(page: Page, idioma: string): Promise<string> {
  const r = await page.request.get("/api/apps");
  expect(r.ok()).toBeTruthy();
  const app = ((await r.json()) as { name: string }[]).find((a) => a.name.endsWith(`-${idioma}`));
  expect(app, "an app made by nueva-app.spec.ts").toBeTruthy();
  return app!.name;
}

test("an app's page in five tabs, with their sections", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const app = await unaApp(page, info.project.name);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/apps/${app}`);
  const pestanas = page.getByRole("navigation", { name: es ? "Partes de la aplicación" : "Parts of the app" });
  await expect(pestanas.getByRole("link")).toHaveCount(5);
  for (const l of await pestanas.getByRole("link").all()) expect((await l.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  await expect(pestanas.getByRole("link", { name: es ? "Hoy" : "Today" })).toHaveAttribute("aria-current", "page");
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);

  // Deliveries: Runs first, then its sections.
  await pestanas.getByRole("link", { name: es ? "Entregas" : "Deliveries" }).click();
  await expect(page).toHaveURL(new RegExp(`/apps/${app}/runs$`));
  const secciones = page.getByRole("navigation", { name: es ? "Entregas" : "Deliveries" });
  await expect(secciones.getByRole("link")).toHaveCount(3);
  await secciones.getByRole("link", { name: es ? "Versiones" : "Releases" }).click();
  await expect(page).toHaveURL(new RegExp(`/apps/${app}/releases$`));

  // Live, then Deliveries again: back on the section last seen.
  await pestanas.getByRole("link", { name: es ? "En vivo" : "Running live" }).click();
  await expect(page).toHaveURL(new RegExp(`/apps/${app}/overview$`));
  await pestanas.getByRole("link", { name: es ? "Entregas" : "Deliveries" }).click();
  await expect(page).toHaveURL(new RegExp(`/apps/${app}/releases$`));

  // An old address opens its section under its tab.
  await page.goto(`/apps/${app}/visits`);
  await expect(pestanas.getByRole("link", { name: es ? "En vivo" : "Running live" })).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("navigation", { name: es ? "En vivo" : "Running live" }).getByRole("link", { name: es ? "Visitas" : "Visits" })).toHaveAttribute("aria-current", "page");
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});
