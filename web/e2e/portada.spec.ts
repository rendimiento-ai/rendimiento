import { fileURLToPath } from "node:url";
import { test, expect, vigilar } from "./vigia";

// The home page (the Apps page): the Today band, the folded scoreboard, the
// Numbers page that took the visits and delivery, and Customize home. Files
// run in name order: by now nueva-app.spec.ts has made the apps. The test
// server runs nothing, so what runs, what waits and yesterday's numbers are
// the page's own.
const estadoVecina = fileURLToPath(new URL(".estado-vecina.json", import.meta.url));
const hace = (s: number) => new Date(Date.now() - s * 1000).toISOString();

test("the Today band: Running, Waiting for you and Need help, each with its links", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const paso = (id: string, dependsOn: string[], status: string, desde?: number) => ({ id, service: id.split(":")[0], kind: "task", dependsOn, status, ...(desde ? { startedAt: hace(desde) } : {}) });
  const corre = {
    id: 880101, appId: 1, app: "una-que-corre", sha: "0123456789abcdef", branch: "vite-8", event: "push", deploy: false, status: "running", message: "",
    createdAt: hace(300), startedAt: hace(300),
    steps: [paso("web:build", [], "succeeded", 300), paso("web:test", ["web:build"], "running", 180), paso("deploy", ["web:test"], "pending")],
  };
  const espera = { ...corre, id: 880102, app: "otra-en-cola", branch: "main", status: "queued", startedAt: undefined, steps: [] };
  let corriendo: object = { ahora: [corre, espera], recientes: [] };
  await page.route("**/api/corriendo", (r) => r.fulfill({ json: corriendo }));
  await page.route("**/api/mesa", (r) => r.fulfill({ json: [{ app: "una-que-espera", you: 2 }, { app: "otra-que-espera", you: 1 }] }));
  // One of the real apps has a check down.
  let caida = "";
  await page.route("**/api/apps", async (r) => {
    const res = await r.fetch();
    const apps = (await res.json()) as { name: string; down?: string[] }[];
    caida = apps[0].name;
    apps[0].down = ["web"];
    await r.fulfill({ response: res, json: apps });
  });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/");

  const corren = page.locator("main section.hoy-corriendo");
  await expect(corren.locator(".hoy-cifra")).toHaveText("2");
  await expect(corren.locator('a[href="/apps/una-que-corre/runs/880101"]')).toContainText("una-que-corre");
  await expect(corren.locator('a[href="/apps/una-que-corre/runs/880101"]')).toContainText("web:test");
  await expect(corren.locator('a[href="/apps/otra-en-cola/runs/880102"]')).toContainText(es ? "Esperando su turno" : "Waiting for its turn");

  const esperan = page.locator("main section.hoy-esperan");
  await expect(esperan.locator(".hoy-cifra")).toHaveText("3");
  await expect(esperan.locator('a[href="/apps/una-que-espera/mesa"]')).toContainText("2");
  await expect(esperan.locator('a[href="/apps/otra-que-espera/mesa"]')).toBeVisible();

  const ayuda = page.locator("main section.hoy-ayuda");
  await expect(ayuda.locator(`a[href="/apps/${caida}"]`)).toContainText(es ? "caída: web" : "down: web");

  // Side by side on a wide screen, the same height; then the mosaic.
  const cajas = await Promise.all([corren, esperan, ayuda].map(async (l) => (await l.boundingBox())!));
  expect(new Set(cajas.map((c) => Math.round(c.y))).size, "one row").toBe(1);
  expect(Math.abs(cajas[0].width - cajas[2].width), "the same width").toBeLessThan(2);
  expect(cajas[0].y).toBeLessThan((await page.locator("main .mosaico .teja").first().boundingBox())!.y);
  // The notices that were strips here live in the bell now.
  await expect(page.locator("main .bando.publicada")).toHaveCount(0);

  // On a phone they stack, and nothing scrolls sideways.
  await page.setViewportSize({ width: 390, height: 844 });
  const pila = await Promise.all([corren, esperan, ayuda].map(async (l) => (await l.boundingBox())!.y));
  expect(pila[0]).toBeLessThan(pila[1]);
  expect(pila[1]).toBeLessThan(pila[2]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);

  // Nothing runs: all quiet, with the last run that ended.
  await page.setViewportSize({ width: 1280, height: 900 });
  corriendo = { ahora: [], recientes: [{ ...corre, status: "succeeded", steps: undefined, finishedAt: hace(60) }] };
  await page.reload();
  await expect(corren.locator(".ahora-calma")).toContainText(es ? "Todo en calma." : "All quiet.");
  await expect(corren.locator('a[href="/apps/una-que-corre/runs/880101"]')).toBeVisible();
  await expect(corren.locator(".hoy-cifra")).toHaveText("0");
  expect(problemas).toEqual([]);
});

