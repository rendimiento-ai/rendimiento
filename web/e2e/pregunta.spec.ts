import { test, expect } from "./vigia";

// What the coding agent asks (Ruta 178) shows first on «For you», on a
// phone: its options, a line more, «Answer and let it go on» (the answer
// sent is the option and the line) and «Pass it to my agent». The server's
// answers are the test's own: the platform's coding agent never runs here.

test("the coding agent's question: answered on «For you», or passed to my agent", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.setViewportSize({ width: 390, height: 844 });
  const pregunta = (id: number) => ({
    id, title: "Comentarios que la gente le escriba al agente", body: "", app: "tienda", status: "asking", createdBy: "p0dxD", byAgent: false,
    keyName: "agente de código", links: [], createdAt: new Date().toISOString(), updatedAt: new Date().toISOString(),
    question: { id: id + 1, encargoId: id, question: "Usted pidió el diseño antes del código. ¿Cuál de las dos formas?", askedAt: new Date(Date.now() - 4 * 60e3).toISOString(),
      options: ["Un botón «Comentar» en cada entrega", "Los comentarios dentro del encargo, como una plática"] },
  });
  let asking = [pregunta(990001), pregunta(990003)];
  await page.route("**/api/ruta/portada*", async (r) => {
    const res = await r.fetch();
    await r.fulfill({ response: res, json: { ...(await res.json()), asking } });
  });
  const enviado: Record<string, unknown> = {};
  await page.route("**/api/encargos/99000*/*", async (r) => {
    const [, id, que] = new URL(r.request().url()).pathname.match(/\/api\/encargos\/(\d+)\/(\w+)$/) ?? [];
    enviado[`${id} ${que}`] = r.request().postDataJSON() ?? true;
    asking = asking.filter((e) => e.id !== Number(id));
    await r.fulfill({ json: { ...pregunta(Number(id)), status: "pending", question: undefined } });
  });

  await page.goto("/ruta");
  const tarjeta = page.locator("section.pu-pregunta").first();
  await expect(tarjeta).toContainText("¿Cuál de las dos formas?");
  await expect(page.locator("section.pu-pregunta")).toHaveCount(2);
  const responder = tarjeta.getByRole("button", { name: es ? "Responder y que siga" : "Answer and let it go on" });
  await expect(responder).toBeDisabled();
  await tarjeta.getByRole("radio").nth(1).check();
  await tarjeta.locator("textarea").fill("Y que se vean también en la campana");
  for (const b of await tarjeta.locator(".pu-acciones .et-boton").all()) expect((await b.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await responder.click();
  await expect(page.locator(".pu-principal [role=status]")).toContainText("990001");
  expect(enviado["990001 answer"]).toEqual({ answer: "B. Los comentarios dentro del encargo, como una plática\nY que se vean también en la campana" });
  await expect(page.locator("section.pu-pregunta")).toHaveCount(1);

  await page.locator("section.pu-pregunta").getByRole("button", { name: es ? "Pasarlo a mi agente" : "Pass it to my agent" }).click();
  await expect(page.locator(".pu-principal [role=status]")).toContainText("990003");
  expect(enviado["990003 pass"]).toBe(true);
  await expect(page.locator("section.pu-pregunta")).toHaveCount(0);
  expect(problemas).toEqual([]);
});
