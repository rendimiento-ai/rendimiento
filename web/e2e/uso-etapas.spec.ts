import { test, expect } from "./vigia";

// The interface's use by stages (decision 196), on a phone (390 px): the Uso page draws a flag for each release that
// changed the interface over the days' views; the latest stage is compared before → after, with a shorter way in
// green; pressing another flag compares that one, and a stage with few days after it says so. The stages are the
// browser's own: the test server builds no releases of itself.
const dia = (n: number) => new Date(Date.now() - n * 86400000).toISOString().slice(0, 10);
const medidas = (vistasDia: number, paginas: number, ms: number, seccion?: string) => ({ vistasDia, paginasPorCorrida: paginas, msDatos: ms, seccion, seccionParte: seccion ? 0.6 : undefined });

test("the stages over the interface's use", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.setViewportSize({ width: 390, height: 844 });
  await page.route("**/api/uso/etapas*", (r) => r.fulfill({ json: {
    dias: Array.from({ length: 14 }, (_, i) => ({ dia: dia(13 - i), vistas: 40 + ((i * 7) % 20), medidas: 20, msTotal: 18000 })),
    etapas: [
      { release: 337, at: `${dia(8)}T17:00:00Z`, sha: "a".repeat(40), rama: "ia-en-pestanas", pull: 337, texto: "it is in tabs now.", dias: 7, pocos: false,
        antes: medidas(62, 2.4, 900), despues: medidas(48, 1.6, 850, "open_runs") },
      { release: 340, at: `${dia(1)}T10:00:00Z`, sha: "b".repeat(40), rama: "app-en-pestanas", pull: 338, texto: "its ten tabs are five now.", dias: 1, pocos: true,
        antes: medidas(50, 2, 800, "open_runs"), despues: medidas(45, 1.9, 820, "open_runs") },
    ],
  } }));
  await page.goto("/uso");
  const etapas = page.locator("section.uso-etapas");
  await expect(etapas.locator("h3")).toHaveText(es ? "Etapas" : "Stages");
  await expect(etapas.locator(".etapa-bandera")).toHaveCount(2);
  // The latest first: few days after it, said.
  await expect(etapas.locator(".etapa-pocos")).toBeVisible();
  await etapas.locator(".etapa-bandera", { hasText: "ia-en-pestanas" }).click();
  await expect(etapas).toContainText("it is in tabs now.");
  await expect(etapas.locator(".etapa-cifra").nth(1)).toContainText("2.4");
  await expect(etapas.locator(".etapa-cifra").nth(1).locator(".etapa-cambio.mejor")).toContainText("-33 %");
  await expect(etapas.locator(".etapa-pocos")).toHaveCount(0);
  for (const b of await etapas.locator(".etapa-bandera").all()) expect((await b.boundingBox())!.width).toBeGreaterThanOrEqual(44);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});
