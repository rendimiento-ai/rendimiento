import { test, expect } from "./vigia";

// Without a connection the page opens from what the browser kept, says so
// and how old it is, switches off what would change something (never kept
// to be sent later), and keeps a draft of what is being written. The
// service worker registers in a browser test only when the test asks for
// it (rendimiento.trabajador): the other tests never read kept answers.
// Each test has a new browser context, so this worker and what it kept
// end with this test.
test("without a connection the page opens with what was kept, and switches orders off", async ({ page, context, problemas }, info) => {
  const es = info.project.name === "es";
  await page.addInitScript(() => localStorage.setItem("rendimiento.trabajador", "si"));

  // Online: the worker installs and takes the page; a second load goes through it, and its answers are kept.
  await page.goto("/");
  await page.waitForFunction(() => !!navigator.serviceWorker?.controller, undefined, { timeout: 60_000 });
  const apps = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/apps" && r.status() === 200);
  await page.reload();
  await apps;
  await expect(page.locator("main h1").first()).toBeVisible();
  await expect(page.locator(".conexion-aviso")).toHaveCount(0);
  const ruta = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/ruta" && r.status() === 200);
  await page.goto("/ruta");
  await ruta;
  await expect(page.getByRole("button", { name: es ? "Entrada nueva" : "New entry" })).toBeVisible();

  // Offline: the page opens anyway, with the notice.
  await context.setOffline(true);
  await page.goto("/");
  await expect(page.locator("header.topbar")).toBeVisible();
  await expect(page.locator("main h1").first()).toBeVisible();
  const aviso = page.locator(".conexion-aviso").filter({ hasText: es ? "Sin conexión." : "Without a connection." });
  await expect(aviso).toBeVisible();
  await expect(page.locator('main [data-falla="unreachable"]'), "kept data, not 'does not answer'").toHaveCount(0);

  // An order is switched off and says why; what is being written is kept as a draft.
  await page.goto("/ruta");
  await expect(aviso).toBeVisible();
  await page.getByRole("button", { name: es ? "Entrada nueva" : "New entry" }).click();
  const titulo = `sin conexión ${info.project.name} ${Date.now()}`;
  const forma = page.locator(".card.stack").filter({ has: page.locator("textarea") }).first();
  await forma.locator("label.field input").first().fill(titulo);
  const agregar = forma.locator("button.primary");
  await expect(agregar).toBeDisabled();
  await expect(agregar).toHaveAttribute("title", es ? /Sin conexión: las acciones que cambian algo esperan/ : /Without a connection: actions that change something wait/);
  await expect(forma.locator(".conexion-porque")).toBeVisible();
  await expect(forma.locator(".conexion-borrador")).toHaveText(es ? "Borrador guardado en este dispositivo" : "Draft kept on this device");

  // The draft comes back after a reload, still offline, and nothing was sent.
  await page.reload();
  await page.getByRole("button", { name: es ? "Entrada nueva" : "New entry" }).click();
  await expect(forma.locator("label.field input").first()).toHaveValue(titulo);

  // Back online: the notice goes by itself, and the order can be given.
  await context.setOffline(false);
  await expect(page.locator(".conexion-aviso")).toHaveCount(0, { timeout: 30_000 });
  await expect(agregar).toBeEnabled();
  const enviadas = await page.request.get(`/api/ruta?q=${encodeURIComponent(titulo)}`);
  expect(JSON.stringify(await enviadas.json()), "the draft was never sent by itself").not.toContain(titulo);
  // Cancel throws the draft away.
  await forma.getByRole("button", { name: es ? "Cancelar" : "Cancel" }).click();
  expect(await page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith("rendimiento.borrador.")))).toEqual([]);
  expect(problemas).toEqual([]);
});
