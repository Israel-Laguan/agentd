import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { BoardSnapshot, Task, TaskAssignee, TaskStatus } from "@/lib/types";
import { getBoard, getProjectTasks } from "@/lib/api";
import { useDashboard } from "./use-dashboard";

vi.mock("@/lib/api", () => ({
  getBoard: vi.fn(),
  getProjectTasks: vi.fn(),
  getWorkforce: vi.fn(async () => null),
  getSystemStatus: vi.fn(async () => null),
}));

const task = (id: string, project_id: string, updated_at = 2): Task => ({
  id, project_id, title: id, description: "", state: TaskStatus.READY, assignee: TaskAssignee.SYSTEM,
  depends_on: [], logs: [], created_at: 1, updated_at,
});
const board = (tasks: Task[]): BoardSnapshot => ({ tasks, failedProjects: [], cappedProjects: [] });

function setHidden(hidden: boolean) {
  Object.defineProperty(document, "hidden", { configurable: true, get: () => hidden });
  document.dispatchEvent(new Event("visibilitychange"));
}

describe("useDashboard live refresh", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(getBoard).mockReset().mockResolvedValue(board([task("a1", "pa"), task("b1", "pb")]));
    vi.mocked(getProjectTasks).mockReset();
    setHidden(false);
  });
  afterEach(() => vi.useRealTimers());

  it("polls every 30s as a fallback, not every 3s", async () => {
    renderHook(() => useDashboard());
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(getBoard).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(getBoard).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(20_000));
    expect(getBoard).toHaveBeenCalledTimes(2);
  });

  it("pauses polling while the tab is hidden and refreshes when it returns", async () => {
    renderHook(() => useDashboard());
    await act(() => vi.advanceTimersByTimeAsync(0));
    act(() => setHidden(true));
    await act(() => vi.advanceTimersByTimeAsync(120_000));
    expect(getBoard).toHaveBeenCalledTimes(1);
    await act(async () => setHidden(false));
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(getBoard).toHaveBeenCalledTimes(2);
  });

  it("refreshProject replaces only that project's tasks", async () => {
    const { result } = renderHook(() => useDashboard());
    await act(() => vi.advanceTimersByTimeAsync(0));
    vi.mocked(getProjectTasks).mockResolvedValue({ tasks: [task("a1", "pa", 9), task("a2", "pa", 9)], capped: false });
    await act(() => result.current.refreshProject("pa"));
    expect(getProjectTasks).toHaveBeenCalledWith("pa");
    expect(getBoard).toHaveBeenCalledTimes(1);
    expect(result.current.localTasks.map((t) => t.id).sort()).toEqual(["a1", "a2", "b1"]);
    expect(result.current.localTasks.find((t) => t.id === "a1")?.updated_at).toBe(9);
  });
});
