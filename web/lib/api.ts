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

  return res.json();
}

// lib/api/comments.ts



export async function fetchTaskComments() {
  if (USE_MOCK) {
    return mockTaskComments;
  }

  // future backend endpoint
  const res = await fetch("/api/v1/tasks/comments");

  if (!res.ok) {
    throw new Error("Failed to fetch comments");
  }

  return res.json();
}

export async function addTaskComment(
  id: string,
  message: string
) {
  if (USE_MOCK) {
    return {
      ...mockTaskComments,
      id: crypto.randomUUID(),
      taskId: id,
      author: {
        id: "me",
        name: "You",
      },
      message,
      createdAt: new Date().toISOString(),
    };
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