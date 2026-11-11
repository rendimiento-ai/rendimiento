import { test, expect } from "./vigia";

// Nobody is signed in here: these are the pages a person sees before that.
test.use({ storageState: { cookies: [], origins: [] } });

// GitHub brings the person back to the start once they chose the
// repositories of the GitHub App (its setup URL). Signing in is what
// follows, and it starts without asking; any other visit gets the welcome.
test("back from GitHub, signing in starts by itself", async ({ page, problemas }) => {
  // The sign-in goes on to GitHub; the test stops at rendimiento's door.
  await page.route("**/api/auth/login", (ruta) => ruta.fulfill({ status: 200, contentType: "text/plain", body: "sign-in" }));
  await page.goto("/");
  await expect(page.locator('a[href="/api/auth/login"]').first(), "the welcome asks to sign in").toBeVisible();
  await page.goto("/?installation_id=1&setup_action=install");
  await expect(page).toHaveURL(/\/api\/auth\/login$/);
  expect(problemas).toEqual([]);
});
