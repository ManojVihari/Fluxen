import { test, expect, type Page } from "@playwright/test";

// Real credentials for the org already created in this environment.
// Overridable via env for other environments/CI.
const EMAIL = process.env.E2E_EMAIL ?? "owner@example.com";
const PASSWORD = process.env.E2E_PASSWORD ?? "verify-pw-12345";

async function login(page: Page) {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: /sign in to fluxen/i })).toBeVisible();
  await page.getByLabel(/email/i).fill(EMAIL);
  await page.getByLabel(/password/i).fill(PASSWORD);
  await page.getByRole("button", { name: /sign in/i }).click();
  // The authenticated shell (TopNav) is the reliable "we're in" signal —
  // "/" itself renders Overview, Applications list, or a loading state
  // depending on data, but the nav bar is present in all of them.
  await expect(page.getByRole("link", { name: "Overview" })).toBeVisible({ timeout: 10_000 });
}

test.describe("golden path: a real browser driving the actual dashboard", () => {
  test("logs in successfully with valid credentials", async ({ page }) => {
    await login(page);
    await expect(page).toHaveURL(/\/$/);
  });

  test("rejects an invalid password with a visible error, no session created", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel(/email/i).fill(EMAIL);
    await page.getByLabel(/password/i).fill("definitely-wrong");
    await page.getByRole("button", { name: /sign in/i }).click();

    await expect(page.getByText(/invalid email or password/i)).toBeVisible();
    await expect(page).toHaveURL(/\/login$/);
  });

  test("creates an application and reaches the connect screen with a real, usable API key", async ({ page }) => {
    await login(page);

    await page.goto("/applications");
    await expect(page.getByRole("heading", { name: "Applications" })).toBeVisible();

    const appName = `E2E App ${Date.now()}`;
    await page.getByRole("link", { name: /new application/i }).click();
    await expect(page).toHaveURL(/\/applications\/new$/);
    await page.getByLabel(/name/i).fill(appName);
    await page.getByRole("button", { name: /create application/i }).click();

    // Creating an application redirects straight to its connect screen.
    await expect(page).toHaveURL(/\/applications\/[^/]+\/connect$/, { timeout: 10_000 });

    await page.getByRole("button", { name: /generate api key/i }).click();
    await expect(page.getByText(/shown only once/i)).toBeVisible();

    // The key itself renders in a code block starting with the known
    // prefix — proving the full setup → login → create-app → issue-key
    // path works end to end through the real rendered UI, not just the
    // API underneath it.
    await expect(page.getByText(/^fx_live_/)).toBeVisible();
  });

  test("onboarding wizard renders its three real, state-driven steps", async ({ page }) => {
    await login(page);

    await page.goto("/onboarding");
    await expect(page.getByRole("heading", { name: /get fluxen running/i })).toBeVisible();
    await expect(page.getByText(/configure a provider/i)).toBeVisible();
    await expect(page.getByText(/create your first application/i)).toBeVisible();
    await expect(page.getByText(/^connect it$/i)).toBeVisible();
  });

  test("searches applications and archives/unarchives one via the real UI", async ({ page }) => {
    await login(page);

    const appName = `E2E Archive Target ${Date.now()}`;
    await page.goto("/applications/new");
    await page.getByLabel(/name/i).fill(appName);
    await page.getByRole("button", { name: /create application/i }).click();
    await expect(page).toHaveURL(/\/applications\/[^/]+\/connect$/, { timeout: 10_000 });

    await page.goto("/applications");
    await page.getByPlaceholder(/search applications/i).fill(appName);
    const row = page.getByRole("row", { name: new RegExp(appName) });
    await expect(row).toBeVisible();

    await row.getByRole("button", { name: "Archive" }).click();
    // Archived rows are hidden by default — the search box's own filter
    // stays applied, so the row disappearing (not just an empty list) is
    // the real signal here.
    await expect(row).not.toBeVisible();

    await page.getByLabel(/show archived/i).check();
    const archivedRow = page.getByRole("row", { name: new RegExp(appName) });
    await expect(archivedRow).toBeVisible();
    await expect(archivedRow.getByText("archived", { exact: true })).toBeVisible();

    await archivedRow.getByRole("button", { name: "Unarchive" }).click();
    await expect(archivedRow.getByText("archived", { exact: true })).not.toBeVisible();
  });

  test("settings > providers page lists all three providers", async ({ page }) => {
    await login(page);

    await page.goto("/settings/providers");
    await expect(page.getByText("OpenAI", { exact: true })).toBeVisible();
    await expect(page.getByText("Google Gemini", { exact: true })).toBeVisible();
    await expect(page.getByText("Ollama", { exact: true })).toBeVisible();
  });
});
