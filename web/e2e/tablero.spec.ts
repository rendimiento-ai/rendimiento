import { test, expect } from "./vigia";

// The top bar on a wide screen: one row with the brand, the search, the
// bell, New app and the account's menu; every page in the row under it.
// Files run in name order: by now nueva-app.spec.ts has made the apps.

test("the account's menu opens, has Sign out, and Escape closes it", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.goto("/");
  const boton = page.locator("header.topbar button.cuenta-boton");
  const menu = page.locator("#menu-cuenta");
  await expect(menu).toHaveCount(0);
  await boton.click();
  await expect(boton).toHaveAttribute("aria-expanded", "true");
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("button", { name: es ? "Cerrar sesión" : "Sign out" })).toBeVisible();
  await expect(menu).toContainText(es ? "Tema" : "Theme");
  await expect(menu).toContainText(es ? "Idioma" : "Language");
  await expect(menu).toContainText(es ? "Organización" : "Organization");
  await expect(menu.locator('a[href="/escribir"]')).toBeVisible();
  // The focus goes into it; the arrows move between its rows.
  const primero = await page.evaluate(() => document.activeElement?.closest("#menu-cuenta") !== null);
  expect(primero, "the focus is in the menu").toBe(true);
  await page.keyboard.press("End");
  await expect(menu.getByRole("button", { name: es ? "Cerrar sesión" : "Sign out" })).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(menu.locator("a.menu-cuenta-quien")).toBeFocused();
  // Escape closes it, and the focus is back on its button.
  await page.keyboard.press("Escape");
  await expect(menu).toHaveCount(0);
  await expect(boton).toBeFocused();
  // A click outside closes it too.
  await boton.click();
  await expect(menu).toBeVisible();
  await page.locator("main h1").first().click();
  await expect(menu).toHaveCount(0);
  expect(problemas).toEqual([]);
});

test("at 1280 px the top bar's first row does not wrap", async ({ page, problemas }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/");
  const fila = page.locator("header.topbar .topbar-fila");
  await expect(fila.locator(".buscador")).toBeVisible();
  const centro = async (sel: string) => { const b = (await fila.locator(sel).first().boundingBox())!; return b.y + b.height / 2; };
  const marca = await centro("a.brand");
  for (const sel of [".buscador", "button.campana-boton", "button.cuenta-boton"]) {
    expect(Math.abs((await centro(sel)) - marca), `${sel} shares the brand's row`).toBeLessThan(8);
  }
  expect((await fila.boundingBox())!.height, "one row").toBeLessThan(64);
  // Every page in sight, in the row under it, in one line.
  const enlaces = page.locator("header.topbar .topbar-secciones a.nav-link");
  expect(await enlaces.count()).toBeGreaterThan(3);
  const tops = await enlaces.evaluateAll((as) => as.map((a) => Math.round(a.getBoundingClientRect().top)));
  expect(new Set(tops).size, "the pages share one line").toBe(1);
  // Sign out lives in the account's menu, not on a row of its own.
  await expect(page.getByRole("button", { name: /^(Sign out|Cerrar sesión)$/ })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(1280);
  expect(problemas).toEqual([]);
});

test("the search finds an app, a page and a run; / opens it", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const apps = (await (await page.request.get("/api/apps")).json()) as { name: string }[];
  expect(apps.length, "apps made by nueva-app.spec.ts").toBeGreaterThan(0);
  const app = apps[0].name;
  // The runs the search lists are read each time it opens: the page's own, set before it ever opens
  // (set later, an open that read the real ones first could keep them).
  await page.route("**/api/corriendo", (r) => r.fulfill({ json: { ahora: [], recientes: [
    { id: 880001, appId: 1, app, sha: "0123456789abcdef", branch: "rama-buscada", event: "push", deploy: false, status: "failed", message: "", createdAt: new Date().toISOString() },
  ] } }));
  await page.goto("/ruta");
  await expect(page.locator("main h1").first()).toBeVisible();
  const caja = page.locator("header.topbar .buscador input");

  // "/" from the page, not while writing.
  await page.locator("main h1").first().click();
  await page.keyboard.press("/");
  await expect(caja).toBeFocused();

  // An app by its name: Enter opens it.
  await caja.fill(app);
  const opcion = page.locator("#buscador-lista [role=option]", { hasText: app }).first();
  await expect(opcion).toBeVisible();
  await expect(page.locator("#buscador-lista [role=option]").first()).toHaveAttribute("aria-selected", "true");
  await caja.press("Enter");
  await expect(page).toHaveURL(new RegExp(`/apps/${app}$`));
  await expect(page.locator("main h1").first()).toHaveText(app);
  await expect(caja).toHaveValue("");

  // A page by its name in the reader's language, without its accent; the arrows move.
  await caja.fill("bitacora");
  const pagina = page.locator("#buscador-lista [role=option]", { hasText: "Bitácora" });
  await expect(pagina).toBeVisible();
  await caja.press("ArrowDown");
  await caja.press("ArrowUp");
  await caja.press("Enter");
  await expect(page).toHaveURL(/\/bitacora$/);

  // A run by its number, among the last ones (the test server runs nothing: they are the page's own, above).
  await page.keyboard.press("Escape");
  await page.locator("main h1").first().click();
  await page.keyboard.press("/");
  await caja.fill("rama-busc");
  await expect(page.locator("#buscador-lista [role=option]", { hasText: "880001" })).toBeVisible();
  await caja.fill("88000");
  await expect(page.locator("#buscador-lista [role=option]", { hasText: "rama-buscada" })).toBeVisible();

  // Nothing found says so; Escape empties the box, then closes it.
  await caja.fill("zzz-no-existe");
  await expect(page.locator(".buscador-nada")).toContainText(es ? "Nada coincide" : "Nothing matches");
  await caja.press("Escape");
  await expect(caja).toHaveValue("");
  await expect(page.locator(".buscador-panel")).toHaveCount(0);
  expect(problemas).toEqual([]);
});

test("on a phone the search opens over the top bar", async ({ page, problemas }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const apps = (await (await page.request.get("/api/apps")).json()) as { name: string }[];
  await page.goto("/ruta");
  await page.locator("header.topbar button.buscador-abrir").click();
  const caja = page.locator(".buscador.flotante input");
  await expect(caja).toBeFocused();
  await caja.fill(apps[0].name);
  await expect(page.locator("#buscador-lista [role=option]").first()).toBeInViewport();
  await caja.press("Enter");
  await expect(page).toHaveURL(new RegExp(`/apps/${apps[0].name}$`));
  await expect(page.locator(".buscador.flotante")).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});
