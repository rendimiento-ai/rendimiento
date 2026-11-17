import { test, expect } from "./vigia";

// The lookout on the Log page. The test server gives it a fake model (no AI
// is called) that words a finding for each of the first three signals it
// reads, the third with no work suggested; and it wrote three repeated
// lines to the log, so there is always something to find. Each project
// (en, es) has its own server and database in CI.
test("the lookout's findings become an encargo, a note in the Ruta, or are set aside", async ({ page }, info) => {
  const es = info.project.name === "es";
  await page.goto("/registro");
  const vigia = page.locator("section.vigia");
  await expect(vigia.locator("h2")).toHaveText(es ? "Lo que vio el vigía" : "What the lookout saw");

  // Review now: three findings, each with its evidence.
  const pedida = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/vigia/revisar");
  await vigia.getByRole("button", { name: es ? "Revisar ahora" : "Review now" }).click();
  expect((await pedida).status()).toBe(202);
  const nuevas = vigia.locator("li.vigia-hallazgo.vigia-nuevo");
  await expect(nuevas).toHaveCount(3);
  await expect(vigia.locator(".vigia-cuando")).toContainText(es ? "lo pidió prueba" : "asked by prueba");
  const [a, b, c] = [nuevas.nth(0), nuevas.nth(1), nuevas.nth(2)];
  const titulo = async (l: typeof a) => (await l.locator(".vigia-titulo").innerText()).trim();
  const [ta, tb, tc] = [await titulo(a), await titulo(b), await titulo(c)];
  const claves = [await a.getAttribute("data-clave"), await b.getAttribute("data-clave"), await c.getAttribute("data-clave")];
  await expect(a.locator(".vigia-evidencia")).toContainText("test evidence");
  // A fact suggests no work: no encargo for it.
  const encargo = es ? "Volverlo encargo" : "Turn it into an encargo";
  await expect(c.getByRole("button", { name: encargo })).toHaveCount(0);
  await expect(a.getByRole("button", { name: encargo })).toBeVisible();

  // The bell says so.
  await page.reload();
  await page.locator("button.campana-boton").click();
  await expect(page.locator(".campana-panel")).toContainText(es ? "El vigía vio 3 cosas esta semana" : "The lookout saw 3 things this week");
  await page.keyboard.press("Escape");

  // Into an encargo, into the Ruta, and not now.
  const porTitulo = (t: string) => vigia.locator("li.vigia-hallazgo", { hasText: t });
  await porTitulo(ta).getByRole("button", { name: encargo }).click();
  await expect(porTitulo(ta)).toHaveClass(/vigia-encargo/);
  await expect(porTitulo(ta).locator(".vigia-decidido a")).toContainText(es ? "Encargo n.º" : "Encargo #");
  await porTitulo(tc).getByRole("button", { name: es ? "Anotarlo en la Ruta" : "Note it in the Ruta" }).click();
  await expect(porTitulo(tc)).toHaveClass(/vigia-ruta/);
  await porTitulo(tb).getByRole("button", { name: es ? "No por ahora" : "Not now" }).click();
  await expect(porTitulo(tb)).toHaveCount(0);
  await expect(vigia).toContainText(es ? "1 para después" : "1 set aside");

  // The encargo carries the evidence and who accepted it; the Ruta entry is a note.
  const encargos = (await (await page.request.get("/api/encargos")).json()) as { title: string; body: string; createdBy: string }[];
  const hecho = encargos.find((e) => e.title === ta);
  expect(hecho?.body).toContain("test evidence");
  expect(hecho?.createdBy).toBe("prueba");
  const ruta = (await (await page.request.get(`/api/ruta?q=${encodeURIComponent(tc)}`)).json()) as { title: string; kind: string; tags: string[] }[];
  expect(ruta.find((e) => e.title === tc)).toMatchObject({ kind: "nota", tags: expect.arrayContaining(["vigia"]) });

  // A second review proposes none of the three again.
  const otra = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/vigia/revisar");
  await vigia.getByRole("button", { name: es ? "Revisar ahora" : "Review now" }).click();
  const { id } = (await (await otra).json()) as { id: number };
  await expect.poll(async () => {
    const v = (await (await page.request.get("/api/vigia")).json()) as { revision?: { id: number; estado: string } };
    return v.revision?.id === id ? v.revision.estado : "";
  }).toBe("hecha");
  const vista = (await (await page.request.get("/api/vigia")).json()) as { revision: { hallazgos: { clave: string }[] } };
  for (const clave of claves) expect(vista.revision.hallazgos.map((h) => h.clave)).not.toContain(clave);
  for (const t of [ta, tb, tc]) await expect(porTitulo(t)).toHaveCount(0);
});
