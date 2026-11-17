import { test, expect } from "./vigia";

// Moving through three pages inside the interface is noted, as totals, and
// the Use page shows those pages counted. The database is shared with the
// other tests: "at least", never exact numbers.
test("the pages people open are counted on the Use page", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.goto("/ruta");
  await expect(page.locator("main h1").first()).toBeVisible();
  await page.locator('header.topbar a.nav-link[href="/universe"]').click();
  await expect(page.locator("main h1").first()).toHaveText(es ? "Universo" : "Universe");
  await page.locator('header.topbar a.nav-link[href="/"]').click();
  await expect(page.locator("main [data-listo]").first()).toBeAttached();

  // Leaving the page sends what is queued (the batch also goes every 15 s).
  const enviado = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/uso" && r.request().method() === "POST");
  await page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
  expect((await enviado).status()).toBe(204);

  await page.goto("/uso");
  await expect(page.locator("main h1").first()).toHaveText(es ? "Uso de la interfaz" : "Use of the interface");
  await expect(page.getByRole("switch")).toHaveAttribute("aria-checked", "true");
  const abiertas = page.locator("section.vis-top", { hasText: es ? "Páginas más abiertas" : "Most opened pages" });
  for (const nombre of es ? ["Ruta", "Universo", "Aplicaciones"] : ["Ruta", "Universe", "Apps"]) {
    const fila = abiertas.locator("tr", { hasText: nombre }).first();
    await expect(fila).toBeVisible();
    expect(Number((await fila.locator("td.num").innerText()).replace(/\D/g, ""))).toBeGreaterThanOrEqual(1);
  }
  // The move between them, and never an address with a name in it.
  const pasos = page.locator("section.vis-top", { hasText: es ? "Los pasos más comunes" : "Most common moves" });
  await expect(pasos).toContainText(es ? "Ruta › Universo" : "Ruta › Universe");
  await expect(page.locator("main")).not.toContainText("/apps/prueba");
  expect(problemas).toEqual([]);
});
