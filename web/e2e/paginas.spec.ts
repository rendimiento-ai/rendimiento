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

// On a phone the menu is a sheet that always peeks from the bottom, with
// the pages and their drawings: nothing hides behind a button. Its handle
// raises it to every page, then to the account; a page's link lowers it.
// The top bar is one short row, and nothing is wider than the screen.
test("on a phone the menu is a sheet that peeks from the bottom", async ({ page, problemas }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  expect((await page.locator("header.topbar").boundingBox())!.height, "one short row").toBeLessThan(70);
  await expect(page.locator("header.topbar a.nav-link")).toHaveCount(0);
  const hoja = page.locator(".menu-hoja");
  const enlaces = hoja.locator(".menu-hoja-paginas a.nav-link");
  await expect(enlaces.first()).toBeVisible();
  await expect(enlaces.first().locator("svg.dibujo")).toBeVisible();
  const asomada = (await hoja.boundingBox())!;
  expect(asomada.height, "it peeks").toBeLessThan(130);
  expect(asomada.y + asomada.height).toBeGreaterThan(840);
  await hoja.locator(".menu-hoja-agarradera").click();
  await expect(hoja).toHaveAttribute("data-altura", "1");
  await hoja.locator(".menu-hoja-agarradera").click();
  await expect(hoja).toHaveAttribute("data-altura", "2");
  await expect(hoja.locator(".menu-hoja-cuenta button").last()).toBeInViewport(); // Sign out
  await enlaces.nth(1).click();
  await expect(hoja, "a page's link lowers it").toHaveAttribute("data-altura", "0");
  // Dragging it up with a finger raises it; dragging it down lowers it.
  await page.waitForTimeout(500); // the sheet has finished lowering
  const caja = (await hoja.boundingBox())!;
  const arrastra = async (desde: number, hasta: number) => {
    const x = caja.x + caja.width / 2;
    await hoja.dispatchEvent("pointerdown", { pointerId: 7, pointerType: "touch", button: 0, clientX: x, clientY: desde, bubbles: true });
    for (const y of [desde + (hasta - desde) / 3, desde + ((hasta - desde) * 2) / 3, hasta]) {
      await hoja.dispatchEvent("pointermove", { pointerId: 7, pointerType: "touch", clientX: x, clientY: y, bubbles: true });
    }
    await hoja.dispatchEvent("pointerup", { pointerId: 7, pointerType: "touch", clientX: x, clientY: hasta, bubbles: true });
  };
  await arrastra(caja.y + 40, caja.y - 30);
  await expect(hoja, "dragged up").toHaveAttribute("data-altura", "1");
  await page.waitForTimeout(500);
  await arrastra(700, 800);
  await expect(hoja, "dragged down").toHaveAttribute("data-altura", "0");
  await page.locator("button.topbar-yo").click();
  await expect(hoja).toHaveAttribute("data-altura", "2");
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});

// On a wide screen each page's name has its drawing beside it.
test("the top bar's pages have their drawings", async ({ page, problemas }) => {
  await page.goto("/");
  const enlaces = page.locator("header.topbar a.nav-link:has(svg.dibujo)");
  await expect(enlaces.first()).toBeVisible(); // count() does not wait for the page to load
  expect(await enlaces.count()).toBeGreaterThan(3);
  await expect(page.locator(".menu-hoja")).toHaveCount(0);
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

// The señales page: this installation's, and those received here.
test("the señales page", async ({ page, problemas }, info) => {
  await page.goto("/senales");
  const es = info.project.name === "es";
  await expect(page.locator("main h1").first()).toContainText("Señales de vida");
  await expect(page.locator("main")).toContainText(es ? "Mandadas desde aquí" : "Sent from here");
  await expect(page.locator("main")).toContainText(es ? "Recibidas de todas las instalaciones" : "Received from every installation");
  expect(problemas).toEqual([]);
});

// The colors beside the day/night switch, in the account's menu: a family
// and a mode, chosen apart, both kept through a reload; the classic papel
// picado takes the family away.
test("the colors and the day/night mode are kept", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/");
  const raiz = page.locator("html");
  const cuenta = page.locator("header.topbar button.cuenta-boton");
  const boton = page.locator("#menu-cuenta button.tema-boton");
  await cuenta.click();
  await boton.click();
  await expect(boton).toHaveAttribute("aria-expanded", "true");
  await page.locator(".tema-panel").getByRole("button", { name: "Sierra" }).click();
  await expect(raiz).toHaveAttribute("data-tema", "sierra");
  await expect(page.locator(".tema-panel")).toBeHidden();
  await expect(page.locator(".tema-franja")).toBeVisible(); // the family's band, in place of the papel picado
  await expect(page.locator(".papel-picado")).toBeHidden();

  await page.getByRole("button", { name: es ? "Cambiar a la noche" : "Switch to night" }).click();
  await expect(raiz).toHaveAttribute("data-theme", "night");
  await expect(raiz).toHaveAttribute("data-tema", "sierra");

  await page.reload();
  await expect(page.locator("header.topbar")).toBeVisible();
  await expect(raiz).toHaveAttribute("data-theme", "night");
  await expect(raiz).toHaveAttribute("data-tema", "sierra");

  // Escape closes the list without choosing, and the menu stays; then the classic one.
  await cuenta.click();
  await boton.click();
  await expect(page.locator(".tema-panel")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".tema-panel")).toBeHidden();
  await expect(page.locator("#menu-cuenta")).toBeVisible();
  await expect(raiz).toHaveAttribute("data-tema", "sierra");
  await boton.click();
  await page.locator(".tema-panel").getByRole("button", { name: "Papel picado" }).click();
  await expect(raiz).not.toHaveAttribute("data-tema", /.*/);
  await expect(raiz).toHaveAttribute("data-theme", "night");
  await expect(page.locator(".papel-picado")).toBeVisible();
  expect(problemas).toEqual([]);
});

