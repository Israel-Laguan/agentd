import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { BoardSnapshot, Task, TaskAssignee, TaskStatus } from "@/lib/types";
import { getBoard } from "@/lib/api";
import { useDashboard } from "./use-dashboard";

vi.mock("@/lib/api", () => ({
  getBoard: vi.fn(),
  getWorkforce: vi.fn(async () => null),
  getSystemStatus: vi.fn(async () => null),
}));
const mockBoard = vi.mocked(getBoard);

const task = (id: string, project_id: string): Task => ({
  id,
  project_id,
  title: id,
  description: "",
  state: TaskStatus.READY,
  assignee: TaskAssignee.SYSTEM,
  depends_on: [],
  logs: [],
  created_at: 1,
  updated_at: 2,
});

const snapshot = (tasks: Task[], extra: Partial<BoardSnapshot> = {}): BoardSnapshot => ({
  tasks,
  failedProjects: [],
  cappedProjects: [],
  ...extra,
});

describe("useDashboard board refresh", () => {
  beforeEach(() => mockBoard.mockReset());

  it("keeps a project's last known tasks and reports the error when its fetch fails", async () => {
    mockBoard.mockResolvedValueOnce(snapshot([task("a1", "pa"), task("b1", "pb")]));
    const { result } = renderHook(() => useDashboard());
    await waitFor(() => expect(result.current.localTasks).toHaveLength(2));

    mockBoard.mockResolvedValueOnce(
      snapshot([task("a1", "pa")], { failedProjects: [{ projectId: "pb", message: "db locked" }] })
    );
    await act(() => result.current.refreshDashboard());

    expect(result.current.localTasks.map((t) => t.id).sort()).toEqual(["a1", "b1"]);
    expect(result.current.boardIssues.failedProjects).toEqual([{ projectId: "pb", message: "db locked" }]);
  });

  it("reports a capped project", async () => {
    mockBoard.mockResolvedValue(snapshot([task("a1", "pa")], { cappedProjects: ["pa"] }));
    const { result } = renderHook(() => useDashboard());
    await waitFor(() => expect(result.current.boardIssues.cappedProjects).toEqual(["pa"]));
  });
});
