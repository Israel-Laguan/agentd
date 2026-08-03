// Shared SSE event-type mapping for the agentd cockpit.
//
// The daemon streams Server-Sent Events at GET /api/v1/events/stream. Each
// frame carries a "event:" line (the cockpit-friendly name) plus a "data:"
// line that keeps the machine envelope:
//
//   event: tool_called
//   data: {"topic":"task:abc","type":"TOOL_CALL","payload":"{\"...\":...}"}
//
// The envelope's "payload" field is itself a JSON-encoded string holding the
// real event body, so consumers JSON.parse the envelope and then the payload.
// This module maps internal event type identifiers to the SSE event names
// clients subscribe to. Extend it here as more event types are surfaced.
export const SSE_EVENT_NAME: Record<string, string> = {
  TOOL_CALL: "tool_called",
  TOOL_RESULT: "tool_result",
};

// The SSE event names we currently consume.
export const TOOL_CALL_EVENT_NAME = SSE_EVENT_NAME["TOOL_CALL"];
export const TOOL_RESULT_EVENT_NAME = SSE_EVENT_NAME["TOOL_RESULT"];

// The machine envelope carried on the "data:" line of every SSE frame.
export interface SseEnvelope {
  topic: string;
  type: string;
  // payload is a JSON-encoded string; parse it for the real event body.
  payload: string;
}

// parseSseEnvelope decodes the "data:" line into a typed envelope.
export function parseSseEnvelope(data: string): SseEnvelope | null {
  try {
    const parsed = JSON.parse(data) as Record<string, unknown>;
    if (typeof parsed !== "object" || parsed === null) return null;
    const topic = typeof parsed.topic === "string" ? parsed.topic : "";
    const type = typeof parsed.type === "string" ? parsed.type : "";
    const payload = typeof parsed.payload === "string" ? parsed.payload : "";
    if (!type) return null;
    return { topic, type, payload };
  } catch {
    return null;
  }
}