// The band of a family with a sun (Sierra, Atardecer, Milpa) has one sun, by
// day and by night, however wide the screen: the pattern repeats, the sun not.
test("the band's sun is one, on a wide screen too", async ({ page, problemas }) => {
  await page.setViewportSize({ width: 1920, height: 900 });
  await page.goto("/");
  await expect(page.locator("header.topbar")).toBeVisible();
  const sol = page.locator(".tema-franja .tema-sol");
  for (const tema of ["sierra", "atardecer", "milpa", "bugambilia", "talavera", "adobe"]) {
    for (const modo of ["day", "night"]) {
      await page.evaluate(([f, m]) => { document.documentElement.dataset.tema = f; document.documentElement.dataset.theme = m; }, [tema, modo]);
      await expect(page.locator(".tema-franja")).toBeVisible();
      if (["sierra", "atardecer", "milpa"].includes(tema)) {
        await expect(sol).toHaveCount(1);
        await expect(sol).toBeVisible();
        const caja = (await sol.boundingBox())!;
        expect(caja.width, tema).toBeGreaterThan(10);
        expect(caja.width, tema).toBeLessThan(60);
        // No sun left in the pattern that repeats.
        const patron = await page.locator(".tema-franja").evaluate((el) => getComputedStyle(el, "::after").backgroundImage);
        expect(patron, tema).not.toContain("circle");
      } else {
        await expect(sol).toBeHidden();
      }
    }
  }
  expect(problemas).toEqual([]);
});

// The Universe's town takes the colors of the family chosen: its sky (the
// canvas's corner) changes with the family, and is the village's own without one.
test("the Universe's town follows the family of colors", async ({ page, problemas }) => {
  await page.goto("/universe");
  const lienzo = page.locator("canvas.u-canvas");
  await expect(lienzo).toBeVisible();
  const cielo = async (tema: string) => {
    await page.evaluate((f) => {
      document.documentElement.dataset.theme = "day";
      if (f) document.documentElement.dataset.tema = f;
      else delete document.documentElement.dataset.tema;
    }, tema);
    // A few frames: the theme is read again and the town drawn with it.
    await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => requestAnimationFrame(r)))));
    return lienzo.evaluate((c: HTMLCanvasElement) => [...c.getContext("2d")!.getImageData(2, 2, 1, 1).data.slice(0, 3)].join(","));
  };
  const clasico = await cielo("");
  const sierra = await cielo("sierra");
  const talavera = await cielo("talavera");
  const atardecer = await cielo("atardecer");
  expect(new Set([clasico, sierra, talavera, atardecer]).size).toBe(4);
  expect(await cielo("")).toBe(clasico);
  expect(problemas).toEqual([]);
});

// While a page's data is on its way, its skeleton stands where the page will
// be (busy, with no title of its own); when the answer comes, the page
// replaces it and no skeleton is left. The answer is held until the skeleton
// was seen.
test("a page's skeleton stands in until its data comes", async ({ page, problemas }) => {
  const apps = (await (await page.request.get("/api/apps")).json()) as { name: string }[];
  expect(apps.length, "apps made by nueva-app.spec.ts").toBeGreaterThan(0);
  const esqueleto = page.locator("main .esq[aria-busy='true']");
  const retener = async (patron: string) => {
    let soltar = () => {};
    const suelta = new Promise<void>((r) => (soltar = r));
    await page.route(patron, async (ruta) => { await suelta; await ruta.continue(); });
    // Released, the handlers still running finish before the route goes:
    // removing it under them answers the request twice.
    return () => { soltar(); return page.unrouteAll({ behavior: "wait" }); };
  };

  await test.step("Apps", async () => {
    const soltar = await retener("**/api/apps");
    await page.goto("/");
    await expect(esqueleto.first()).toBeVisible();
    await expect(page.locator("main .mosaico .teja")).toHaveCount(0);
    await soltar();
    await expect(page.locator("main .mosaico .teja").first()).toBeVisible();
    await expect(page.locator("main .esq")).toHaveCount(0);
  });

  await test.step("an app's page", async () => {
    const app = apps[0].name;
    const soltar = await retener(`**/api/apps/${app}`);
    await page.goto(`/apps/${app}`);
    await expect(esqueleto.first()).toBeVisible();
    await expect(page.locator("main h1"), "a skeleton has no title").toHaveCount(0);
    await soltar();
    await expect(page.locator("main h1").first()).toHaveText(app);
    await expect(page.locator("main .esq")).toHaveCount(0);
  });
  expect(problemas).toEqual([]);
});
