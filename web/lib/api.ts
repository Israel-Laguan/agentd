import { mockBoard } from "@/lib/mocks/board.mock";
import { mockWorkforce } from "@/lib/mocks/workforce.mock";
import { mockChat } from "@/lib/mocks/chat.mock";
import { mockApprovePlan } from "@/lib/mocks/plan.mock";
import { mockTaskComments } from "@/lib/mocks/mock-task-comment";
import { mockProviders } from "@/lib/mocks/providers.mock";
import { ChatSettings } from "@/app/components/chat/chat-settings-modal";
import { Provider, Task, TaskComment, ChatResponse, WorkforceState, SystemStatus, DraftPlan } from "@/lib/types";
import { mockSystemStatus } from "@/lib/mocks/system.mock";
import { unwrapData, mapDaemonTask, mapDaemonComment, mapDaemonDraftPlan } from "@/lib/mappers";

// Set NEXT_PUBLIC_USE_MOCK=false to disable mock mode and hit the real daemon.
const USE_MOCK = process.env.NEXT_PUBLIC_USE_MOCK !== "false";
// Set NEXT_PUBLIC_API_URL to override the daemon address (default: http://localhost:8765).
const API = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8765";
// Set NEXT_PUBLIC_MATERIALIZE_TOKEN to match api.materialize_token in the daemon config.
const MATERIALIZE_TOKEN = process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN ?? "";

// OpenAI function-tool definition sent with every chat request so the daemon
// emits tool_calls (create_plan / status_report) when the response is structured.
const CREATE_PLAN_TOOL_DEF = {
  type: "function",
  function: {
    name: "create_plan",
    description: "Create a structured project plan",
    parameters: { type: "object", properties: {} },
  },
} as const;

// ---------------- BOARD ----------------
export async function getBoard(): Promise<{ tasks: Task[] }> {
  if (USE_MOCK) return structuredClone(mockBoard);

  // Fetch all projects, then fetch tasks per project in parallel.
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

  // No dedicated workforce-shape endpoint exists today.
  // Return null so the UI shows blank metrics rather than crashing.
  return null;
}

// ---------------- CHAT ----------------
export async function sendChat(
  message: string,
  settings?: ChatSettings,
  approvedScopes?: string[]
): Promise<ChatResponse> {
  if (USE_MOCK) return mockChat(message);

  const body: Record<string, unknown> = {
    model: settings?.model,
    messages: [{ role: "user", content: message }],
    // Opt into structured plan/status tool_calls from the daemon.
    tools: [CREATE_PLAN_TOOL_DEF],
  };
  if (approvedScopes && approvedScopes.length > 0) {
    body.approved_scopes = approvedScopes;
  }

  const res = await fetch(`${API}/v1/chat/completions`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`HTTP error! status: ${res.status}`);
  const envelope = await res.json();
  // Unwrap OpenAI choices array into a ChatResponse shape.
  const choice = envelope?.choices?.[0];
  const content: string | null | undefined = choice?.message?.content;
  const toolCalls: { function?: { name?: string; arguments?: string } }[] | undefined =
    choice?.message?.tool_calls;

  if (!content && (!toolCalls || toolCalls.length === 0)) {
    throw new Error("Invalid chat response: missing choices[0].message.content and tool_calls");
  }

  // --- Plan extraction ---
  // Primary: look for a create_plan tool call (daemon emits this when tools were requested).
  let plan: DraftPlan | undefined;
  if (toolCalls && toolCalls.length > 0) {
    const createPlanCall = toolCalls.find((tc) => tc.function?.name === "create_plan");
    if (createPlanCall?.function?.arguments) {
      try {
        plan = mapDaemonDraftPlan(JSON.parse(createPlanCall.function.arguments));
      } catch {
        // malformed arguments — fall through to content fallback
      }
    }
  }
  // Fallback: try to parse plan JSON from message.content (no-tools path).
  if (!plan && content) {
    try {
      const parsed = JSON.parse(content) as Record<string, unknown>;
      if (parsed.project_name || parsed.ProjectName || parsed.tasks || parsed.Tasks) {
        plan = mapDaemonDraftPlan(parsed);
      }
    } catch {
      // plain text content — no plan
    }
  }

  return {
    message: {
      id: envelope?.id ?? "",
      role: "assistant" as const,
      content: content ?? "",
    },
    ...(plan ? { plan } : {}),
  } satisfies ChatResponse;
}

// ---------------- PROVIDERS ----------------
export async function fetchProviders(): Promise<Provider[]> {
  if (USE_MOCK) return mockProviders;

  const res = await fetch(`${API}/api/v1/gateway/providers`);
  if (!res.ok) throw new Error(`HTTP error! status: ${res.status}`);
  const envelope = await res.json();
  return envelope.data as Provider[];
}

export async function postApprovePlan(plan: DraftPlan) {
  if (USE_MOCK) return mockApprovePlan;

  // Map web DraftPlan back to the daemon's models.DraftPlan wire shape.
  const daemonBody = {
    project_name: plan.name,
    description: plan.description,
    tasks: plan.tasks.map((t) => ({
      title: t.title,
      description: t.description,
      ...(t.id ? { temp_id: t.id } : {}),
    })),
  };

  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (MATERIALIZE_TOKEN) {
    headers["X-Agentd-Materialize-Token"] = MATERIALIZE_TOKEN;
  }

  const res = await fetch(`${API}/api/v1/projects/materialize`, {
    method: "POST",
    headers,
    body: JSON.stringify(daemonBody),
  });

  if (!res.ok) {
    throw new Error(`Failed to approve plan: ${res.status}`);
  }

  return res.json();
}

export async function fetchTaskComments(taskId: string): Promise<TaskComment[]> {
  if (USE_MOCK) {
    return mockTaskComments.filter((c: { task_id: string }) => c.task_id === taskId);
  }

  const res = await fetch(`${API}/api/v1/tasks/${taskId}/comments`);

  if (!res.ok) {
    throw new Error("Failed to fetch comments");
  }

  return unwrapData<Record<string, unknown>[]>(await res.json()).map(mapDaemonComment);
}

export async function updateTask(
  id: string,
  updates: Partial<{
    title: string;
    description: string;
    state: string;
    updated_at: number;
  }>
) {
  if (USE_MOCK) {
    const task = mockBoard.tasks.find((t) => t.id === id);
    if (!task) throw new Error("Task not found");
    Object.assign(task, updates, { updated_at: Date.now() });
    return structuredClone(task);
  }

  const res = await fetch(`${API}/api/v1/tasks/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    // Backend patchRequest expects { state, description }; title is not supported yet.
    body: JSON.stringify({
      ...(updates.description !== undefined ? { description: updates.description } : {}),
      ...(updates.state !== undefined ? { state: updates.state } : {}),
    }),
  });

  if (!res.ok) {
    throw new Error("Failed to update task");
  }

  return mapDaemonTask(unwrapData<Record<string, unknown>>(await res.json()));
}

export async function addTaskComment(
  id: string,
  message: string
) {
  if (USE_MOCK) {
    const comment: TaskComment = {
      id: crypto.randomUUID(),
      task_id: id,
      author: {
        id: "me",
        name: "You",
      },
      message,
      created_at: new Date().toISOString(),
    };
    mockTaskComments.push(comment);
    return comment;
  }

  const res = await fetch(
    `${API}/api/v1/tasks/${id}/comments`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      // Backend commentRequest expects { content } not { message }.
      body: JSON.stringify({ content: message }),
    }
  );

  if (!res.ok) {
    throw new Error("Failed to add comment");
  }

  return mapDaemonComment(unwrapData<Record<string, unknown>>(await res.json()));
}