test("yesterday's scoreboard is one line of the greeting that opens in place", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const ayer = new Date(Date.now() - 864e5);
  const dia = `${ayer.getFullYear()}-${String(ayer.getMonth() + 1).padStart(2, "0")}-${String(ayer.getDate()).padStart(2, "0")}`;
  const board = {
    releases: 3, healthy: 3, rollbacks: 0, runs: 68, failed: 13, merges: 2, problems: 0, toReview: 0,
    hours: Array.from({ length: 24 }, (_, h) => [h % 3, h % 5 === 0 ? 1 : 0]),
    apps: [{ app: "la-del-marcador", releases: 3, healthy: 3, rollbacks: 0, night: 0, runs: 68, failed: 13, merges: 2 }],
  };
  await page.route("**/api/summary", (r) => r.fulfill({ json: { days: [], pending: false, timeZone: "UTC", boards: { [dia]: board } } }));
  await page.goto("/");
  const linea = page.locator("main button.inicio-ayer");
  await expect(linea).toContainText(es ? "Ayer: 68 ejecuciones, 13 fallaron" : "Yesterday: 68 runs, 13 of them failed");
  await expect(linea).toHaveAttribute("aria-expanded", "false");
  await expect(page.locator("#marcador-del-dia")).toHaveCount(0);
  await linea.click();
  const marcador = page.locator("#marcador-del-dia");
  await expect(marcador).toContainText(es ? "El marcador del día" : "The day's scoreboard");
  await expect(linea).toHaveAttribute("aria-expanded", "true");
  // Right under the greeting, above the band.
  expect((await marcador.boundingBox())!.y).toBeLessThan((await page.locator("main .hoy").boundingBox())!.y);
  await marcador.getByRole("button", { name: es ? "Plegarlo" : "Fold it" }).click();
  await expect(marcador).toHaveCount(0);
  // The quiet line under the mosaic opens it too.
  const pie = page.locator("main nav.inicio-pie");
  await expect(pie.locator('a[href="/numeros"]')).toBeVisible();
  await pie.getByRole("button", { name: es ? /3 de 3 versiones sanas/ : /3 of 3 releases healthy/ }).click();
  await expect(page.locator("#marcador-del-dia")).toBeVisible();
  expect(problemas).toEqual([]);
});

