// Shared SSE event-type mapping for the agentd cockpit.
//
// The daemon streams Server-Sent Events at GET /api/v1/events/stream. Each
// frame carries a "event:" line (the cockpit-friendly name) plus a "data:"
// line that keeps the machine envelope:
//
//   event: tool_called
//   data: {"topic":"*","type":"TOOL_CALL","payload":"{\"...\":...}","task_id":"abc","project_id":"p1"}
//
// "topic" is the topic the client subscribed to: "*" on the unfiltered global
// stream, "task:abc" when subscribed with ?task_id=abc. "task_id" and
// "project_id" are present on every frame that belongs to a task, whatever the
// topic, and omitted on frames that do not (agent events).
//
// The envelope's "payload" field is itself a JSON-encoded string holding the
// real event body, so consumers JSON.parse the envelope and then the payload.
// This module maps internal event type identifiers to the SSE event names
// clients subscribe to. Extend it here as more event types are surfaced.
export const SSE_EVENT_NAME = {
  TOOL_CALL: "tool_called",
  TOOL_RESULT: "tool_result",
} as const;

export type SseEventType = keyof typeof SSE_EVENT_NAME;

// The SSE event names we currently consume.
export const TOOL_CALL_EVENT_NAME = SSE_EVENT_NAME["TOOL_CALL"];
export const TOOL_RESULT_EVENT_NAME = SSE_EVENT_NAME["TOOL_RESULT"];

// Frames that change a task row on the board: refetch the frame's project.
export const BOARD_ROW_EVENT_NAMES = [
  "task_updated",
  "task_failed",
  "task_retried",
  "task_split",
  "task_assigned",
  "task_recovered",
  "task_reconciled",
] as const;

// Frames that add to an open task's detail view (events, comments, logs).
export const TASK_DETAIL_EVENT_NAMES = [
  "comment_added",
  "log_chunk",
  TOOL_CALL_EVENT_NAME,
  TOOL_RESULT_EVENT_NAME,
] as const;

// The machine envelope carried on the "data:" line of every SSE frame.
export interface SseEnvelope {
  topic: string;
  type: string;
  // payload is a JSON-encoded string; parse it for the real event body.
  payload: string;
  // Present when the frame belongs to a task / project (daemon since B-073).
  task_id?: string;
  project_id?: string;
}

// parseSseEnvelope decodes the "data:" line into a typed envelope.
export function parseSseEnvelope(data: string): SseEnvelope | null {
  try {
    const parsed = JSON.parse(data) as Record<string, unknown>;
    if (typeof parsed !== "object" || parsed === null) return null;
    const topic = typeof parsed.topic === "string" ? parsed.topic : "";
    const type = typeof parsed.type === "string" ? parsed.type : "";
    if (typeof parsed.payload !== "string") return null;
    if (!type) return null;
    const task_id = typeof parsed.task_id === "string" ? parsed.task_id : undefined;
    const project_id = typeof parsed.project_id === "string" ? parsed.project_id : undefined;
    return { topic, type, payload: parsed.payload, task_id, project_id };
  } catch {
    return null;
  }
}
