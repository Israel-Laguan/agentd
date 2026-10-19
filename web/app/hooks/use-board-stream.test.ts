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
      latest().frame("comment_added", { task_id: "other", project_id: "pa" });
      vi.advanceTimersByTime(300);
    });
    expect(result.current.taskActivityKey).toBe(before);
    act(() => {
      latest().frame("comment_added", { task_id: "t1", project_id: "pa" });
      vi.advanceTimersByTime(300);
    });
    expect(result.current.taskActivityKey).toBe(before + 1);
  });

  it("resyncs the whole board when the stream reopens after an error", () => {
    mount();
    act(() => latest().onopen?.());
    expect(resync).not.toHaveBeenCalled();
    act(() => latest().onerror?.());
    act(() => latest().onopen?.());
    expect(resync).toHaveBeenCalledTimes(1);
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
