import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";

vi.mock("@/lib/api-config", () => ({ API: "http://localhost:8765", USE_MOCK: false }));

import { useBoardStream } from "./use-board-stream";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 1;
  closed = false;
  private listeners: Record<string, Array<(ev: { data: string }) => void>> = {};
  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }
  addEventListener(name: string, h: (ev: { data: string }) => void) {
    (this.listeners[name] ??= []).push(h);
  }
  removeEventListener(name: string, h: (ev: { data: string }) => void) {
    this.listeners[name] = (this.listeners[name] ?? []).filter((x) => x !== h);
  }
  close() {
    this.closed = true;
  }
  frame(name: string, ids: { task_id?: string; project_id?: string }) {
    const data = JSON.stringify({ topic: "*", type: "RESULT", payload: "{}", ...ids });
    for (const h of this.listeners[name] ?? []) h({ data });
  }
}

const latest = () => FakeEventSource.instances[FakeEventSource.instances.length - 1];

describe("useBoardStream", () => {
  const refreshProject = vi.fn();
  const resync = vi.fn();

  beforeEach(() => {
    vi.useFakeTimers();
    FakeEventSource.instances = [];
    refreshProject.mockReset();
    resync.mockReset();
    vi.stubGlobal("EventSource", FakeEventSource);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  const mount = (selectedTaskId?: string) =>
    renderHook(({ id }) => useBoardStream({ selectedTaskId: id, refreshProject, resync }), {
      initialProps: { id: selectedTaskId },
    });

  it("refetches only the project a frame names, once for a burst", () => {
    mount();
    act(() => {
      latest().frame("task_updated", { task_id: "a1", project_id: "pa" });
      latest().frame("task_failed", { task_id: "a2", project_id: "pa" });
      vi.advanceTimersByTime(300);
    });
    expect(refreshProject).toHaveBeenCalledTimes(1);
    expect(refreshProject).toHaveBeenCalledWith("pa");
  });

  // B-074: the store now sends task_updated for every committed row change, so
  // a change that also has a specialised frame (retry, split, assign, failure)
  // arrives twice. The daemon does not dedupe; this absorbs it.
  it("costs one refetch when a specialised frame and the store's task_updated name the same task", () => {
    mount();
    act(() => {
      latest().frame("task_retried", { task_id: "a1", project_id: "pa" });
      latest().frame("task_updated", { task_id: "a1", project_id: "pa" });
      vi.advanceTimersByTime(300);
    });
    expect(refreshProject).toHaveBeenCalledTimes(1);
    expect(refreshProject).toHaveBeenCalledWith("pa");
  });

  it("ignores frames that do not change a task row", () => {
    mount();
    act(() => {
      latest().frame("log_chunk", { task_id: "a1", project_id: "pa" });
      latest().frame("comment_added", { task_id: "a1", project_id: "pa" });
      vi.advanceTimersByTime(300);
    });
    expect(refreshProject).not.toHaveBeenCalled();
  });

  it("bumps the activity key only for the selected task", () => {
    const { result } = mount("t1");
    const before = result.current.taskActivityKey;
    act(() => {
      latest().frame("log_chunk", { task_id: "other", project_id: "pa" });
      vi.advanceTimersByTime(2000);
    });
    expect(result.current.taskActivityKey).toBe(before);
    act(() => {
      latest().frame("log_chunk", { task_id: "t1", project_id: "pa" });
      vi.advanceTimersByTime(2000);
    });
    expect(result.current.taskActivityKey).toBe(before + 1);
  });

  it("re-reads comments only on comment_added, not on log or tool frames", () => {
    const { result } = mount("t1");
    const comments = result.current.commentActivityKey;
    act(() => {
      latest().frame("log_chunk", { task_id: "t1", project_id: "pa" });
      latest().frame("tool_called", { task_id: "t1", project_id: "pa" });
      vi.advanceTimersByTime(2000);
    });
    expect(result.current.commentActivityKey).toBe(comments);
    act(() => {
      latest().frame("comment_added", { task_id: "t1", project_id: "pa" });
      vi.advanceTimersByTime(300);
    });
    expect(result.current.commentActivityKey).toBe(comments + 1);
  });

  it("throttles the event list: a log stream costs one refetch per window", () => {
    const { result } = mount("t1");
    const before = result.current.taskActivityKey;
    act(() => {
      for (let i = 0; i < 20; i++) {
        latest().frame("log_chunk", { task_id: "t1", project_id: "pa" });
        vi.advanceTimersByTime(100);
      }
    });
    expect(result.current.taskActivityKey - before).toBeLessThanOrEqual(2);
  });

  it("resyncs the whole board when the stream reopens after an error", () => {
    mount();
    act(() => latest().onopen?.());
    expect(resync).not.toHaveBeenCalled();
    act(() => latest().onerror?.());
    act(() => latest().onopen?.());
    expect(resync).toHaveBeenCalledTimes(1);
  });

  it("re-reads the open drawer's comments and events after a reconnect", () => {
    const { result } = mount("t1");
    act(() => latest().onopen?.());
    const { taskActivityKey: events, commentActivityKey: comments } = result.current;
    act(() => latest().onerror?.());
    act(() => latest().onopen?.());
    expect(result.current.commentActivityKey).toBe(comments + 1);
    expect(result.current.taskActivityKey).toBe(events + 1);
  });

  it("opens a fresh stream after the browser gives up, and closes on unmount", () => {
    const { unmount } = mount();
    const first = latest();
    first.readyState = 2;
    act(() => {
      first.onerror?.();
      vi.advanceTimersByTime(5000);
    });
    expect(FakeEventSource.instances).toHaveLength(2);
    unmount();
    expect(latest().closed).toBe(true);
  });
});
