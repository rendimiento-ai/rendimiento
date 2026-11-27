import { test, expect } from "./vigia";
import type { Locator, Page } from "@playwright/test";

// On a phone the sheet keeps its icons in place (seen on p0dxD's phone: the ×
// off to the left of its circle, the send button cut at the right edge, the
// sheet over the page's top bar): the × and the arrow centered in their
// circles, the arrow inside the screen, the sheet starting below the top bar.
async function enSuLugar(page: Page, hoja: Locator, ancho: number) {
  const centro = async (boton: Locator) => boton.evaluate((b) => {
    const caja = b.getBoundingClientRect();
    const r = document.createRange();
    r.selectNodeContents(b);
    const g = r.getBoundingClientRect();
    return { dx: Math.abs((g.left + g.right) / 2 - (caja.left + caja.right) / 2), right: caja.right };
  });
  const cerrar = await centro(hoja.locator(".ay-cerrar"));
  expect(cerrar.dx).toBeLessThanOrEqual(2);
  await hoja.locator("#ayudante-pregunta").fill("?");
  const enviar = await centro(hoja.locator(".ay-enviar"));
  expect(enviar.dx).toBeLessThanOrEqual(2);
  expect(enviar.right).toBeLessThanOrEqual(ancho);
  await hoja.locator("#ayudante-pregunta").fill("");
  const barra = await page.locator(".topbar").boundingBox();
  const arriba = await hoja.boundingBox();
  expect(arriba!.y).toBeGreaterThanOrEqual(barra!.y + barra!.height - 1);
}

// El ayudante on a phone: the hummingbird's button opens a sheet; a quick
// question is asked with the page it was asked from, and the answer brings
// links (a page of rendimiento that opens here and closes the sheet, a page
// of the book); after the local model it offers the platform's AI; Escape
// closes it; nothing is wider than the phone. The helper's answers are
// mocked: the test server has no model.

test("the hummingbird: asked from a page, it answers with where to go", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.setViewportSize({ width: 390, height: 844 });
  const mensajes: unknown[] = [];
  let pedido: { texto: string; pagina: string; ia?: string } | undefined;
  await page.route("**/api/ayudante", async (r) => {
    if (r.request().method() === "GET") {
      await r.fulfill({ json: { mensajes, ias: [{ id: "local", model: "qwen", mensajeMicros: 0, gratis: true }, { id: "platform", model: "claude-haiku-5-5", mensajeMicros: 9000 }], ia: "local", libro: "https://learn.example.com/" } });
      return;
    }
    pedido = r.request().postDataJSON();
    const ahora = new Date().toISOString();
    await r.fulfill({ json: {
      pregunta: { id: 1, kind: "persona", body: pedido!.texto, pagina: pedido!.pagina, enlaces: [], leido: [], costMicros: 0, createdAt: ahora },
      respuesta: { id: 2, kind: "ayudante", body: es ? "Las llaves están en Entorno." : "The keys are in the Environment.", provider: "local", model: "qwen", costMicros: 0, createdAt: ahora,
        leido: ["libro:guide/keys/|Keys"],
        enlaces: [{ texto: es ? "Entorno → Llaves" : "Environment → Keys", href: "/environment/llaves", tipo: "pagina" }, { texto: "Keys", href: "libro:guide/keys/", tipo: "libro" }] },
    } });
  });

  await page.goto("/ruta");
  await expect(page.locator("main h1").first()).toBeVisible();
  const boton = page.getByRole("button", { name: es ? "Pregúntele al colibrí" : "Ask the hummingbird" });
  await expect(boton).toBeVisible();
  await boton.click();
  const hoja = page.getByRole("dialog", { name: es ? "Tsïntsuni, el colibrí" : "Tsïntsuni, the hummingbird" });
  await expect(hoja).toBeVisible();
  await expect(hoja.locator(".ay-hola")).toBeVisible();
  await enSuLugar(page, hoja, 390);

  await hoja.getByRole("button", { name: es ? "¿Dónde están las llaves?" : "Where are the keys?" }).click();
  await expect(hoja.locator(".ay-msg.respuesta")).toContainText(es ? "Las llaves están en Entorno." : "The keys are in the Environment.");
  expect(pedido?.pagina).toBe("/ruta");
  expect(pedido?.ia).toBeUndefined();
  const libro = hoja.getByRole("link", { name: /Keys/ }).last();
  await expect(libro).toHaveAttribute("href", "https://learn.example.com/guide/keys/");
  await expect(libro).toHaveAttribute("target", "_blank");
  await expect(hoja.getByRole("button", { name: es ? "Preguntarle a la IA de la plataforma" : "Ask the platform's AI" })).toBeVisible();
  for (const b of await hoja.locator(".ay-enlace").all()) expect((await b.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);

  // A page of rendimiento opens here, and on a phone the sheet closes.
  await hoja.getByRole("link", { name: es ? "Entorno → Llaves" : "Environment → Keys" }).click();
  await expect(page).toHaveURL(/\/environment\/llaves$/);
  await expect(hoja).toHaveCount(0);

  // Escape closes it.
  await boton.click();
  await expect(hoja).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(hoja).toHaveCount(0);

  // A narrow phone too.
  await page.setViewportSize({ width: 320, height: 640 });
  await boton.click();
  await expect(hoja).toBeVisible();
  await enSuLugar(page, hoja, 320);
  expect(problemas).toEqual([]);
});
