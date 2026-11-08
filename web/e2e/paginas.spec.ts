import { test, expect } from "./vigia";

// Every page in the menu opens and shows its title, without errors. Files
// run in name order: by now nueva-app.spec.ts has made an app of each kind,
// so the pages have something to show.
test("every page in the menu opens", async ({ page, problemas }) => {
  await page.goto("/");
  await expect(page.locator("header.topbar")).toBeVisible();
  const enlaces = await page.locator("header.topbar a.nav-link, header.topbar a.btn").evaluateAll((as) =>
    [...new Set(as.map((a) => a.getAttribute("href") ?? ""))].filter((h) => h.startsWith("/")),
  );
  expect(enlaces.length).toBeGreaterThan(3);
  for (const href of enlaces) {
    await test.step(href, async () => {
      await page.goto(href);
      await expect(page.locator("main h1").first(), `${href} shows a title`).toBeVisible();
    });
  }
  expect(problemas).toEqual([]);
});

// Creating apps (nueva-app.spec.ts) was written to the bitácora, as the
// person who did it, and the chain holds.
test("the bitácora shows what was done, and its chain holds", async ({ page, problemas }, info) => {
  await page.goto("/bitacora");
  await expect(page.locator("main h1").first()).toBeVisible();
  // The action in words, with its route beside it.
  const fila = page.locator("table.bitacora tr", { hasText: "POST /api/apps" }).first();
  await expect(fila).toBeVisible();
  await expect(fila).toContainText(info.project.name === "es" ? "Creó una aplicación" : "Created an app");
  await expect(page.locator("table.bitacora").first()).toContainText("prueba");
  await expect(page.locator(".badge.ok").first()).toBeVisible(); // intact
  expect(problemas).toEqual([]);
});

// The bell opens with what waits for the person and what happened.
test("the bell opens", async ({ page, problemas }, info) => {
  await page.goto("/");
  await page.locator("button.campana-boton").click();
  const panel = page.locator(".campana-panel");
  await expect(panel).toBeVisible();
  await expect(panel).toContainText(info.project.name === "es" ? "Esperan su atención" : "Waiting for you");
  await page.keyboard.press("Escape");
  await expect(panel).toBeHidden();
  expect(problemas).toEqual([]);
});

// On a phone the bar wraps and the bell sits in the middle: its panel
// still opens whole inside the screen.
test("the bell's panel fits a phone's screen", async ({ page, problemas }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await page.locator("button.campana-boton").click();
  const caja = await page.locator(".campana-panel").boundingBox();
  expect(caja, "the panel is shown").not.toBeNull();
  expect(caja!.x).toBeGreaterThanOrEqual(0);
  expect(caja!.x + caja!.width).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});

// The operators see what the señales would send, exactly, and turn them
// off; the notice on the Apps page goes once seen.
test("the señales: what goes, and the switch", async ({ page, problemas }, info) => {
  await page.goto("/members");
  const seccion = page.locator("#senales");
  await expect(seccion.locator("pre")).toContainText('"modo": "local"');
  await expect(seccion.locator("pre")).not.toContainText("prueba"); // no person's name
  const es = info.project.name === "es";
  await seccion.getByRole("button", { name: es ? "Apagar" : "Turn off" }).click();
  await expect(seccion).toContainText(es ? "Apagadas aquí" : "Turned off here");
  await seccion.getByRole("button", { name: es ? "Encender" : "Turn on" }).click();
  await expect(seccion.getByRole("button", { name: es ? "Apagar" : "Turn off" })).toBeVisible();
  expect(problemas).toEqual([]);
});
