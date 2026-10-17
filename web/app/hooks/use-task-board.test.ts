import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { useState } from "react";
import { Task, TaskAssignee, TaskStatus } from "@/lib/types";
import { updateTask } from "@/lib/api";
import { ApiError } from "@/lib/http";
import { useTaskBoard } from "./use-task-board";

vi.mock("@/lib/api", () => ({ updateTask: vi.fn() }));
const mockUpdate = vi.mocked(updateTask);

const task: Task = {
  id: "t1",
  project_id: "p1",
  title: "ship it",
  description: "",
  state: TaskStatus.READY,
  assignee: TaskAssignee.SYSTEM,
  depends_on: [],
  logs: [],
  created_at: 1,
  updated_at: 2,
};

function setup() {
  return renderHook(() => {
    const [tasks, setTasks] = useState<Task[]>([task]);
    const [, setSelected] = useState<Task | null>(null);
    return { tasks, board: useTaskBoard(tasks, setTasks, setSelected) };
  });
}

const drop = (to: TaskStatus) =>
  ({ active: { id: "t1" }, over: { id: to } }) as unknown as Parameters<
    ReturnType<typeof useTaskBoard>["handleDragEnd"]
  >[0];

describe("useTaskBoard drag errors", () => {
  beforeEach(() => mockUpdate.mockReset());

  it("snaps back and exposes the daemon's message when a drag is rejected", async () => {
    mockUpdate.mockRejectedValueOnce(
      new ApiError("invalid transition READY -> COMPLETED", 409, "STATE_CONFLICT")
    );
    const { result } = setup();

    act(() => result.current.board.handleDragEnd(drop(TaskStatus.COMPLETED)));

    await waitFor(() =>
      expect(result.current.board.writeError).toBe("invalid transition READY -> COMPLETED")
    );
    expect(result.current.tasks[0].state).toBe(TaskStatus.READY);
  });

  it("clears the message on dismiss and on the next successful drag", async () => {
    mockUpdate.mockRejectedValueOnce(new ApiError("nope", 409));
    const { result } = setup();
    act(() => result.current.board.handleDragEnd(drop(TaskStatus.COMPLETED)));
    await waitFor(() => expect(result.current.board.writeError).toBe("nope"));

    act(() => result.current.board.clearWriteError());
    expect(result.current.board.writeError).toBeNull();

    mockUpdate.mockRejectedValueOnce(new ApiError("again", 409));
    act(() => result.current.board.handleDragEnd(drop(TaskStatus.COMPLETED)));
    await waitFor(() => expect(result.current.board.writeError).toBe("again"));

    mockUpdate.mockResolvedValueOnce({ ...task, state: TaskStatus.QUEUED, updated_at: 9 });
    act(() => result.current.board.handleDragEnd(drop(TaskStatus.QUEUED)));
    await waitFor(() => expect(result.current.board.writeError).toBeNull());
  });
});
