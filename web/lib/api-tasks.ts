import { mockBoard } from "@/lib/mocks/board.mock";
import { mockTaskComments } from "@/lib/mocks/mock-task-comment";
import { TaskComment, TaskEvent, HumanHandoffResolution, TaskStatus } from "@/lib/types";
import { unwrapData, mapDaemonTask, mapDaemonComment, mapDaemonTaskEvent } from "@/lib/mappers";
import { API, MATERIALIZE_TOKEN, USE_MOCK } from "@/lib/api-config";
import { mockTaskEvents } from "@/lib/mocks/task-event.mock";

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

export async function fetchTaskEvents(taskId: string): Promise<TaskEvent[]> {
  if (USE_MOCK) {
    return structuredClone(mockTaskEvents.filter((event) => event.task_id === taskId));
  }

  const res = await fetch(`${API}/api/v1/tasks/${taskId}/events?limit=200`);
  if (!res.ok) {
    throw new Error("Failed to fetch task events");
  }

  return unwrapData<Record<string, unknown>[]>(await res.json()).map(mapDaemonTaskEvent);
}

export async function updateTask(
  id: string,
  updates: Partial<{
    description: string;
    state: string;
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

export async function resolveHumanHandoff(
  taskId: string,
  result: string,
  expectedUpdatedAt: number
): Promise<HumanHandoffResolution> {
  if (USE_MOCK) {
    const task = mockBoard.tasks.find((candidate) => candidate.id === taskId);
    if (!task) throw new Error("Task not found");
    if (task.assignee !== "HUMAN") throw new Error("Task is not a human handoff");
    const parent = mockBoard.tasks.find((candidate) => task.depends_on.includes(candidate.id));
    if (!parent) throw new Error("Human handoff has no parent");
    task.state = TaskStatus.COMPLETED;
    task.updated_at = Date.now();
    parent.state = TaskStatus.COMPLETED;
    parent.updated_at = task.updated_at;
    const createdAt = new Date().toISOString();
    mockTaskEvents.push(
      {
        id: crypto.randomUUID(),
        project_id: task.project_id,
        task_id: task.id,
        type: "HUMAN_RESOLUTION",
        payload: JSON.stringify({ parent_task_id: parent.id, result }),
        created_at: createdAt,
        updated_at: createdAt,
      },
      {
        id: crypto.randomUUID(),
        project_id: parent.project_id,
        task_id: parent.id,
        type: "RESULT",
        payload: result,
        created_at: createdAt,
        updated_at: createdAt,
      }
    );
    return structuredClone({ task, parent, result });
  }

  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (MATERIALIZE_TOKEN) {
    headers["X-Agentd-Materialize-Token"] = MATERIALIZE_TOKEN;
  }
  const res = await fetch(`${API}/api/v1/tasks/${taskId}/human-resolution`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      result,
      expected_updated_at: new Date(expectedUpdatedAt).toISOString(),
    }),
  });
  if (!res.ok) {
    throw new Error(`Failed to resolve human handoff: ${res.status}`);
  }

  const data = unwrapData<Record<string, unknown>>(await res.json());
  return {
    task: mapDaemonTask((data.task ?? data.Task) as Record<string, unknown>),
    parent: mapDaemonTask((data.parent ?? data.Parent) as Record<string, unknown>),
    result: String(data.result ?? data.Result ?? ""),
  };
}

export async function addTaskComment(id: string, message: string) {
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

  const res = await fetch(`${API}/api/v1/tasks/${id}/comments`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ content: message }),
  });

  if (!res.ok) {
    throw new Error("Failed to add comment");
  }

  return mapDaemonComment(unwrapData<Record<string, unknown>>(await res.json()));
}
