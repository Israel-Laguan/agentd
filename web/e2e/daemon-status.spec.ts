import { expect, test } from "@playwright/test";
import { API } from "./helpers";

// B-072: the header and footer used to print fixed text ("Daemon Online")
// whatever the daemon was doing. They now follow what /system/status says, so
// this flow fails when the daemon is down: the header reads "Daemon
// unreachable" instead.
test("the header and footer report the daemon's real state", async ({ page, request }) => {
  const res = await request.get(`${API}/api/v1/system/status`);
  expect(res.ok()).toBeTruthy();
  const { breaker } = (await res.json()).data as { breaker: { state: string } };
  const footerLabel =
    breaker.state === "OPEN" ? "PAUSED: BREAKER OPEN" : breaker.state === "HALF_OPEN" ? "RECOVERING" : "RUNNING";

  await page.goto("/");

  await expect(page.getByText("Daemon online")).toBeVisible();
  await expect(page.getByText("Mock mode")).toHaveCount(0);
  await expect(page.getByRole("contentinfo")).toContainText(footerLabel);
});

// The other half of B-072: a header that always said "online" would pass the
// test above. With the board's project list failing at the network, the header
// must say so.
test("the header reports the daemon unreachable when the board cannot load", async ({ page }) => {
  await page.route(/\/api\/v1\/projects(\?.*)?$/, (route) => route.abort());

  await page.goto("/");

  await expect(page.getByText("Daemon unreachable")).toBeVisible();
  await expect(page.getByText("Daemon online")).toHaveCount(0);
});
