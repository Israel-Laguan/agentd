import { expect, test } from "@playwright/test";
import { seedProject, taskEventTypes, uniqueTag } from "./helpers";

// J06's gap: the vitest tests render the drawer in jsdom, which has no layout
// and no navigation. Here a task seeded through the daemon shows up on the
// board of the real stack, and opening it lists the events the daemon wrote.
test("a seeded task opens in the drawer with its event log", async ({ page, request }) => {
  const tag = uniqueTag("drawer");
  const title = `Drawer task ${tag}`;
  const { taskId } = await seedProject(request, `Drawer project ${tag}`, title);

  // The worker claims the task and writes its first events on its own; wait
  // for the daemon to have them so the drawer has something to render.
  await expect
    .poll(() => taskEventTypes(request, taskId), { timeout: 30_000 })
    .toContain("LOG_CHUNK");

  await page.goto("/");
  await page.getByRole("button", { name: "Workforce Board" }).click();

  const card = page.getByRole("heading", { name: title });
  await expect(card).toBeVisible();
  await card.click();

  await expect(page.getByRole("heading", { name: "Task Details" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Task events" })).toBeVisible();
  // Each event is a collapsed <details>; the payload is the daemon's own log
  // line, which names the task, so static text cannot satisfy this.
  const logRow = page.locator("details").filter({ hasText: "LOG_CHUNK" });
  await logRow.getByText("LOG_CHUNK", { exact: true }).click();
  await expect(logRow.getByText(new RegExp(`title=${title}`))).toBeVisible();

  await page.getByRole("button", { name: "Close task drawer" }).click();
  await expect(page.getByRole("heading", { name: "Task Details" })).toBeHidden();
});
