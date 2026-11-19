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

// The welcome offers each forja that is connected, and GitHub only when it
// is; with none connected it still offers GitHub, whose sign-in leads to the
// first-run setup, so the page never ends with no way in.
test("the welcome offers the connected forjas, and always a way in", async ({ page, problemas }) => {
  const gitlab = { forja: "gitlab", id: 7, host: "git.example.com" };
  const bitbucket = { forja: "bitbucket", id: 8, host: "equipo-ejemplo" };
  const casos: { github: boolean; signIn: object[]; ve: string[] }[] = [
    { github: true, signIn: [], ve: ["/api/auth/login"] },
    { github: true, signIn: [gitlab, bitbucket], ve: ["/api/auth/login", "/api/auth/gitlab/7", "/api/auth/bitbucket/8"] },
    { github: false, signIn: [gitlab], ve: ["/api/auth/gitlab/7"] },
    { github: false, signIn: [], ve: ["/api/auth/login"] },
  ];
  for (const c of casos) {
    await page.unroute("**/api/setup/status");
    await page.route("**/api/setup/status", async (r) => {
      const res = await r.fetch();
      await r.fulfill({ response: res, json: { ...(await res.json()), configured: true, github: c.github, signIn: c.signIn } });
    });
    await page.goto("/");
    const acciones = page.locator(".bienv-heroe .bienv-acciones a.btn[href^='/api/auth/']");
    await expect(acciones, JSON.stringify(c)).toHaveCount(c.ve.length);
    expect(await acciones.evaluateAll((els) => els.map((el) => el.getAttribute("href")))).toEqual(c.ve);
    // The closing section offers the same, and the top bar's Sign in goes to the first one.
    await expect(page.locator(".bienv-cierre a.btn[href^='/api/auth/']")).toHaveCount(c.ve.length);
    await expect(page.locator(".bienv-barra a.btn.primary")).toHaveAttribute("href", c.ve[0]);
  }
  expect(problemas).toEqual([]);
});
