import { test, expect } from "./vigia";

// An encargo the coding agent gave back (Ruta: comentar-al-reenviar): on
// «The agent» and on its card in Encargos, on a phone, the person writes
// what it should know when it tries again, and «Send it again» or «Leave it
// to my agent» carry it. The server's answers are the test's own: the
// platform's coding agent never runs here.

const devuelto = (id: number, title: string) => ({
  id, title, body: "", app: "tienda", status: "pending", createdBy: "p0dxD", byAgent: false, links: [], route: "local", givenBack: true,
  spentMicros: 120_000, createdAt: new Date(Date.now() - 3600e3).toISOString(), updatedAt: new Date().toISOString(),
});
const porque = "Me detuve: la migración necesita lock_timeout y no sé si lo quiere.";

test("a given-back encargo goes again with what it should know", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.setViewportSize({ width: 390, height: 844 });
  let returned = [devuelto(990201, "Endurecer las migraciones"), devuelto(990203, "Mirar el índice de runs")];
  await page.route("**/api/encargos/cola*", async (r) => {
    const res = await r.fetch();
    await r.fulfill({ response: res, json: { ...(await res.json()), mode: "asked", returned } });
  });
  await page.route((u) => u.pathname === "/api/encargos", async (r) => {
    const res = await r.fetch();
    await r.fulfill({ response: res, json: [...returned, ...(await res.json())] });
  });
  await page.route(/\/api\/encargos\/99020\d$/, (r) => {
    const id = Number(new URL(r.request().url()).pathname.split("/").pop());
    return r.fulfill({ json: { ...devuelto(id, "x"), log: [{ at: new Date().toISOString(), who: "agente de código", what: "released", note: porque }] } });
  });
  const enviado: Record<string, unknown> = {};
  await page.route("**/api/encargos/do", async (r) => {
    const body = r.request().postDataJSON();
    enviado.do = body;
    returned = returned.filter((e) => !body.ids.includes(e.id));
    await r.fulfill({ json: { sent: body.ids.length } });
  });
  await page.route(/\/api\/encargos\/99020\d\/route$/, async (r) => {
    const id = Number(new URL(r.request().url()).pathname.split("/")[3]);
    enviado.route = r.request().postDataJSON();
    returned = returned.filter((e) => e.id !== id);
    await r.fulfill({ json: { ...devuelto(id, "x"), givenBack: false } });
  });

  const label = es ? "Lo que quiere que sepa al volver a intentarlo" : "What it should know when it tries again";
  await page.goto("/ruta?tab=agente");
  const tarjeta = page.locator(".cola-fin.devuelto").first();
  await expect(tarjeta).toContainText(porque);
  await expect(page.locator(".cola-fin.devuelto")).toHaveCount(2);
  await tarjeta.getByLabel(label).fill("  Sí, haga lo de lock_timeout.  ");
  for (const b of await tarjeta.locator(".et-acciones .et-boton").all()) expect((await b.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await tarjeta.getByRole("button", { name: es ? "Mandarlo otra vez" : "Send it again" }).click();
  await expect(page.locator(".cola-fin.devuelto")).toHaveCount(1);
  expect(enviado.do).toEqual({ ids: [990201], notes: { "990201": "Sí, haga lo de lock_timeout." } });

  // On its card in Encargos: to my own agent, with the note.
  await page.goto("/ruta?tab=encargos");
  const card = page.locator("#encargo-990203");
  await card.getByLabel(label).fill("Mire el índice en vez de la migración");
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await card.getByRole("button", { name: es ? "Dejarlo a mi agente" : "Leave it to my agent" }).click();
  await expect.poll(() => enviado.route).toEqual({ route: "local", note: "Mire el índice en vez de la migración" });
  expect(problemas).toEqual([]);
});
