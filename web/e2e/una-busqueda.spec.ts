import type { Page } from "@playwright/test";
import { test, expect } from "./vigia";

// Home with one search (decision 195): the mosaic has no box of its own; the top bar's filters it as one types (the
// address keeps ?q=), says how many match, and lists no apps over it; «Clear the search» empties both. Each tile has its
// shortcuts: its last run and its runs. The app is one nueva-app.spec.ts made in this language (this file runs after it).
async function unaApp(page: Page, idioma: string): Promise<string> {
  const r = await page.request.get("/api/apps");
  expect(r.ok()).toBeTruthy();
  const app = ((await r.json()) as { name: string }[]).find((a) => a.name.endsWith(`-${idioma}`));
  expect(app, "an app made by nueva-app.spec.ts").toBeTruthy();
  return app!.name;
}

test("home: the top bar's search filters the apps, and each tile has its shortcuts", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const app = await unaApp(page, info.project.name);
  await page.goto("/");
  await expect(page.locator("main .mosaico .teja").first()).toBeVisible();
  await expect(page.locator("main .mosaico-buscar")).toHaveCount(0);

  const caja = page.locator("header.topbar .buscador input");
  await caja.fill(app);
  await expect(page).toHaveURL(new RegExp(`[?&]q=${app}`));
  // The line that says how many match comes once the mosaic is filtered; then only a few tiles are left.
  await expect(page.locator("main .mosaico-coinciden")).toContainText(app);
  await expect(page.locator("main .mosaico .teja", { hasText: app })).toHaveCount(1);
  await expect.poll(() => page.locator("main .mosaico .teja").count()).toBeLessThan(5);
  await expect(page.locator("#buscador-lista [role=option]", { hasText: app }).filter({ hasNotText: "#" })).toHaveCount(0);

  const teja = page.locator("main .mosaico .teja", { hasText: app });
  const corridas = teja.getByRole("link", { name: es ? "Ejecuciones" : "Runs" });
  await expect(corridas).toHaveAttribute("href", `/apps/${app}/runs`);
  // Its layout height: the tile comes in growing a little (teja-entra), which a bounding box would catch mid-way.
  expect(await corridas.evaluate((el) => (el as HTMLElement).offsetHeight)).toBeGreaterThanOrEqual(40);

  await page.locator("main .mosaico-coinciden").getByRole("button", { name: es ? "Quitar la búsqueda" : "Clear the search" }).click();
  await expect(caja).toHaveValue("");
  await expect(page).not.toHaveURL(/[?&]q=/);
  expect(problemas).toEqual([]);
});

// The search finds every place and setting, not only pages by their name: «keys» (in either language) finds the Keys,
// with its book page beside it; Enter opens it. The book's sections come too, each opening the book in another tab.
test("the search finds settings by any word for them, with their book page, and the book's sections", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.goto("/ruta");
  await expect(page.locator("main h1").first()).toBeVisible();
  const caja = page.locator("header.topbar .buscador input");
  for (const palabra of ["keys", "llaves", "cloudflare"]) {
    await caja.fill(palabra);
    const llaves = page.locator("#buscador-lista [role=group]").first().locator("[role=option]", { hasText: es ? "Llaves" : "Keys" }).first();
    await expect(llaves, palabra).toBeVisible();
    await expect(llaves.locator("a.buscador-libro")).toHaveAttribute("href", new RegExp(`^https://(learn|aprende)\\.rendimiento\\.ai/environment/deploy/#keys-and-settings$`));
    await expect(llaves.locator("a.buscador-libro")).toHaveAttribute("target", "_blank");
  }
  // The book's sections: «capacidad» in Spanish, «capacity» in English, by their heading.
  await caja.fill(es ? "capacidad" : "capacity");
  const libro = page.locator("#buscador-lista [role=group]", { has: page.locator("[role=option]") }).last();
  await expect(page.locator("#buscador-lista .buscador-opcion", { hasText: es ? "Capacidad" : "Capacity" }).first()).toBeVisible();
  await expect(libro.locator("[role=option]").first()).toContainText(/↗/);

  // Enter on the keys opens them here.
  await caja.fill(es ? "llaves" : "keys");
  await expect(page.locator("#buscador-lista [role=option]").first()).toHaveAttribute("aria-selected", "true");
  await caja.press("Enter");
  await expect(page).toHaveURL(/\/environment\/llaves$/);
  expect(problemas).toEqual([]);
});
