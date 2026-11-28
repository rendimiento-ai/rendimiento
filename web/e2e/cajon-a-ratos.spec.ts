import { test, expect } from "./vigia";

// The drawer of a node «a ratos» on the Environment page (Ruta pendiente 199): since when it is on, what it did, and its
// switches, each saved as it is pressed. The test server has no cluster, so the page's answers are the browser's own.
const hace = (s: number) => new Date(Date.now() - s * 1000).toISOString();
const nodo = (name: string, arch: string, aRatos = false) => ({
  name, roles: aRatos ? [] : ["worker"], ready: true, arch, os: "Linux", kubelet: "v1.34.1", pods: 4,
  cpuCores: 4, cpuUsed: 1, memBytes: 8 * 1024 ** 3, memUsed: 2 * 1024 ** 3, cpuReserved: 1, memReserved: 1024 ** 3, ...(aRatos ? { aRatos: true } : {}),
});
const informe = {
  generatedAt: hace(5), overall: "ok", summary: "",
  cluster: { distribution: "k3s", name: "k3s", version: "v1.34.1", nodes: 2, nodesReady: 2, pods: 8, namespaces: 4 },
  providers: [], checks: [], problems: [],
  nodes: [nodo("nodo-a", "arm64"), nodo("desktop-node", "amd64", true)],
};
const capacidad = (aRatos: Record<string, unknown>) => ({
  saved: { parallelRuns: 0, runsPerApp: 0, stepCpuMillis: 0, nodesOff: [], testCaches: 0, busyPercent: 0, aRatos },
  defaults: { parallelRuns: 2, runsPerApp: 1, testCaches: 2, nodesOff: [] }, maxWorkers: 8, maxTestCaches: 6, busy: [], defaultBusyPercent: 85,
});

test("a node «a ratos»: its figures and its switches", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.route("**/api/me", async (r) => {
    const res = await r.fetch();
    r.fulfill({ response: res, json: { ...(await res.json()), cluster: true, operator: true } });
  });
  // What else of the cluster the page asks is not there: the routes below, registered later, answer first.
  await page.route(/\/api\/(environment|platform)\//, (r) => r.fulfill({ status: 404, json: { error: "not here" } }));
  await page.route("**/api/environment", (r) => r.fulfill({ json: informe }));
  let guardado: Record<string, unknown> = {};
  const puestos: unknown[] = [];
  await page.route("**/api/environment/capacity", (r) => r.fulfill({ json: capacidad(guardado) }));
  await page.route("**/api/environment/a-ratos/*", (r) => {
    const cuerpo = r.request().postDataJSON();
    puestos.push(cuerpo);
    guardado = { "desktop-node": cuerpo };
    return r.fulfill({ json: capacidad(guardado) });
  });
  await page.route("**/api/environment/nodes/*/cifras", (r) => r.fulfill({
    json: { nodo: "desktop-node", prendidaDesde: hace(3600), horasSemana: 9, pasosHoy: 6, imagenesHoy: 4, pasosSemana: 20, fallasSemana: 2, minutosSemana: 80, apps: [] },
  }));
  await page.goto("/environment");

  await page.getByRole("button", { name: /desktop-node/ }).click();
  const cajon = page.locator(".cajon-a-ratos");
  await expect(cajon.locator(".cajon-cifra")).toHaveText([
    new RegExp(es ? "^prendida desde las " : "^on since "),
    es ? "hoy: 6 pasos · 4 imágenes" : "today: 6 steps · 4 images",
    es ? "esta semana: 9 h prendida" : "this week: 9 h on",
  ]);
  // Everything on by default; turning tests off saves just that.
  const pruebas = cajon.getByRole("checkbox", { name: es ? /Correr pasos de prueba/ : /Run test steps/ });
  await expect(pruebas).toBeChecked();
  await pruebas.uncheck();
  await expect.poll(() => puestos).toEqual([{ sinPruebas: true }]);
  await expect(pruebas).not.toBeChecked();
  // Waiting for it while it is off keeps the switch before.
  await cajon.getByRole("radio", { name: es ? /Esperar a que se prenda/ : /Wait for it to come on/ }).check();
  await expect.poll(() => puestos.at(-1)).toEqual({ sinPruebas: true, esperar: true });
  expect(problemas).toEqual([]);
});
