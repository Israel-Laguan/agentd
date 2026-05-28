import { ChatSettings } from "@/app/components/chat/chat-settings-modal";

// Set NEXT_PUBLIC_USE_MOCK=false to disable mock mode and hit the real daemon.
export const USE_MOCK = process.env.NEXT_PUBLIC_USE_MOCK !== "false";
// Set NEXT_PUBLIC_API_URL to override the daemon address (default: http://localhost:8765).
export const API = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8765";
// Set NEXT_PUBLIC_MATERIALIZE_TOKEN to match api.materialize_token in the daemon config.
export const MATERIALIZE_TOKEN = process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN ?? "";

// OpenAI function-tool definition sent with every chat request so the daemon
// emits tool_calls (create_plan / status_report) when the response is structured.
export const CREATE_PLAN_TOOL_DEF = {
  type: "function",
  function: {
    name: "create_plan",
    description: "Create a structured project plan",
    parameters: { type: "object", properties: {} },
  },
} as const;

export type { ChatSettings };
