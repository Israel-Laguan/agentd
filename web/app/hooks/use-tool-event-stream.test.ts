import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { TOOL_CALL_EVENT_NAME, TOOL_RESULT_EVENT_NAME } from "@/lib/sse-events";

// Control USE_MOCK per test via a hoisted mutable holder so the mocked
// @/lib/api-config module reports the desired mode.
const mockState = vi.hoisted(() => ({ useMock: true }));

vi.mock("@/lib/api-config", () => ({
  API: "http://localhost:8765",
  get USE_MOCK() {
    return mockState.useMock;
  },
}));

import { useToolEventStream } from "./use-tool-event-stream";

function sseData(type: string, body: Record<string, unknown>): string {
  return JSON.stringify({ topic: "task:test", type, payload: JSON.stringify(body) });
}

function toolCall(callId: string): string {
  return sseData("TOOL_CALL", { tool_name: "bash", call_id: callId, arguments_summary: "ls" });
}

function toolResult(callId: string): string {
  return sseData("TOOL_RESULT", {
    tool_name: "bash", call_id: callId, exit_code: 0,
    duration_ms: 12, output_summary: "ok", stdout_bytes: 2, stderr_bytes: 0,
  });
}

/** Minimal EventSource double that records subscriptions and can fire events. */
class MockEventSource {
  url: string;
  onopen: ((ev: Event) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  closed = false;
  private listeners: Record<string, Array<(ev: { data: string }) => void>> = {};
  static instances: MockEventSource[] = [];
  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }
  addEventListener(name: string, handler: (ev: { data: string }) => void) {
    (this.listeners[name] ??= []).push(handler);
  }
  removeEventListener(name: string, handler: (ev: { data: string }) => void) {
    this.listeners[name] = (this.listeners[name] ?? []).filter((h) => h !== handler);
  }
  close() { this.closed = true; }
  fireOpen() { this.onopen?.(new Event("open")); }
  fireError() { this.onerror?.(new Event("error")); }
  fireEvent(name: string, data: string) {
    for (const h of this.listeners[name] ?? []) h({ data } as MessageEvent);
  }
}

describe("useToolEventStream (mock mode)", () => {
  beforeEach(() => { mockState.useMock = true; });

  // USE_MOCK is true, so the hook seeds the sample tool events without opening
  // an EventSource (which jsdom doesn't implement).
  it("seeds sample tool activity entries, pairing results with calls", () => {
    const { result } = renderHook(() => useToolEventStream());
    const { entries, connected } = result.current;
    expect(connected).toBe(false);
    expect(entries).toHaveLength(4);
    expect(entries.map((e) => e.kind)).toEqual(["call", "result", "call", "result"]);
    expect(entries[0]).toMatchObject({ kind: "call", call: { tool_name: "bash" } });
    const results = entries.filter((e) => e.kind === "result");
    for (const r of results) {
      if (r.kind === "result") {
        expect(r.call?.call_id).toBe(r.result.call_id);
      }
    }
  });
});

describe("useToolEventStream (live mode)", () => {
  let originalEventSource: typeof EventSource | undefined;

  beforeEach(() => {
    mockState.useMock = false;
    MockEventSource.instances = [];
    originalEventSource = globalThis.EventSource;
    // jsdom does not provide EventSource; install the double.
    (globalThis as unknown as { EventSource: typeof EventSource }).EventSource =
      MockEventSource as unknown as typeof EventSource;
  });

  afterEach(() => {
    if (originalEventSource === undefined) {
      delete (globalThis as { EventSource?: typeof EventSource }).EventSource;
    } else {
      globalThis.EventSource = originalEventSource;
    }
    mockState.useMock = true;
  });

  it("opens an EventSource, subscribes, and pairs tool calls with results", () => {
    const { result } = renderHook(() => useToolEventStream("task-1"));
    expect(MockEventSource.instances).toHaveLength(1);
    const source = MockEventSource.instances[0];
    expect(source.url).toBe("http://localhost:8765/api/v1/events/stream?task_id=task-1");
    act(() => {
      source.fireOpen();
      source.fireEvent(TOOL_CALL_EVENT_NAME, toolCall("call_1"));
      source.fireEvent(TOOL_RESULT_EVENT_NAME, toolResult("call_1"));
    });
    expect(result.current.connected).toBe(true);
    expect(result.current.entries).toHaveLength(2);
    expect(result.current.entries.map((e) => e.kind)).toEqual(["call", "result"]);
    const res = result.current.entries[1];
    if (res.kind !== "result") throw new Error("expected result entry");
    expect(res.call?.call_id).toBe("call_1");
  });

  it("sets connected to false on EventSource error", () => {
    const { result } = renderHook(() => useToolEventStream("task-err"));
    const source = MockEventSource.instances[0];
    act(() => source.fireOpen());
    expect(result.current.connected).toBe(true);
    act(() => source.fireError());
    expect(result.current.connected).toBe(false);
  });

  it("clears entries and connection state when taskId changes", () => {
    const { result, rerender } = renderHook(({ id }) => useToolEventStream(id), {
      initialProps: { id: "task-1" },
    });
    const source1 = MockEventSource.instances[0];
    act(() => {
      source1.fireOpen();
      source1.fireEvent(TOOL_CALL_EVENT_NAME, toolCall("call_1"));
    });
    expect(result.current.connected).toBe(true);
    expect(result.current.entries).toHaveLength(1);
    rerender({ id: "task-2" });
    // Second stream starts fresh: no entries or connection state from task-1.
    expect(result.current.entries).toHaveLength(0);
    expect(result.current.connected).toBe(false);
    expect(MockEventSource.instances).toHaveLength(2);
    expect(MockEventSource.instances[1].url).toContain("task_id=task-2");
    expect(source1.closed).toBe(true);
  });

  it("closes the EventSource on unmount", () => {
    const { unmount } = renderHook(() => useToolEventStream("task-cleanup"));
    const source = MockEventSource.instances[0];
    expect(source.closed).toBe(false);
    unmount();
    expect(source.closed).toBe(true);
  });
});
