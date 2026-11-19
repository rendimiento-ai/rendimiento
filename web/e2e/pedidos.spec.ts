import { test, expect } from "./vigia";

// The Libreta tab of an app: what was asked for it and whether it is done.
// Files run in name order: by now nueva-app.spec.ts has made the apps.

test("the libreta: noting a request writes it in the Ruta, about the app", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const apps = (await (await page.request.get("/api/apps")).json()) as { name: string }[];
  expect(apps.length, "apps made by nueva-app.spec.ts").toBeGreaterThan(0);
  const app = apps[0].name;
  const titulo = `Que el arreglo se dispare desde el botón (${info.project.name})`;

  await page.goto(`/apps/${app}`);
  await page.locator("nav.app-tabs a", { hasText: es ? "Libreta" : "Notebook" }).click();
  await expect(page).toHaveURL(new RegExp(`/apps/${app}/libreta$`));
  await expect(page.locator(".libreta-manda")).toContainText(es ? "La Ruta manda" : "La Ruta is in charge");

  await page.getByRole("button", { name: es ? "Anotar un pedido" : "Note a request" }).click();
  await page.locator(".libreta-form input").fill(titulo);
  await page.getByRole("button", { name: es ? "Anotarlo" : "Note it" }).click();
  const pedido = page.locator("article.libreta-pedido", { hasText: titulo });
  await expect(pedido).toBeVisible();
  await expect(pedido.locator(".libreta-estado")).toHaveText(es ? "Pedido" : "Asked");
  await expect(page.getByRole("button", { name: es ? /^Abiertos/ : /^Open/ })).toHaveAttribute("aria-pressed", "true");

  // It is a to-do in the Ruta, about the app, and its link opens it there.
  const ruta = (await (await page.request.get(`/api/ruta?kind=pendiente&q=${encodeURIComponent(titulo)}`)).json()) as { app?: string }[];
  expect(ruta.map((e) => e.app)).toEqual([app]);
  await pedido.getByRole("link", { name: es ? /Entrada \d+ de la Ruta/ : /Ruta entry \d+/ }).click();
  await expect(page.locator(".pagina-titulo")).toHaveText(titulo);
  await expect(page.locator("main")).toContainText(es ? `De ${app}:` : `About ${app}:`);
  expect(problemas).toEqual([]);
});

test("the libreta fits a phone", async ({ page, problemas }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const apps = (await (await page.request.get("/api/apps")).json()) as { name: string }[];
  await page.goto(`/apps/${apps[0].name}/libreta`);
  await expect(page.locator(".libreta-filtros")).toBeVisible();
  const pedido = page.locator("article.libreta-pedido").first();
  await expect(pedido).toBeVisible();
  // On a phone the way is a bar of four steps, not the row of chips.
  await expect(pedido.locator(".libreta-barra-pasos")).toBeVisible();
  await expect(pedido.locator(".libreta-pasos")).toBeHidden();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});
