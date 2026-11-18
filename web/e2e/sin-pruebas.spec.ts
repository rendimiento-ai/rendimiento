import { test, expect } from "./vigia";

// A branch's run that passed, though no step builds or tests part of its
// change: the page says which paths, and that a person must merge it. The
// test server runs nothing, so the run is the browser's own.
const id = 990101;
const hace = (s: number) => new Date(Date.now() - s * 1000).toISOString();
const ejecucion = {
  id, appId: 1, sha: "0123456789abcdef", branch: "renovate/react-native-0.x", event: "pull_request", deploy: false,
  status: "succeeded", message: "", createdAt: hace(300), startedAt: hace(300), finishedAt: hace(100),
  untested: ["mobile/", "scripts/"],
  steps: [{ id: "web:build", service: "web", kind: "build", dependsOn: [], status: "succeeded", startedAt: hace(300), finishedAt: hace(100) }],
};

test("a run that passed with paths no step tests says a person must merge it", async ({ page, problemas }, info) => {
  const es = info.project.name === "es";
  await page.route(`**/api/runs/${id}`, (r) => r.fulfill({ json: ejecucion }));
  await page.route(`**/api/runs/${id}/events`, (r) => r.fulfill({ status: 200, contentType: "text/event-stream", body: ": nada\n\n" }));
  await page.route(`**/api/runs/${id}/steps/*/log`, (r) => r.fulfill({ contentType: "text/plain", body: "built\n" }));
  await page.goto(`/apps/sin-pruebas/runs/${id}`);

  const aviso = page.locator("#untested");
  await expect(aviso).toContainText(es ? "Pasó, pero ningún paso probó todo el cambio" : "It passed, but no step tested all of it");
  await expect(aviso).toContainText(es ? "debe integrarla una persona" : "a person must merge this");
  await expect(aviso.locator("li")).toHaveText(["mobile/", "scripts/"]);
  expect(problemas).toEqual([]);
});
