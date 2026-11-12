import { fileURLToPath } from "node:url";
import { test, expect, vigilar } from "./vigia";

// Two people of two organizations on one rendimiento: "prueba", of the
// principal one, who by now has an app of each kind (nueva-app.spec.ts),
// and "vecina", who administers her own (hack/interfaz makes it and signs
// her in). Neither finds the other's: not in the lists, not by typing an
// address, not on any page of the menu.
const estadoVecina = fileURLToPath(new URL(".estado-vecina.json", import.meta.url));

test("two organizations: neither sees the other's", async ({ page, browser, baseURL, problemas }, info) => {
  const idioma = info.project.name;
  const nota = (de: string) => ({ kind: "nota", title: `nota de ${de} (${idioma})`, body: `solo la lee ${de}`, tags: [], done: false });

  // What prueba has: her apps and a note.
  const apps = await page.request.get("/api/apps");
  expect(apps.ok()).toBeTruthy();
  const suyas = ((await apps.json()) as { name: string; repo: string }[]).filter((a) => a.name.endsWith(`-${idioma}`));
  expect(suyas.length, "prueba's apps, made by nueva-app.spec.ts").toBeGreaterThan(3);
  expect((await page.request.post("/api/ruta", { data: nota("prueba") })).ok()).toBeTruthy();
  const ajeno = [...suyas.map((a) => a.name), nota("prueba").title, "solo la lee prueba"];

  const contexto = await browser.newContext({ storageState: estadoVecina, baseURL, locale: info.project.use.locale });
  const suya = await contexto.newPage();
  const problemasSuyos = vigilar(suya);
  try {
    // Vecina's lists are her own: no apps, and her note.
    const lista = await suya.request.get("/api/apps");
    expect(lista.ok()).toBeTruthy();
    expect(await lista.json(), "vecina has no apps").toEqual([]);
    expect((await suya.request.post("/api/ruta", { data: nota("vecina") })).ok()).toBeTruthy();

    // An address of prueba's app, typed: it does not exist for her.
    for (const app of suyas.slice(0, 3)) {
      await test.step(`/apps/${app.name}`, async () => {
        for (const ruta of ["", "/runs", "/releases", "/caminos"]) {
          expect((await suya.request.get(`/api/apps/${app.name}${ruta}`)).status(), `GET /api/apps/${app.name}${ruta}`).toBe(404);
        }
        await suya.goto(`/apps/${app.name}`);
        await expect(suya.locator("main .alert").first(), "the page says it does not exist").toBeVisible();
        await expect(suya.locator("main")).not.toContainText(app.repo);
      });
    }

    // Every page of her menu, and nothing of prueba's on any.
    await suya.goto("/");
    await expect(suya.locator("header.topbar")).toBeVisible();
    const enlaces = await suya.locator("header.topbar a.nav-link, header.topbar a.btn").evaluateAll((as) =>
      [...new Set(as.map((a) => a.getAttribute("href") ?? ""))].filter((h) => h.startsWith("/")),
    );
    expect(enlaces.length).toBeGreaterThan(3);
    for (const href of ["/", ...enlaces, "/bitacora", "/ruta"]) {
      await test.step(`vecina: ${href}`, async () => {
        await suya.goto(href);
        await expect(suya.locator("main").first()).toBeVisible();
        await suya.waitForTimeout(700); // the page's first answers
        const texto = await suya.locator("body").innerText();
        for (const marca of ajeno) expect(texto, `${href} shows vecina "${marca}"`).not.toContain(marca);
      });
    }

    // Her Ruta has her note; her bitácora, what she did and not what prueba did.
    await suya.goto("/ruta");
    await expect(suya.locator("main")).toContainText(nota("vecina").title);
    await suya.goto("/bitacora");
    await expect(suya.locator("table.bitacora").first()).toContainText("POST /api/ruta");
    await expect(suya.locator("main")).not.toContainText("POST /api/apps");
    await expect(suya.locator("main")).not.toContainText("prueba");
  } finally {
    await contexto.close();
  }
  expect(problemasSuyos, "errors in vecina's browser or the API").toEqual([]);

  // And prueba does not find what vecina wrote (her organization's name
  // she may see: prueba runs the platform).
  for (const href of ["/", "/ruta", "/bitacora"]) {
    await test.step(`prueba: ${href}`, async () => {
      await page.goto(href);
      await expect(page.locator("main h1").first()).toBeVisible();
      await page.waitForTimeout(700);
      const texto = await page.locator("body").innerText();
      expect(texto, `${href} shows prueba the other's note`).not.toContain(nota("vecina").title);
      expect(texto, `${href} shows prueba the other's note`).not.toContain("solo la lee vecina");
    });
  }
  await page.goto("/ruta");
  await expect(page.locator("main")).toContainText(nota("prueba").title);
  expect(problemas).toEqual([]);
});
