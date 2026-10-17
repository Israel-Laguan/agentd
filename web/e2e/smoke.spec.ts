import { expect, test } from "@playwright/test";

// SP-025 smoke: proves a real browser can load the board served by the
// running devenv stack, and that client-side tab navigation works. This is
// the seed of the T-040 tier, not a product flow.
test("the board loads from the running stack", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("heading", { name: "agentd" })).toBeVisible();

  await page.getByRole("button", { name: "Workforce Board" }).click();

  await expect(
    page.getByRole("heading", { name: "Active Operation Board" })
  ).toBeVisible();
});