test("Numbers: the visits, and delivery for the operators", async ({ page, browser, baseURL, problemas }, info) => {
  const es = info.project.name === "es";
  await page.goto("/");
  await page.locator("main nav.inicio-pie a[href='/numeros']").click();
  await expect(page).toHaveURL(/\/numeros$/);
  await expect(page.locator("main h1")).toHaveText(es ? "Números" : "Numbers");
  await expect(page.locator("main #visitas")).toContainText(es ? "Visitantes" : "Visitors");
  await expect(page.locator("main #entregas .dlv h2")).toContainText(es ? "Entregas" : "Delivery");
  await expect(page.locator("main a[href='/uso']")).toBeVisible();
  // In the top bar's pages too.
  await expect(page.locator("header.topbar .topbar-secciones a[href='/numeros']")).toHaveClass(/active/);

  // Someone who is not an operator sees the visits, not delivery.
  const contexto = await browser.newContext({ storageState: estadoVecina, baseURL, locale: info.project.use.locale });
  const suya = await contexto.newPage();
  const suyos = vigilar(suya);
  try {
    await suya.goto("/numeros");
    await expect(suya.locator("main #visitas")).toContainText(es ? "Visitantes" : "Visitors");
    await expect(suya.locator("main #entregas")).toHaveCount(0);
    await expect(suya.locator("main a[href='/uso']")).toHaveCount(0);
    expect(suyos).toEqual([]);
  } finally {
    await contexto.close();
  }
  expect(problemas).toEqual([]);
});

/** The sections of the Customize panel, in its order. */
const orden = (page: import("@playwright/test").Page) => page.locator("#inicio-ajustes li").evaluateAll((ls) => ls.map((l) => l.getAttribute("data-seccion")));
/** Reloads without what this browser kept: what shows is the server's. */
const deNuevo = async (page: import("@playwright/test").Page) => {
  await page.evaluate(() => localStorage.removeItem("rendimiento.inicio"));
  await page.reload();
  await expect(page.locator("main .hero-vida .sub[data-listo]")).toBeVisible();
};

test("Customize home: a section off stays off, the order changes, and back to the usual", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/");
  const abrir = page.getByRole("button", { name: es ? "Personalizar inicio" : "Customize home" });
  await abrir.click();
  const panel = page.locator("#inicio-ajustes");
  await expect(panel).toBeVisible();
  expect(await orden(page)).toEqual(["corriendo", "esperan", "ayuda", "apps", "marcador", "visitas", "entregas", "lluvia"]);

  // Off: it goes at once, and stays gone once saved, on any device.
  await panel.getByRole("switch", { name: es ? "En ejecución" : "Running" }).click();
  await expect(page.locator("main section.hoy-corriendo")).toHaveCount(0);
  await panel.getByRole("button", { name: es ? "Guardar" : "Save" }).click();
  await expect(panel).toHaveCount(0);
  await deNuevo(page);
  await expect(page.locator("main section.hoy-esperan")).toBeVisible();
  await expect(page.locator("main section.hoy-corriendo")).toHaveCount(0);

  // The order: with the arrows (and the keyboard on them), and by dragging.
  await abrir.click();
  const mosaico = es ? "Sus aplicaciones (el mosaico)" : "Your apps (the mosaic)";
  const subir = panel.getByRole("button", { name: es ? `Subir ${mosaico}` : `Move ${mosaico} up` });
  await subir.click();
  await subir.click();
  await subir.focus();
  await page.keyboard.press("Enter");
  expect(await orden(page)).toEqual(["apps", "corriendo", "esperan", "ayuda", "marcador", "visitas", "entregas", "lluvia"]);
  await panel.locator("li[data-seccion=ayuda]").dragTo(panel.locator("li[data-seccion=corriendo]"));
  expect(await orden(page)).toEqual(["apps", "ayuda", "corriendo", "esperan", "marcador", "visitas", "entregas", "lluvia"]);
  await panel.getByRole("button", { name: es ? "Guardar" : "Save" }).click();
  await deNuevo(page);
  const y = async (sel: string) => (await page.locator(sel).first().boundingBox())?.y ?? -1;
  await expect.poll(async () => (await y("main .mosaico .teja")) < (await y("main section.hoy-ayuda")), { message: "the mosaic first" }).toBe(true);

  // Back to the usual: the built-in home again.
  await abrir.click();
  await panel.getByRole("button", { name: es ? "Volver a lo de siempre" : "Back to the usual", exact: true }).click();
  await expect.poll(() => orden(page)).toEqual(["corriendo", "esperan", "ayuda", "apps", "marcador", "visitas", "entregas", "lluvia"]);
  await panel.getByRole("button", { name: es ? "Cancelar" : "Cancel" }).click();
  await deNuevo(page);
  await expect(page.locator("main section.hoy-corriendo")).toBeVisible();
  await expect.poll(async () => (await y("main section.hoy-corriendo")) < (await y("main .mosaico .teja")), { message: "the band first again" }).toBe(true);
  expect(problemas).toEqual([]);
});

