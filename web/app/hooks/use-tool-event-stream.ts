"use client";

import { useCallback, useEffect, useState } from "react";
import { API, USE_MOCK } from "@/lib/api-config";
import { TOOL_CALL_EVENT_NAME, TOOL_RESULT_EVENT_NAME } from "@/lib/sse-events";
import {
  ToolCallRecord,
  ToolEventEntry,
  ToolRawEvent,
  buildToolEventGroups,
  parseToolEventFromSseData,
  SAMPLE_TOOL_EVENTS,
} from "@/lib/tool-events";

// Cap the rendered activity list to avoid unbounded growth on long-running
// tasks (kept in step with LogsView's own tail window).
const MAX_TOOL_EVENTS = 200;

interface ToolEventStreamState {
  entries: ToolEventEntry[];
  connected: boolean;
}

/**
 * useToolEventStream subscribes the cockpit activity view to the daemon's SSE
 * firehose and surfaces tool_called / tool_result events as renderable entries.
 *
 * - Real mode (NEXT_PUBLIC_USE_MOCK=false): opens an EventSource on
 *   GET /api/v1/events/stream (optionally narrowed to a task_id), parses
 *   tool events, and pairs results with their calls by call_id. The stream
 *   closes on unmount.
 * - Mock mode (default): seeds a small static sample so the view is not empty.
 *   No EventSource is constructed, which also keeps the hook jsdom-safe.
 *
 * @param taskId optional task to narrow the stream to (firehose when omitted)
 */
export function useToolEventStream(taskId?: string): ToolEventStreamState {
  const [state, setState] = useState<ToolEventStreamState>(
    USE_MOCK
      ? { entries: buildToolEventGroups(SAMPLE_TOOL_EVENTS), connected: false }
      : { entries: [], connected: false }
  );

  // Keep the rendered entry list and connection status in state; the effect
  // below re-runs (reopening the stream) whenever taskId changes.
  const handleRaw = useCallback((raw: ToolRawEvent) => {
    setState((prev) => {
      // Rebuild the ordered entry list, pairing results with earlier calls.
      const callsByCallId = new Map<string, ToolCallRecord>();
      const entries: ToolEventEntry[] = [];
      for (const entry of prev.entries) {
        if (entry.kind === "call") {
          callsByCallId.set(entry.call.call_id, entry.call);
          entries.push(entry);
        } else {
          entries.push({ ...entry, call: entry.call ?? callsByCallId.get(entry.result.call_id) });
        }
      }
      if ("output_summary" in raw) {
        const call = callsByCallId.get(raw.call_id);
        entries.push({ kind: "result", result: raw, ...(call ? { call } : {}) });
      } else {
        callsByCallId.set(raw.call_id, raw);
        entries.push({ kind: "call", call: raw });
      }
      return { connected: prev.connected, entries: entries.slice(-MAX_TOOL_EVENTS) };
    });
  }, []);

  useEffect(() => {
    if (USE_MOCK) return; // static sample; no live stream in mock mode

    // Reset prior activity so a taskId change does not render the previous
    // task's entries or stale connection state before reopening the stream.
    setState({ entries: [], connected: false });

    // EventSource isn't available in jsdom; in real builds it is. Guard so
    // tests rendering this hook in mock mode never touch it.
    if (typeof EventSource === "undefined") return;

    const url = taskId
      ? `${API}/api/v1/events/stream?task_id=${encodeURIComponent(taskId)}`
      : `${API}/api/v1/events/stream`;
    const source = new EventSource(url);

    // Flip the live indicator once the connection is established (async
    // callback, not a synchronous effect-body update).
    source.onopen = () => setState((prev) => ({ ...prev, connected: true }));
    source.onerror = () => setState((prev) => ({ ...prev, connected: false }));

    const onToolCall = (event: MessageEvent) => {
      const raw = parseToolEventFromSseData(event.data);
      if (raw) handleRaw(raw);
    };

    source.addEventListener(TOOL_CALL_EVENT_NAME, onToolCall);
    source.addEventListener(TOOL_RESULT_EVENT_NAME, onToolCall);

    return () => {
      source.onopen = null;
      source.onerror = null;
      source.removeEventListener(TOOL_CALL_EVENT_NAME, onToolCall);
      source.removeEventListener(TOOL_RESULT_EVENT_NAME, onToolCall);
      source.close();
      setState((prev) => ({ ...prev, connected: false }));
    };
  }, [handleRaw, taskId]);

  return state;
}
