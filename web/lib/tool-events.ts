// Tool-event parsing, pairing, and display helpers for the cockpit activity
// view. The daemon already scrubs and truncates payloads (arguments <=200
// chars, output <=1000 chars); these helpers only reshuffle that scrubbed
// content into renderable shapes — nothing here re-introduces raw secrets.
import { parseSseEnvelope } from "@/lib/sse-events";

// Matches internal/queue/worker/worker_events.go ToolCallEvent.
export interface ToolCallRecord {
  tool_name: string;
  call_id: string;
  arguments_summary: string;
}

// Matches internal/queue/worker/worker_events.go ToolResultEvent.
export interface ToolResultRecord {
  tool_name: string;
  call_id: string;
  exit_code: number;
  duration_ms: number;
  output_summary: string;
  stdout_bytes: number;
  stderr_bytes: number;
}

export type ToolRawEvent = ToolCallRecord | ToolResultRecord;

// A renderable activity entry. A result may carry its matching call (paired by
// call_id) so the UI can show the tool's arguments alongside the outcome.
export type ToolEventEntry =
  | { kind: "call"; call: ToolCallRecord }
  | { kind: "result"; result: ToolResultRecord; call?: ToolCallRecord };

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null;
}

function asString(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function asNumber(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

// parseToolCallRecord coerces an unknown object into a ToolCallRecord.
export function parseToolCallRecord(v: unknown): ToolCallRecord {
  const r = isRecord(v) ? v : {};
  return {
    tool_name: asString(r.tool_name ?? r.toolName),
    call_id: asString(r.call_id ?? r.callId),
    arguments_summary: asString(r.arguments_summary ?? r.argumentsSummary),
  };
}

// parseToolResultRecord coerces an unknown object into a ToolResultRecord.
export function parseToolResultRecord(v: unknown): ToolResultRecord {
  const r = isRecord(v) ? v : {};
  return {
    tool_name: asString(r.tool_name ?? r.toolName),
    call_id: asString(r.call_id ?? r.callId),
    exit_code: asNumber(r.exit_code ?? r.exitCode),
    duration_ms: asNumber(r.duration_ms ?? r.durationMs),
    output_summary: asString(r.output_summary ?? r.outputSummary),
    stdout_bytes: asNumber(r.stdout_bytes ?? r.stdoutBytes),
    stderr_bytes: asNumber(r.stderr_bytes ?? r.stderrBytes),
  };
}

// parseToolEventFromSseData decodes a full "data:" line into a typed tool
// event, or null when the frame is not a recognized tool event. It handles the
// nested payload string emitted by the daemon.
export function parseToolEventFromSseData(data: string): ToolRawEvent | null {
  const envelope = parseSseEnvelope(data);
  if (!envelope) return null;

  let body: unknown;
  try {
    body = JSON.parse(envelope.payload);
  } catch {
    return null;
  }

  switch (envelope.type) {
    case "TOOL_CALL": {
      const record = parseToolCallRecord(body);
      if (!record.call_id) return null;
      return record;
    }
    case "TOOL_RESULT": {
      const record = parseToolResultRecord(body);
      if (!record.call_id) return null;
      return record;
    }
    default:
      return null;
  }
}

// buildToolEventGroups converts an ordered list of raw tool events into
// renderable entries, pairing each tool_result with an earlier tool_called
// that shares its call_id ("where ordering allows"). Results whose call was not
// seen, and calls that have not yet returned, render standalone.
export function buildToolEventGroups(raw: ToolRawEvent[]): ToolEventEntry[] {
  const entries: ToolEventEntry[] = [];
  const callsByCallId = new Map<string, ToolCallRecord>();

  for (const evt of raw) {
    if ("output_summary" in evt) {
      // Result
      const call = callsByCallId.get(evt.call_id);
      entries.push({ kind: "result", result: evt, ...(call ? { call } : {}) });
    } else {
      // Call
      callsByCallId.set(evt.call_id, evt);
      entries.push({ kind: "call", call: evt });
    }
  }

  return entries;
}

// Default preview length for collapsible summaries in the UI.
export const DEFAULT_PREVIEW_MAX = 140;

// truncateForPreview computes a collapsed preview for a (already scrubbed)
// summary. Returns the full text when it fits within max; otherwise a truncated
// preview plus needsExpansion so the UI can offer "Show more".
export function truncateForPreview(
  text: string,
  max: number = DEFAULT_PREVIEW_MAX
): { preview: string; needsExpansion: boolean } {
  if (text.length <= max) {
    return { preview: text, needsExpansion: false };
  }
  return { preview: `${text.slice(0, max)}…`, needsExpansion: true };
}

// Sample tool events used to populate the activity view in mock mode so the
// default (USE_MOCK) UI is not empty. Mirrors the scrubbed payload shapes the
// daemon emits.
export const SAMPLE_TOOL_EVENTS: ToolRawEvent[] = [
  {
    tool_name: "bash",
    call_id: "call_sample_1",
    arguments_summary: "ls -la && cat README.md",
  },
  {
    tool_name: "bash",
    call_id: "call_sample_1",
    exit_code: 0,
    duration_ms: 412,
    output_summary: "total 184\ndrw-rw-r--   1 anthony anthony …",
    stdout_bytes: 184,
    stderr_bytes: 0,
  },
  {
    tool_name: "read_file",
    call_id: "call_sample_2",
    arguments_summary: "path=web/lib/tool-events.ts",
  },
  {
    tool_name: "read_file",
    call_id: "call_sample_2",
    exit_code: 0,
    duration_ms: 18,
    output_summary: "// Tool-event parsing, pairing, and display helpers …",
    stdout_bytes: 46,
    stderr_bytes: 0,
  },
];
