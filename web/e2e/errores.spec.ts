import { test, expect } from "./vigia";

// An address no page has shows the page that says so, with its way out,
// inside the frame: the top bar stays.
test("an unknown address shows that the page does not exist, and the way out", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.goto("/");
  await expect(page.locator("header.topbar")).toBeVisible();
  const reporte = page.waitForResponse((r) => new URL(r.url()).pathname === "/api/fallas");
  await page.goto("/no-hay-tal-pagina");
  const falla = page.locator('main [data-falla="not_found"]');
  await expect(falla.locator("h1")).toHaveText(es ? "Esta página no existe" : "This page does not exist");
  await expect(page.locator("header.topbar")).toBeVisible();
  await expect(falla.locator(".falla-codigo")).toContainText("404 · /no-hay-tal-pagina");
  expect((await reporte).status(), "it is recorded").toBe(204);
  await falla.getByRole("link", { name: es ? "Ir a Aplicaciones" : "Go to Apps" }).click();
  await expect(page).toHaveURL(/\/$/);
  expect(problemas).toEqual([]);
});

// An app that does not exist is the same page, not a bare error.
test("an app that does not exist shows that the page does not exist", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.goto("/apps/ninguna-app-se-llama-asi");
  const falla = page.locator('main [data-falla="not_found"]');
  await expect(falla.locator("h1")).toHaveText(es ? "Esta página no existe" : "This page does not exist");
  await expect(page.locator("header.topbar")).toBeVisible();
  await expect(falla.getByRole("button", { name: es ? "Atrás" : "Back" })).toBeVisible();
  // A run that is not a number names nothing either.
  await page.goto("/apps/ninguna-app-se-llama-asi/runs/abc");
  await expect(page.locator('main [data-falla="not_found"]')).toBeVisible();
  expect(problemas).toEqual([]);
});
