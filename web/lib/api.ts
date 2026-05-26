import { mockBoard } from "@/lib/mocks/board.mock";
import { mockWorkforce } from "@/lib/mocks/workforce.mock";
import { mockChat } from "@/lib/mocks/chat.mock";
import { mockApprovePlan } from "@/lib/mocks/plan.mock";
import { mockTaskComments } from "@/lib/mocks/mock-task-comment";
import { mockProviders } from "@/lib/mocks/providers.mock";
import { ChatSettings } from "@/app/components/chat/chat-settings-modal";
import { Provider, Task, TaskComment, ChatResponse, WorkforceState } from "@/lib/types";
import { unwrapData, mapDaemonTask, mapDaemonComment } from "@/lib/mappers";

// Set NEXT_PUBLIC_USE_MOCK=false to disable mock mode and hit the real daemon.
const USE_MOCK = process.env.NEXT_PUBLIC_USE_MOCK !== "false";
// Set NEXT_PUBLIC_API_URL to override the daemon address (default: http://localhost:8765).
const API = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8765";

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

// ---------------- WORKFORCE ----------------
export async function getWorkforce(): Promise<WorkforceState | null> {
  if (USE_MOCK) return mockWorkforce;

  // No dedicated workforce-shape endpoint exists today.
  // Return null so the UI shows blank metrics rather than crashing.
  return null;
}

// ---------------- CHAT ----------------
export async function sendChat(message: string, settings?: ChatSettings): Promise<ChatResponse> {
  if (USE_MOCK) return mockChat(message);

  const res = await fetch(`${API}/v1/chat/completions`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    // Backend expects OpenAI-compatible wire shape.
    body: JSON.stringify({
      model: settings?.model,
      messages: [{ role: "user", content: message }],
    }),
  });
  if (!res.ok) throw new Error(`HTTP error! status: ${res.status}`);
  const envelope = await res.json();
  // Unwrap OpenAI choices array into a ChatResponse shape.
  const choice = envelope?.choices?.[0];
  if (!choice?.message?.content) {
    throw new Error("Invalid chat response: missing choices[0].message.content");
  }
  return {
    message: {
      id: envelope?.id ?? "",
      role: "assistant" as const,
      content: choice.message.content,
    },
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

export async function postApprovePlan() {
  if (USE_MOCK) return mockApprovePlan;

  const res = await fetch(`${API}/api/v1/approve-plan`, {
    method: "POST",
  });

  if (!res.ok) {
    throw new Error(`Failed to approve plan: ${res.status}`);
  }

  return res.json();
}

export async function fetchTaskComments(taskId: string): Promise<TaskComment[]> {
  if (USE_MOCK) {
    return mockTaskComments.filter((c: { taskId: string }) => c.taskId === taskId);
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
    status: string;
    updatedAt: number;
  }>
) {
  if (USE_MOCK) {
    const task = mockBoard.tasks.find((t) => t.id === id);
    if (!task) throw new Error("Task not found");
    Object.assign(task, updates, { updatedAt: Date.now() });
    return structuredClone(task);
  }

  const res = await fetch(`${API}/api/v1/tasks/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    // Backend patchRequest expects { state, description }; title is not supported yet.
    body: JSON.stringify({
      ...(updates.description !== undefined ? { description: updates.description } : {}),
      ...(updates.status !== undefined ? { state: updates.status } : {}),
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
    const comment = {
      id: crypto.randomUUID(),
      taskId: id,
      author: {
        id: "me",
        name: "You",
      },
      message,
      createdAt: new Date().toISOString(),
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