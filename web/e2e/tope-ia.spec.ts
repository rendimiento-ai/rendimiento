import { test, expect } from "./vigia";

// What the platform's key may spend a month in all (AI_PLATFORM_CAP), on the
// AI page: an allowance that does not fit in it raises it, the total is
// changed there too, and a total below the allowances says what it does.
test("the platform's AI total follows the allowances", async ({ page }, info) => {
  const es = info.project.name === "es";
  // From the start: the test server's $5 total (no AI_PLATFORM_CAP), and no allowance for the platform's organization.
  expect((await page.request.put("/api/ia/cap", { data: { dollars: 5 } })).ok()).toBeTruthy();
  expect((await page.request.put("/api/ia/orgs/1/allowance", { data: { dollars: 0 } })).ok()).toBeTruthy();
  const vista = (await (await page.request.get("/api/ia/orgs")).json()) as { orgs: { org: { id: number; name: string } }[]; allowancesMicros: number };
  const nombre = vista.orgs.find((o) => o.org.id === 1)?.org.name ?? "";
  // Other organizations' allowances count too.
  const esperado = Math.max(5, vista.allowancesMicros / 1e6 + 15);
  const pesos = (n: number) => `$${n.toFixed(2)}`;

  await page.goto("/ia");
  const total = page.locator("#ia-total");
  await expect(total).toHaveValue("5");

  // 15 for the platform's organization: past the $5 total, which goes up with it.
  const fila = page.locator("tr").filter({ hasText: nombre });
  await fila.locator("input").fill("15");
  await fila.getByRole("button", { name: es ? "Guardar" : "Save" }).click();
  await expect(page.locator(".ia-total-subio")).toContainText(es
    ? `El total subió a ${pesos(esperado)} al mes, para que quepan todas las asignaciones.`
    : `The total went up to ${pesos(esperado)} a month, so every allowance fits.`);
  await expect(total).toHaveValue(String(esperado));
  await expect(page.getByText(es ? "Guardado aquí: manda sobre AI_PLATFORM_CAP." : "Saved here: it wins over AI_PLATFORM_CAP.")).toBeVisible();

  // The total by hand, below the allowances: allowed, and said.
  await total.fill("3");
  await page.locator(".ia-total").getByRole("button", { name: es ? "Guardar" : "Save" }).click();
  await expect(page.locator(".ia-total-subio")).toHaveCount(0);
  await expect(page.locator(".ia-total-corto")).toContainText(es
    ? `más que el total de ${pesos(3)}`
    : `more than the total of ${pesos(3)}`);

  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);

  // As it was, for the tests after this one.
  expect((await page.request.put("/api/ia/orgs/1/allowance", { data: { dollars: 0 } })).ok()).toBeTruthy();
  expect((await page.request.put("/api/ia/cap", { data: { dollars: 5 } })).ok()).toBeTruthy();
});
