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
