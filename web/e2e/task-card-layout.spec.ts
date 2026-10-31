import { expect, test } from "@playwright/test";
import { seedProject, uniqueTag } from "./helpers";

// The vitest suite renders in jsdom, which has no layout, so it cannot prove
// that the Human action badge and the description stack. This test measures the
// real rendered boxes on the board instead.
test("a human task stacks its description below the action badge", async ({ page, request }) => {
  const tag = uniqueTag("card-layout");
  const title = `Human task ${tag}`;
  const description = `Run the privileged check ${tag}`;
  await seedProject(request, `Layout project ${tag}`, title, description, "HUMAN");

  await page.goto("/");
  await page.getByRole("button", { name: "Workforce Board" }).click();

  const heading = page.getByRole("heading", { name: title });
  await expect(heading).toBeVisible();
  const card = heading.locator("xpath=../../..");
  const badge = card.getByText("Human action", { exact: true });
  const body = card.getByText(description, { exact: true });
  await expect(badge).toBeVisible();
  await expect(body).toBeVisible();

  const badgeBox = await badge.boundingBox();
  const bodyBox = await body.boundingBox();
  expect(badgeBox).not.toBeNull();
  expect(bodyBox).not.toBeNull();
  // Allow a sub-pixel rounding difference; side-by-side would put the body at
  // roughly the badge's own top instead of below its bottom.
  expect(bodyBox!.y).toBeGreaterThanOrEqual(badgeBox!.y + badgeBox!.height - 1);
});
