import { mockBoard } from "@/lib/mocks/board.mock";
import { mockWorkforce } from "@/lib/mocks/workforce.mock";
import { mockChat } from "@/lib/mocks/chat.mock";
import { mockApprovePlan } from "@/lib/mocks/plan.mock";
import { mockTaskComments } from "@/lib/mocks/mock-task-comment";

const API = process.env.NEXT_PUBLIC_API_URL;
const USE_MOCK = true;

// ---------------- BOARD ----------------
export async function getBoard() {
  if (USE_MOCK) return mockBoard;

  const res = await fetch(`${API}/api/v1/projects`);
  if (!res.ok) throw new Error(`HTTP error! status: ${res.status}`);
  return res.json();
}

// ---------------- WORKFORCE ----------------
export async function getWorkforce() {
  if (USE_MOCK) return mockWorkforce;

  const res = await fetch(`${API}/api/v1/agents`);
  return res.json();
}

// ---------------- CHAT ----------------
export async function sendChat(message: string) {
  if (USE_MOCK) return mockChat(message);

  const res = await fetch(`${API}/v1/chat/completions`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message }),
  });

  return res.json();
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

export async function fetchTaskComments(taskId: string) {
  if (USE_MOCK) {
    return mockTaskComments.filter((c: { taskId: string }) => c.taskId === taskId);
  }

  const res = await fetch(`${API}/api/v1/tasks/${taskId}/comments`);

  if (!res.ok) {
    throw new Error("Failed to fetch comments");
  }

  return res.json();
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
    body: JSON.stringify(updates),
  });

  if (!res.ok) {
    throw new Error("Failed to update task");
  }

  return res.json();
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
      body: JSON.stringify({ message }),
    }
  );

  if (!res.ok) {
    throw new Error("Failed to add comment");
  }

  return res.json();
}