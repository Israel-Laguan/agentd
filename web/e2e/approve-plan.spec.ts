import { expect, test } from "@playwright/test";
import { listProjects, uniqueTag } from "./helpers";

// B-069: EXECUTE STRATEGY used to stay clickable while the approval was in
// flight, so a double click materialized the plan twice. The mock LLM turns
// "Build a <thing>" into a deterministic two-task plan that echoes the prompt,
// so the unique tag makes the project name unique too.
test("a double click on EXECUTE STRATEGY creates one project", async ({ page, request }) => {
  const tag = uniqueTag("approve");
  const materializes: string[] = [];
  page.on("request", (req) => {
    if (req.method() === "POST" && req.url().endsWith("/api/v1/projects/materialize")) {
      materializes.push(req.url());
    }
  });

  await page.goto("/");
  await page.getByLabel("Chat input").fill(`Build a todo app ${tag}`);
  await page.getByRole("button", { name: "Send message" }).click();

  await expect(page.getByRole("heading", { name: "Workforce Allocation Plan" })).toBeVisible();
  await page.getByRole("button", { name: "EXECUTE STRATEGY" }).dblclick();

  // Approval moves the user to the board with the new tasks on it.
  await expect(page.getByRole("heading", { name: `Set up todo app ${tag}` })).toBeVisible();

  expect(materializes).toHaveLength(1);
  const mine = (await listProjects(request)).filter((p) => p.name.includes(tag));
  expect(mine).toHaveLength(1);
});
