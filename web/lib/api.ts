import { mockBoard } from "@/lib/mocks/board.mock";
import { mockWorkforce } from "@/lib/mocks/workforce.mock";
import { mockProviders } from "@/lib/mocks/providers.mock";
import {
  Provider,
  Task,
  WorkforceState,
  SystemStatus,
} from "@/lib/types";
import { mockSystemStatus } from "@/lib/mocks/system.mock";
import { unwrapData, mapDaemonTask } from "@/lib/mappers";
import { API, USE_MOCK } from "@/lib/api-config";

export { sendChat, postApprovePlan } from "@/lib/api-chat";
export { fetchTaskComments, updateTask, addTaskComment } from "@/lib/api-tasks";
export type { ChatSettings } from "@/lib/api-config";

// ---------------- BOARD ----------------
export async function getBoard(): Promise<{ tasks: Task[] }> {
  if (USE_MOCK) return structuredClone(mockBoard);

  const projectsRes = await fetch(`${API}/api/v1/projects`);
  if (!projectsRes.ok) throw new Error(`HTTP error! status: ${projectsRes.status}`);
  const projects = unwrapData<Record<string, unknown>[]>(await projectsRes.json());

  const taskArrays = await Promise.all(
    projects.map(async (p) => {
      const projectId = (p.ID ?? p.id) as string;
      if (!projectId) return [] as Task[];
      const r = await fetch(`${API}/api/v1/projects/${projectId}/tasks`);
      if (!r.ok) return [] as Task[];
      return unwrapData<Record<string, unknown>[]>(await r.json()).map(mapDaemonTask);
    })
  );
  return { tasks: taskArrays.flat() };
}

// ---------------- SYSTEM STATUS ----------------
export async function getSystemStatus(): Promise<SystemStatus> {
  if (USE_MOCK) return structuredClone(mockSystemStatus);

  const res = await fetch(`${API}/api/v1/system/status`);
  if (!res.ok) throw new Error(`HTTP error! status: ${res.status}`);
  const raw = unwrapData<Record<string, unknown>>(await res.json());
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

  const res = await fetch(`${API}/api/v1/gateway/providers`);
  if (!res.ok) throw new Error(`HTTP error! status: ${res.status}`);
  const envelope = await res.json();
  return envelope.data as Provider[];
}