test("an administrator makes a home the default for whoever never chose", async ({ page, browser, baseURL, problemas }, info) => {
  const es = info.project.name === "es";
  await page.goto("/");
  await page.getByRole("button", { name: es ? "Personalizar inicio" : "Customize home" }).click();
  const panel = page.locator("#inicio-ajustes");
  await panel.getByRole("switch", { name: es ? "Necesitan ayuda" : "Need help" }).click();
  await panel.getByRole("button", { name: es ? "Hacer esto lo predeterminado para todos" : "Make this the default for everyone" }).click();
  await expect(panel).toContainText(es ? "prueba fijó lo predeterminado" : "The default was set by prueba");
  await panel.getByRole("button", { name: es ? "Cancelar" : "Cancel" }).click();
  // prueba never chose a home of her own: she sees the default.
  await deNuevo(page);
  await expect(page.locator("main section.hoy-esperan")).toBeVisible();
  await expect(page.locator("main section.hoy-ayuda")).toHaveCount(0);

  // So does anyone else who never chose, in another organization too; and she cannot set it.
  const contexto = await browser.newContext({ storageState: estadoVecina, baseURL, locale: info.project.use.locale });
  try {
    const suyo = await (await contexto.request.get("/api/inicio")).json();
    expect(suyo.secciones.find((s: { id: string }) => s.id === "ayuda").on).toBe(false);
    expect(suyo.puedeFijar).toBe(false);
    expect((await contexto.request.put("/api/inicio/predeterminado", { data: { secciones: [{ id: "apps", on: true }] } })).status()).toBe(403);
  } finally {
    await contexto.close();
  }

  // Back to the built-in default.
  await page.getByRole("button", { name: es ? "Personalizar inicio" : "Customize home" }).click();
  await panel.getByRole("button", { name: es ? "Volver a lo de fábrica" : "Back to the built-in default" }).click();
  await expect(panel.getByRole("button", { name: es ? "Volver a lo de fábrica" : "Back to the built-in default" })).toHaveCount(0);
  await panel.getByRole("button", { name: es ? "Cancelar" : "Cancel" }).click();
  await deNuevo(page);
  await expect(page.locator("main section.hoy-ayuda")).toBeVisible();
  expect(problemas).toEqual([]);
});

// The notices that were strips on the home page are in the bell: a newer
// rendimiento, and the señales until an operator saw them.
test("the bell has what the home page's strips said", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.route("**/api/platform/published", (r) => r.fulfill({ json: {
    version: "0.2.0", how: "computer", newer: true, onlyFix: true, canInstall: false, auto: false,
    latest: { version: "0.2.9", notas: "", url: "https://github.com/rendimiento-ai/rendimiento/releases/tag/v0.2.9", fecha: hace(3600), firmada: true },
  } }));
  const senales = await (await page.request.get("/api/senales/estado")).json();
  await page.goto("/");
  await page.locator("button.campana-boton").click();
  const panel = page.locator(".campana-panel");
  await expect(panel.locator('a[href="/environment"]', { hasText: es ? "rendimiento 0.2.9 ya está publicada" : "rendimiento 0.2.9 is published" })).toBeVisible();
  if (senales.encendidas && !senales.avisoVisto) {
    await expect(panel.locator('a[href="/senales"]', { hasText: "señal de vida" })).toBeVisible();
  }
  expect(problemas).toEqual([]);
});
