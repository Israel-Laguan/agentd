import { mockBoard } from "@/lib/mocks/board.mock";
import { mockTaskComments } from "@/lib/mocks/mock-task-comment";
import { TaskComment } from "@/lib/types";
import { unwrapData, mapDaemonTask, mapDaemonComment } from "@/lib/mappers";
import { API, USE_MOCK } from "@/lib/api-config";

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
