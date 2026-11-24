import { test, expect } from "./vigia";

// La Ruta nueva on a phone: it opens on «For you», its four places are a bar
// at the bottom, an encargo asked without its app says so (its box cannot be
// ticked) and gets it on its card, and dropping it asks on the page. Files run
// in name order: by now nueva-app.spec.ts has made the apps.

test("the Ruta on a phone: For you, an encargo without its app, the agent and the journal", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.setViewportSize({ width: 390, height: 844 });
  const apps = (await (await page.request.get("/api/apps")).json()) as { name: string }[];
  expect(apps.length, "apps made by nueva-app.spec.ts").toBeGreaterThan(0);
  const titulo = `Revisar por qué tarda la portada (${info.project.name})`;

  await page.goto("/ruta");
  await expect(page.locator("main h1").first()).toBeVisible();
  const lugares = page.getByRole("navigation", { name: es ? "Lugares de la Ruta" : "Places of the Ruta" });
  await expect(lugares.getByRole("link")).toHaveCount(4);
  await expect(lugares.getByRole("link", { name: es ? "Para usted" : "For you" })).toHaveAttribute("aria-current", "page");
  // The bar sits above the menu sheet, not under it.
  const barra = (await lugares.boundingBox())!;
  const hoja = (await page.locator(".menu-hoja").boundingBox())!;
  expect(barra.y + barra.height).toBeLessThanOrEqual(hoja.y + 1);
  await expect(page.locator("main .esq")).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);

  await test.step("an encargo asked without its app", async () => {
    await lugares.getByRole("link", { name: "Encargos" }).click();
    await expect(page).toHaveURL(/\?tab=encargos$/);
    await page.getByRole("button", { name: es ? "+ Pedir algo" : "+ Ask for something" }).click();
    await page.locator("form.nuevo-encargo input").fill(titulo);
    await page.locator("form.nuevo-encargo select").selectOption("-");
    await page.getByRole("button", { name: es ? "Pedírselo a los agentes" : "Ask agents" }).click();
    const card = page.locator("article.encargo-tarjeta", { hasText: titulo });
    await expect(card).toBeVisible();
    await expect(card.locator(".et-insignia.sin-app")).toBeVisible();
    await expect(card.locator(".et-escoger input")).toBeDisabled();
    // Its app, chosen right on the card.
    await card.locator(".et-falta select").selectOption(apps[0].name);
    await expect(card.locator(".et-insignia.app")).toHaveText(apps[0].name);
    await expect(card.locator(".et-falta")).toHaveCount(0);
    // Every action on it is at least 44 px tall.
    for (const b of await card.locator(".et-acciones .et-boton").all()) expect((await b.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);

    // Dropping it asks on the page, not with the browser's confirm().
    await card.getByRole("button", { name: es ? /^Más del encargo/ : /^More for encargo/ }).click();
    // The «⋯» list stays on the screen, even when the button sits at the card's left edge.
    const lista = (await card.getByRole("menu").boundingBox())!;
    expect(lista.x).toBeGreaterThanOrEqual(0);
    expect(lista.x + lista.width).toBeLessThanOrEqual(390);
    await card.getByRole("menuitem", { name: es ? "Soltarlo" : "Drop it" }).click();
    const pregunta = card.getByRole("alertdialog");
    await expect(pregunta).toBeVisible();
    await pregunta.locator("textarea").fill("Era una prueba");
    await pregunta.getByRole("button", { name: es ? "Soltarlo" : "Drop it" }).click();
    await expect(page.locator("article.encargo-tarjeta", { hasText: titulo })).toHaveCount(0);
  });

  await test.step("the agent's queue and the journal", async () => {
    await lugares.getByRole("link", { name: es ? "El agente" : "The agent" }).click();
    await expect(page.locator("main h1").first()).toHaveText(es ? "El agente de código" : "The coding agent");
    await expect(page.getByRole("heading", { name: es ? "Haciendo ahora" : "Doing now" })).toBeVisible();
    await lugares.getByRole("link", { name: es ? "Cuaderno" : "Journal" }).click();
    await expect(page.locator("main h1").first()).toContainText(es ? "El cuaderno" : "The journal");
    await page.locator(".cu-tarjeta", { hasText: es ? "Pendientes" : "To-dos" }).click();
    await expect(page).toHaveURL(/\?tab=pendientes$/);
    await expect(lugares.getByRole("link", { name: es ? "Cuaderno" : "Journal" })).toHaveAttribute("aria-current", "page");
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  });

  // The old addresses still open their place.
  await page.goto("/ruta?tab=cargos");
  await expect(page.getByRole("button", { name: "Cargos" }).first()).toHaveAttribute("aria-current", "page");
  expect(problemas).toEqual([]);
});
