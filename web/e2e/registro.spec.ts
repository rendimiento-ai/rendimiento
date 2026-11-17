import { test, expect } from "./vigia";

// The Log page: the lines the test server wrote (hack/interfaz logs that
// it is listening, with its address), newest first; a line opens to its
// attributes, and the filters narrow the list.
test("the log shows the platform's lines, and its filters work", async ({ page }, info) => {
  const es = info.project.name === "es";
  // Reached from the Problems page.
  await page.goto("/problems");
  await page.locator("a.registro-enlace").click();
  await expect(page).toHaveURL(/\/registro$/);
  await expect(page.locator("main h1").first()).toHaveText(es ? "Registro" : "Log");

  const tabla = page.locator("table.registro-tabla");
  await expect(tabla.locator("tr.registro-fila").first()).toBeVisible();

  // By now the test server has written hundreds of lines, and the page shows
  // the newest hundred: the search finds the first one it wrote.
  const buscar = page.getByRole("searchbox", { name: es ? "Buscar en los mensajes" : "Search the messages" });
  await buscar.fill("nothing-logs-this-zzz");
  await expect(page.locator(".card.empty")).toBeVisible();
  await buscar.fill("is listening");
  const linea = tabla.locator("tr.registro-fila", { hasText: "is listening" }).first();
  await expect(linea).toBeVisible();
  await expect(linea).toContainText("interfaz");

  // It opens to its attributes.
  await linea.locator("button.registro-abrir").click();
  await expect(page.locator(".registro-attrs").first()).toContainText("addr");

  // Errors only: the INFO line is gone.
  await page.getByLabel(es ? "Nivel" : "Level").selectOption("ERROR");
  await expect(tabla.locator("tr.registro-fila", { hasText: "is listening" })).toHaveCount(0);
  await page.getByLabel(es ? "Nivel" : "Level").selectOption("");
  await expect(linea).toBeVisible();
});
