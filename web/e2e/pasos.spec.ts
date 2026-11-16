import type { Page } from "@playwright/test";
import { test, expect } from "./vigia";

// A run's steps, the race and the threads. The test server runs nothing,
// so the run is the browser's own: three steps side by side, a deploy that
// waits for them, a check that runs now and a notice still to come.
const id = 990001;
const hace = (s: number) => new Date(Date.now() - s * 1000).toISOString();
const paso = (id: string, kind: string, dependsOn: string[], status: string, desde?: number, hasta?: number) => ({
  id, service: id.split(":")[0], kind, dependsOn, status,
  ...(desde !== undefined ? { startedAt: hace(desde) } : {}),
  ...(hasta !== undefined ? { finishedAt: hace(hasta) } : {}),
});
const ejecucion = {
  id, appId: 1, sha: "0123456789abcdef", branch: "main", event: "push", deploy: true, status: "running", message: "",
  createdAt: hace(400), startedAt: hace(400),
  steps: [
    paso("api:test", "test", [], "succeeded", 400, 300),
    paso("api:build", "build", [], "succeeded", 400, 200),
    paso("web:build", "build", [], "succeeded", 400, 340),
    paso("deploy", "task", ["api:test", "api:build", "web:build"], "succeeded", 200, 60),
    paso("verify", "task", ["deploy"], "running", 60),
    paso("notify", "task", ["verify"], "pending"),
  ],
};

async function abrir(page: Page) {
  await page.route(`**/api/runs/${id}`, (r) => r.fulfill({ json: ejecucion }));
  await page.route(`**/api/runs/${id}/events`, (r) => r.fulfill({ status: 200, contentType: "text/event-stream", body: ": nada\n\n" }));
  await page.route(`**/api/runs/${id}/steps/*/log`, (r) => r.fulfill({ contentType: "text/plain", body: `log of ${decodeURIComponent(r.request().url().split("/steps/")[1].split("/")[0])}\n` }));
  await page.goto(`/apps/una-carrera/runs/${id}`);
}

test("a run's steps: the race, then the threads, and a step's log", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await abrir(page);
  const pasos = page.locator("section.pasos");
  // The race first: a lane per step, the place it arrived in.
  await expect(pasos.getByRole("button", { name: es ? "La carrera" : "The race" })).toHaveAttribute("aria-pressed", "true");
  await expect(pasos.locator(".carrera-carril")).toHaveCount(6);
  await expect(pasos.locator(".carrera-carril", { hasText: "web:build" })).toContainText(es ? "1.º" : "1st");
  await expect(pasos.locator(".carrera-carril.vivo")).toHaveCount(1);

  // The threads: three leave from the change, and three steps start at a knot.
  await pasos.getByRole("button", { name: es ? "Los hilos" : "The threads" }).click();
  await expect(pasos.locator("g.hilos-paso")).toHaveCount(6);
  await expect(pasos.locator(".hilos-nudo")).toHaveCount(3);

  // A step chosen with the mouse, another with the keyboard: its log shows.
  const registro = page.locator(".ejecucion-registro");
  await pasos.getByRole("button", { name: /^api:build,/ }).click();
  await expect(pasos.getByRole("button", { name: /^api:build,/ })).toHaveAttribute("aria-current", "true");
  await expect(registro).toContainText("log of api:build");
  await pasos.getByRole("button", { name: /^deploy,/ }).focus();
  await page.keyboard.press("Enter");
  await expect(registro).toContainText("log of deploy");

  // The browser remembers the threads; on a phone only their box scrolls sideways.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await expect(pasos.getByRole("button", { name: es ? "Los hilos" : "The threads" })).toHaveAttribute("aria-pressed", "true");
  await expect(pasos.locator("g.hilos-paso")).toHaveCount(6);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  expect(problemas).toEqual([]);
});
