// The browser tests: the real UI and API (hack/interfaz), with an empty
// database and a fake GitHub. hack/ci-interfaz.sh starts the server and
// runs them on every push; INTERFAZ_URL points them elsewhere.
import { defineConfig } from "@playwright/test";
import { fileURLToPath } from "node:url";

export default defineConfig({
  testDir: ".",
  outputDir: "resultados",
  // Generous: on a busy cluster a page can take long to come, and a wait
  // that runs out fails a test that was right (runs 1263, 1275 and 1276).
  // No retries: the tests share one database, and a retried one would find
  // what its first try made.
  timeout: 120_000,
  expect: { timeout: 30_000 },
  // One server and one database: the tests run one after another, in order.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  reporter: [["list"]],
  use: {
    baseURL: process.env.INTERFAZ_URL ?? "http://127.0.0.1:8090",
    storageState: fileURLToPath(new URL(".estado.json", import.meta.url)),
  },
  // English and Spanish: the language follows the browser's.
  projects: [
    { name: "en", use: { locale: "en-US" } },
    { name: "es", use: { locale: "es-MX" } },
  ],
});
