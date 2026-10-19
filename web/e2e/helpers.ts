import { APIRequestContext, expect } from "@playwright/test";
import { randomUUID } from "node:crypto";

// The daemon the browser's page talks to; the tests seed and read through it
// directly. Playwright's request context sends no Origin header, so the
// daemon's write guard treats it as a non-browser client and needs no token
// (the devenv config sets none).
export const API = process.env.PLAYWRIGHT_API_URL ?? "http://localhost:8765";

// A unique, readable suffix so a test finds exactly its own rows on a stack
// that earlier runs have already filled.
export const uniqueTag = (prefix: string) => `${prefix}-${randomUUID().slice(0, 8)}`;

export interface SeededProject {
  projectId: string;
  taskId: string;
}

export async function seedProject(
  request: APIRequestContext,
  name: string,
  taskTitle: string
): Promise<SeededProject> {
  const res = await request.post(`${API}/api/v1/projects/materialize`, {
    data: {
      project_name: name,
      description: "seeded by a Playwright flow",
      start_empty_workspace: true,
      tasks: [{ title: taskTitle, description: "list the workspace", temp_id: "t1" }],
    },
  });
  expect(res.ok(), `materialize: ${res.status()} ${await res.text()}`).toBeTruthy();
  const { data } = await res.json();
  return { projectId: data.project.id, taskId: data.tasks[0].id };
}

export async function listProjects(
  request: APIRequestContext
): Promise<{ id: string; name: string }[]> {
  const res = await request.get(`${API}/api/v1/projects`);
  expect(res.ok()).toBeTruthy();
  return (await res.json()).data;
}

export async function taskEventTypes(
  request: APIRequestContext,
  taskId: string
): Promise<string[]> {
  const res = await request.get(`${API}/api/v1/tasks/${taskId}/events`);
  expect(res.ok()).toBeTruthy();
  return ((await res.json()).data as { type: string }[]).map((e) => e.type);
}
