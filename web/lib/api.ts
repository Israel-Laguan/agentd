import { mockBoard } from "@/lib/mocks/board.mock";
import { mockWorkforce } from "@/lib/mocks/workforce.mock";
import { mockProviders } from "@/lib/mocks/providers.mock";
import {
  BoardSnapshot,
  Provider,
  Task,
  WorkforceState,
  SystemStatus,
} from "@/lib/types";
import { mockSystemStatus } from "@/lib/mocks/system.mock";
import { mapBreaker, mapDaemonTask, mapProviderBreakers } from "@/lib/mappers";
import { requestJSON } from "@/lib/http";
import { API, USE_MOCK } from "@/lib/api-config";

export { sendChat, postApprovePlan } from "@/lib/api-chat";
export { fetchTaskComments, fetchTaskEvents, updateTask, addTaskComment, resolveHumanHandoff } from "@/lib/api-tasks";
export type { ChatSettings } from "@/lib/chat-settings";

// ---------------- BOARD ----------------
// The daemon caps a task page at 200; the board reads up to MAX_TASK_PAGES
// pages per project and flags the project when tasks are left over.
const TASK_PAGE_SIZE = 200;
const MAX_TASK_PAGES = 5;

interface TaskPage {
  data?: Record<string, unknown>[];
  meta?: { total?: number; has_more?: boolean };
}

async function fetchProjectTasks(projectId: string): Promise<{ tasks: Task[]; capped: boolean }> {
  const tasks: Task[] = [];
  for (let page = 0; ; page++) {
    const offset = page * TASK_PAGE_SIZE;
    const body = await requestJSON<TaskPage>(
      `${API}/api/v1/projects/${projectId}/tasks?limit=${TASK_PAGE_SIZE}&offset=${offset}&include_healing=true`,
      { unwrap: false }
    );
    const rows = body.data ?? [];
    tasks.push(...rows.map(mapDaemonTask));
    const more = Boolean(body.meta?.has_more) || (body.meta?.total ?? 0) > offset + rows.length;
    if (!more || rows.length === 0) return { tasks, capped: false };
    if (page + 1 >= MAX_TASK_PAGES) return { tasks, capped: true };
  }
}

// getProjectTasks refetches one project, for a board refresh driven by an SSE
// frame that names it.
export async function getProjectTasks(projectId: string): Promise<{ tasks: Task[]; capped: boolean }> {
  if (USE_MOCK) {
    return { tasks: structuredClone(mockBoard).tasks.filter((t) => t.project_id === projectId), capped: false };
  }
  return fetchProjectTasks(projectId);
}

export async function getBoard(): Promise<BoardSnapshot> {
  if (USE_MOCK) return structuredClone(mockBoard);

  const projects = await requestJSON<Record<string, unknown>[]>(`${API}/api/v1/projects`);
  const snapshot: BoardSnapshot = { tasks: [], failedProjects: [], cappedProjects: [] };

  const results = await Promise.all(
    projects.map(async (p) => {
      const projectId = (p.ID ?? p.id) as string;
      if (!projectId) return null;
      try {
        return { projectId, ...(await fetchProjectTasks(projectId)) };
      } catch (e) {
        return { projectId, error: e instanceof Error ? e.message : String(e) };
      }
    })
  );
  for (const r of results) {
    if (!r) continue;
    if (r.error !== undefined) {
      snapshot.failedProjects.push({ projectId: r.projectId, message: r.error });
      continue;
    }
    snapshot.tasks.push(...r.tasks);
    if (r.capped) snapshot.cappedProjects.push(r.projectId);
  }
  return snapshot;
}

// ---------------- SYSTEM STATUS ----------------
export async function getSystemStatus(): Promise<SystemStatus> {
  if (USE_MOCK) return structuredClone(mockSystemStatus);

  const raw = await requestJSON<Record<string, unknown>>(`${API}/api/v1/system/status`);
  const status = raw.status ?? raw.Status;
  const summary =
    status && typeof status === "object"
      ? (status as Record<string, unknown>).summary ?? (status as Record<string, unknown>).Summary
      : undefined;
  const tasksByState =
    summary && typeof summary === "object"
      ? ((summary as Record<string, unknown>).tasks_by_state ??
          (summary as Record<string, unknown>).TasksByState ??
          {}) as Record<string, number>
      : undefined;

  return {
    total_token_usage: Number(raw.total_token_usage ?? raw.TotalTokenUsage ?? 0),
    rolling_budget_enabled: Boolean(raw.rolling_budget_enabled ?? raw.RollingBudgetEnabled),
    rolling_token_limit: Number(raw.rolling_token_limit ?? raw.RollingTokenLimit ?? 0),
    rolling_token_remaining:
      Number(raw.rolling_token_remaining ?? raw.RollingTokenRemaining ?? 0),
    rolling_token_window: (raw.rolling_token_window ?? raw.RollingTokenWindow) as string | undefined,
    breaker: mapBreaker(raw.breaker),
    provider_breakers: mapProviderBreakers(raw.provider_breakers),
    status:
      status && typeof status === "object"
        ? {
            kind: String((status as Record<string, unknown>).kind ?? (status as Record<string, unknown>).Kind ?? ""),
            message: String(
              (status as Record<string, unknown>).message ?? (status as Record<string, unknown>).Message ?? ""
            ),
            summary: {
              total_projects: Number(
                (summary as Record<string, unknown> | undefined)?.total_projects ??
                  (summary as Record<string, unknown> | undefined)?.TotalProjects ??
                  0
              ),
              tasks_by_state: tasksByState ?? {},
            },
          }
        : undefined,
  };
}

// ---------------- WORKFORCE ----------------
export async function getWorkforce(): Promise<WorkforceState | null> {
  if (USE_MOCK) return mockWorkforce;
  return null;
}

// ---------------- PROVIDERS ----------------
export async function fetchProviders(): Promise<Provider[]> {
  if (USE_MOCK) return mockProviders;

  return requestJSON<Provider[]>(`${API}/api/v1/gateway/providers`);
}
