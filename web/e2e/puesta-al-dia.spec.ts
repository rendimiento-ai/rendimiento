import type { Page } from "@playwright/test";
import { test, expect } from "./vigia";

// Poner al día (decision 191), on a phone (390 px): an app's Caminos tab shows the reports on its dependency updates
// (one with something to change, its three figures and its button; one that needs nothing; one being written);
// «Put the app up to date» sends it and the card then says who works on it; every button is at least 44 px tall and
// nothing is wider than the phone. On the AI page the «Put up to date» card saves what an administrator chose. The
// test server writes no reports, so they are the browser's own; the app is one nueva-app.spec.ts made (this file runs after it).
const hace = (s: number) => new Date(Date.now() - s * 1000).toISOString();
const informe = (id: number, pull: number, titulo: string, extra: object) => ({
  id, pull, titulo, branch: `renovate/${titulo.split(" ")[1]}`, sha: "c".repeat(40), runId: 990300 + id, runFailed: false, falla: [], cambia: [],
  costoMicros: 0, informeMicros: 0, provider: "local", model: "qwen", createdAt: hace(600), updatedAt: hace(300), ...extra,
});

async function unaApp(page: Page, idioma: string): Promise<string> {
  const r = await page.request.get("/api/apps");
  expect(r.ok()).toBeTruthy();
  const app = ((await r.json()) as { name: string }[]).find((a) => a.name.endsWith(`-${idioma}`));
  expect(app, "an app made by nueva-app.spec.ts").toBeTruthy();
  return app!.name;
}

async function alto(page: Page, sel: string, min: number) {
  for (const b of await page.locator(sel).all()) if (await b.isVisible()) expect((await b.boundingBox())!.height).toBeGreaterThanOrEqual(min);
}

test("the reports on an app's dependency updates, and putting it up to date", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const app = await unaApp(page, info.project.name);
  let puesto = 0;
  const vista = () => ({
    ajustes: { encendido: true, quien: "platform", cuando: "toda", sola: false, topeMicros: 250000 },
    informes: [
      informe(3, 57, "Update react to v19", {
        runFailed: true, estado: "listo", riesgo: "medio", dificultad: 3, costoMicros: 180000, resumen: "React 19 quita ReactDOM.render.",
        falla: ["ReactDOM.render ya no existe en react-dom 19"], cambia: [{ archivo: "web/src/main.tsx", mas: 4, menos: 3 }, { archivo: "web/package.json", mas: 2, menos: 2 }],
        ...(puesto ? { encargoId: puesto, encargoStatus: "taken", encargoRoute: "platform", puestoPor: "p0dxD" } : {}),
      }),
      informe(2, 56, "Update zod to v3.24", { estado: "sin_cambios", riesgo: "bajo" }),
      informe(1, 55, "Update vite to v8", { estado: "pendiente", provider: "", model: "" }),
    ],
  });
  await page.route(`**/api/apps/${app}`, async (r) => {
    const res = await r.fetch();
    await r.fulfill({ response: res, json: { ...(await res.json()), role: "admin" } });
  });
  await page.route(`**/api/apps/${app}/al-dia`, (r) => r.fulfill({ json: vista() }));
  await page.route(`**/api/apps/${app}/al-dia/3/poner`, (r) => { puesto = 91; return r.fulfill({ json: { encargo: 91 } }); });
  await page.setViewportSize({ width: 390, height: 844 });

  await page.goto(`/apps/${app}/caminos`);
  const seccion = page.locator("section#al-dia");
  await expect(seccion.locator("h2")).toHaveText(es ? "Al día" : "Up to date");
  const listo = seccion.locator(".al-dia-informe.estado-listo");
  await expect(listo.locator(".al-dia-cifra")).toHaveCount(3);
  await expect(listo.locator(".al-dia-cifra").nth(1)).toContainText(es ? "2 archivos" : "2 files");
  await expect(listo.locator(".al-dia-cifra").nth(2)).toContainText(es ? "medio · $0.18" : "medium · $0.18");
  await expect(seccion.locator(".al-dia-informe.estado-sin_cambios")).toContainText(es ? "Sin cambios" : "No changes");
  await expect(seccion.locator(".al-dia-informe.estado-pendiente")).toContainText(es ? "Leyendo las notas" : "Reading the release notes");
  await listo.locator("summary").click();
  await expect(listo.locator(".al-dia-archivos li")).toHaveCount(2);
  await alto(page, "section#al-dia button, section#al-dia summary", 44);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);

  const boton = listo.getByRole("button", { name: es ? "Poner la app al día · ~$0.18" : "Put the app up to date · ~$0.18" });
  await boton.click();
  await expect(listo.locator(".al-dia-trabaja")).toContainText(es ? "El agente la está poniendo al día" : "The agent is putting it up to date");
  await expect(listo.getByRole("link", { name: es ? "Encargo 91" : "Encargo 91" })).toBeVisible();
  expect(problemas).toEqual([]);
});

test("the «Put up to date» card of the AI page saves what was chosen", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  let guardado: Record<string, unknown> | undefined;
  let ajustes = { encendido: true, quien: "platform", cuando: "toda", sola: false, topeMicros: 250000 };
  await page.route("**/api/ia/al-dia", async (r) => {
    if (r.request().method() === "PUT") {
      guardado = r.request().postDataJSON();
      ajustes = { ...ajustes, ...(guardado as typeof ajustes) };
    }
    await r.fulfill({ json: { ...ajustes, updatedBy: "p0dxD" } });
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/ia#al-dia"); // its drawer, on the Agents tab: other pages link here
  const tarjeta = page.locator("section#al-dia");
  await expect(tarjeta.locator("h2")).toHaveText(es ? "Poner al día" : "Put up to date");
  await tarjeta.getByLabel(es ? "Su agente (un encargo para su Claude)" : "Your own agent (an encargo for your Claude)").check();
  await tarjeta.getByLabel(es ? "Sola cuando el riesgo es bajo y cuesta a lo más" : "By itself when the risk is low and it costs at most").check();
  await tarjeta.getByLabel(es ? "Lo más que puede costar sin preguntar, en dólares" : "The most it may cost without asking, in dollars").fill("0.40");
  await alto(page, "section#al-dia label", 40);
  await tarjeta.getByRole("button", { name: es ? "Guardar" : "Save" }).click();
  await expect(tarjeta.locator(".al-dia-guardado")).toBeVisible();
  expect(guardado).toMatchObject({ quien: "local", cuando: "toda", sola: true, topeMicros: 400000 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});
