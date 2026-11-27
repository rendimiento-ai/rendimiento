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
    { ...paso("api:build", "build", [], "succeeded", 400, 200), lugares: [{ nodo: "node-a", arch: "arm64" }, { nodo: "node-b", arch: "amd64" }] },
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
  await expect(pasos.locator('.carrera-carril[aria-label^="web:build,"]')).toContainText(es ? "1.º" : "1st");
  await expect(pasos.locator(".carrera-carril.vivo")).toHaveCount(1);
  // Where a step ran: each half of the image with its node.
  const lugar = pasos.locator('.carrera-carril[aria-label^="api:build,"] .carrera-lugar');
  await expect(lugar.locator(".carrera-nodo")).toHaveText(["node-aarm64", "node-bamd64"]);
  await expect(lugar).toHaveAttribute("title", es ? "Corrió en node-a (arm64) + node-b (amd64)" : "Ran on node-a (arm64) + node-b (amd64)");

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

// Live: two steps run side by side; the stream then says they finished (one
// as an older server says it, without times; one with them) and that the
// deploy started. Without a reload, the finished lanes show their place and
// stop, the deploy's runs; the last run that passed marks where each step
// finished then.
test("a run's steps follow the live events: a finished step stops", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  const vivo = 990002, previa = 990001 - 1;
  const lanes = (pasos: ReturnType<typeof paso>[]) => ({ ...ejecucion, id: vivo, branch: "main", deploy: true, status: "running", steps: pasos });
  let servida = lanes([
    paso("api:test", "test", [], "running", 90),
    paso("api:build", "build", [], "running", 90),
    paso("deploy", "task", ["api:test", "api:build"], "pending"),
  ]);
  let soltar = () => {};
  const suelto = new Promise<void>((r) => { soltar = r; });
  await page.route(`**/api/runs/${vivo}`, (r) => r.fulfill({ json: servida }));
  await page.route(`**/api/apps/en-vivo/runs`, (r) => r.fulfill({ json: [{ ...ejecucion, id: previa, status: "succeeded", steps: undefined }] }));
  await page.route(`**/api/runs/${previa}`, (r) => r.fulfill({ json: { ...ejecucion, id: previa, status: "succeeded", steps: [
    paso("api:test", "test", [], "succeeded", 900, 800),
    paso("api:build", "build", [], "succeeded", 900, 840),
    paso("deploy", "task", ["api:test", "api:build"], "succeeded", 800, 700),
  ] } }));
  await page.route(`**/api/runs/${vivo}/steps/*/log`, (r) => r.fulfill({ contentType: "text/plain", body: "" }));
  await page.route(`**/api/runs/${vivo}/events`, async (r) => {
    await suelto; // the events come once the page shows the run
    const ev = (data: object) => `event: step\ndata: ${JSON.stringify(data)}\n\n`;
    await r.fulfill({ status: 200, contentType: "text/event-stream", body: "retry: 600000\n\n" +
      ev({ id: "api:test", status: "succeeded", digest: "", message: "" }) +
      ev({ id: "api:build", status: "succeeded", digest: "", message: "", startedAt: hace(90), finishedAt: hace(30) }) +
      ev({ id: "deploy", status: "running", digest: "", message: "", startedAt: hace(0) }) });
  });
  await page.goto(`/apps/en-vivo/runs/${vivo}`);

  const pasos = page.locator("section.pasos");
  const carril = (id: string) => pasos.locator(`.carrera-carril[aria-label^="${id},"]`);
  await expect(pasos.locator(".carrera-carril")).toHaveCount(3);
  await expect(pasos.locator(".carrera-carril.vivo")).toHaveCount(2);
  await expect(pasos.locator(".carrera-antes")).toHaveCount(3);

  // What the server says from now on, should the page fetch the run again.
  servida = lanes([
    paso("api:test", "test", [], "succeeded", 90, 0),
    paso("api:build", "build", [], "succeeded", 90, 30),
    paso("deploy", "task", ["api:test", "api:build"], "running", 0),
  ]);
  soltar();
  await expect(carril("deploy")).toHaveClass(/\bvivo\b/);
  await expect(pasos.locator(".carrera-carril.vivo")).toHaveCount(1);
  await expect(carril("api:build")).toContainText(es ? "1.º" : "1st");
  await expect(carril("api:test")).toContainText(es ? "2.º" : "2nd");
  await expect(carril("deploy").locator(".carrera-tramo")).toHaveCount(1);

  // A finished step's time stands still while the deploy's goes on.
  const antes = await carril("api:test").getAttribute("aria-label");
  const deployAntes = await carril("deploy").getAttribute("aria-label");
  await page.waitForTimeout(2200);
  expect(await carril("api:test").getAttribute("aria-label")).toBe(antes);
  expect(await carril("deploy").getAttribute("aria-label")).not.toBe(deployAntes);

  // The threads say the same: one thread running.
  await pasos.getByRole("button", { name: es ? "Los hilos" : "The threads" }).click();
  await expect(pasos.locator("g.hilos-paso")).toHaveCount(3);
  await expect(pasos.locator("g.hilos-paso.vivo")).toHaveCount(1);
  await expect(pasos.locator("g.hilos-paso.vivo")).toHaveAttribute("aria-label", /^deploy,/);
  await expect(pasos.locator(".hilos-avanza")).toHaveCount(1);
  expect(problemas).toEqual([]);
});
