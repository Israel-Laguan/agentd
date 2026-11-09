import { expect, test } from "@playwright/test";
import { seedProject, taskEventTypes, uniqueTag } from "./helpers";

// J06's gap: the vitest tests render the drawer in jsdom, which has no layout
// and no navigation. Here a task seeded through the daemon shows up on the
// board of the real stack, and opening it lists the events the daemon wrote.
test("a seeded task opens in the drawer with its event log", async ({ page, request }) => {
  const tag = uniqueTag("drawer");
  const title = `Drawer task ${tag}`;
  // exec.turn runs on the devenv's fake acp agent, which reads its script from
  // the task description: a completed edit, then a summary that names the task.
  const summary = `done title=${title}`;
  const script = JSON.stringify([
    { did: "edit" },
    {
      tool: {
        name: "report_outcome",
        args: { outcome: "completed", summary, result: { summary } },
      },
    },
  ]);
  const { taskId } = await seedProject(request, `Drawer project ${tag}`, title, script);

  // The worker claims the task and writes its result on its own; wait for the
  // daemon to have it so the drawer has something to render.
  await expect
    .poll(() => taskEventTypes(request, taskId), { timeout: 30_000 })
    .toContain("RESULT");

  await page.goto("/");
  await page.getByRole("button", { name: "Workforce Board" }).click();

  const card = page.getByRole("heading", { name: title });
  await expect(card).toBeVisible();
  await card.click();

  await expect(page.getByRole("heading", { name: "Task Details" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Task events" })).toBeVisible();
  // Each event is a collapsed <details>; the payload is the agent's summary,
  // which names the task, so static text cannot satisfy this.
  const resultRow = page.locator("details").filter({ hasText: "RESULT" });
  await resultRow.getByText("RESULT", { exact: true }).click();
  await expect(resultRow.getByText(new RegExp(`title=${title}`))).toBeVisible();

  await page.getByRole("button", { name: "Close task drawer" }).click();
  await expect(page.getByRole("heading", { name: "Task Details" })).toBeHidden();
});
