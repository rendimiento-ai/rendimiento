import { test, expect } from "./vigia";

// What each agent may do (Ruta 184), on a phone. On the AI page: the subjects as a list (the coding agent
// first), each permission's mode as a segmented control (an action with effect never «Always»), the change
// bar before «Save», saved for real and put back. On «For you»: the agent's request with its reach and «No»,
// and an action with effect with only «Do it» / «No». The server's answers to the requests are the test's own.

const phone = { width: 390, height: 844 };
// The AI page's own view (the test server has no AI gateway): only what the page needs.
const vista = {
  settings: {
    orgId: 1, provider: "local", capMicros: 1_000_000, allowanceMicros: 0, autoDiagnose: true, escalateTo: "", escalateLimit: 10,
    encargoAutoMicros: 500_000, codeAgent: "asked", codeConcurrency: 1, estimateLearn: true, autoEncargo: "off", updatedAt: new Date().toISOString(),
  },
  spentOwnMicros: 0, spentPlatformMicros: 0, localModel: "qwen3.5:4b", platformAvailable: false, escalations: 0, usage: [], coderSandbox: true,
};

test("the Permissions section: a mode changed, saved and put back", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.setViewportSize(phone);
  await page.route("**/api/ia", (r) => r.request().method() === "GET" ? r.fulfill({ json: vista }) : r.fallback());
  await page.goto("/ia#permisos-coder");
  const sec = page.locator("section#permisos");
  await expect(sec.getByRole("radio", { name: es ? "El agente de código" : "The coding agent" })).toHaveAttribute("aria-checked", "true");
  await expect(sec.getByRole("radio", { name: es ? "El agente diagnosticador" : "The diagnostician" })).toBeVisible();
  const fila = sec.locator(".permisos-fila", { hasText: es ? "Ver la infraestructura de su app" : "See its app's infrastructure" });
  const siempre = fila.getByRole("radio", { name: es ? "Siempre" : "Always" });
  const preguntar = fila.getByRole("radio", { name: es ? "Preguntar" : "Ask" });
  const efectos = sec.locator(".permisos-fila", { hasText: es ? "Acciones con efecto" : "Actions with effect" });
  await expect(efectos.getByRole("radio", { name: es ? "Siempre" : "Always" })).toBeDisabled();
  const antes = (await siempre.getAttribute("aria-checked")) === "true" ? siempre : preguntar;
  const otro = antes === siempre ? preguntar : siempre;
  await otro.click();
  const barra = sec.locator(".ia-savebar");
  await expect(barra).toContainText(es ? "Ver la infraestructura de su app" : "See its app's infrastructure");
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(phone.width);
  for (const b of await fila.getByRole("radio").all()) expect((await b.boundingBox())!.height).toBeGreaterThanOrEqual(36);
  await barra.getByRole("button", { name: es ? "Guardar" : "Save" }).click();
  await expect(sec.locator(".badge.ok", { hasText: es ? "Guardado" : "Saved" })).toBeVisible();
  await expect(otro).toHaveAttribute("aria-checked", "true");
  // Put back as it was, so the other language's run finds the same.
  await antes.click();
  await sec.locator(".ia-savebar").getByRole("button", { name: es ? "Guardar" : "Save" }).click();
  await expect(antes).toHaveAttribute("aria-checked", "true");
  await expect(sec.locator(".ia-savebar")).toHaveCount(0);
  expect(problemas).toEqual([]);
});

test("«For you»: the agent asks for permission, given for the encargo; an action with effect, said no", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.setViewportSize(phone);
  const ahora = new Date().toISOString();
  let permisos = [
    { id: 880001, sujeto: "coder", permiso: "infra_read", app: "stockpulse", tool: "infra_read", que: "ver la infraestructura de stockpulse: events, logs",
      porQue: "El rollout se agotó; los eventos dicen si el pod quedó sin lugar.", ventana: "los últimos 10 min", encargoId: 53, encargoTitle: "Por qué no arranca la 71",
      askedBy: "agente de código", askedAt: ahora },
    { id: 880002, sujeto: "mcp:7", permiso: "effects", app: "stockpulse", accion: "rerun:feature/encargo-53", que: "relanzar la ejecución de la rama feature/encargo-53",
      porQue: "Falló por el clúster (etcd).", askedBy: "claude", askedAt: ahora, keyName: "claude", cargoId: 4 },
  ];
  await page.route("**/api/ruta/portada*", async (r) => {
    const res = await r.fetch();
    await r.fulfill({ response: res, json: { ...(await res.json()), permisos } });
  });
  const enviado: Record<string, unknown> = {};
  await page.route("**/api/permisos/pedidos/88000*", async (r) => {
    const id = Number(new URL(r.request().url()).pathname.split("/").pop());
    enviado[id] = r.request().postDataJSON();
    permisos = permisos.filter((p) => p.id !== id);
    await r.fulfill({ json: { pedido: { id }, dado: null } });
  });
  await page.goto("/ruta");
  const pide = page.locator("section.pu-permiso").first();
  await expect(pide).toContainText(es ? "El agente pide permiso" : "The agent asks for permission");
  await expect(pide).toContainText("El rollout se agotó");
  await expect(pide.getByRole("radio")).toHaveCount(3);
  await pide.getByRole("radio", { name: es ? "Mientras dure este encargo" : "While this encargo lasts" }).check();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(phone.width);
  for (const b of await pide.locator(".pu-acciones .et-boton").all()) expect((await b.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  await pide.getByRole("button", { name: es ? "Dar permiso" : "Give permission" }).click();
  await expect(page.locator(".pu-principal [role=status]")).toContainText(es ? "Permiso dado" : "Permission given");
  expect(enviado[880001]).toEqual({ answer: "encargo" });

  const efecto = page.locator("section.pu-permiso.efecto");
  await expect(efecto).toContainText(es ? "Quiere hacer algo con efecto" : "It wants to do something with effect");
  await expect(efecto.getByRole("radio")).toHaveCount(0);
  await expect(efecto.getByRole("button", { name: es ? "Hacerlo" : "Do it" })).toBeVisible();
  await efecto.getByRole("button", { name: "No" }).click();
  expect(enviado[880002]).toEqual({ answer: "no" });
  await expect(page.locator("section.pu-permiso")).toHaveCount(0);
  expect(problemas).toEqual([]);
});
