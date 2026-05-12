import { mockBoard } from "@/lib/mocks/board.mock";
import { mockWorkforce } from "@/lib/mocks/workforce.mock";
import { mockChat } from "@/lib/mocks/chat.mock";
import { mockApprovePlan } from "@/lib/mocks/plan.mock";

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