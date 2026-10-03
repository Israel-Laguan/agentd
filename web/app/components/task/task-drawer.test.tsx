import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { Task, TaskEvent, TaskStatus } from "@/lib/types";
import { fetchTaskEvents } from "@/lib/api";
import { TaskDrawer } from "./task-drawer";

// Captured from the devenv stack on 2026-10-01 (T-034); see
// ./task-event-list.test.tsx for the capture provenance.
const CAPTURED_EVENTS: TaskEvent[] = [
  {
    id: "4d3ea4ab-43c1-44a1-81de-44f366e54057",
    project_id: "89bfee74-42b5-433f-bbf1-260bd9f5e36d",
    task_id: "task-under-test",
    type: "RECOVERY",
    payload: "reset ghost task to READY",
    created_at: "2026-10-01T17:33:37.188144663Z",
    updated_at: "2026-10-01T17:33:37.188144663Z",
  },
  {
    id: "c78814d4-14a2-49dd-ac58-41e341abd002",
    project_id: "89bfee74-42b5-433f-bbf1-260bd9f5e36d",
    task_id: "task-under-test",
    type: "LOG_CHUNK",
    payload: "tick 0",
    created_at: "2026-10-01T17:33:40.211024831Z",
    updated_at: "2026-10-01T17:33:40.211024831Z",
  },
];

// A second task's event, so a drawer that forgets to scope by task_id would
// show it. The daemon only ever returns one task's events, so this row cannot
// arrive over the wire — which is exactly why the filter needs a test.
const OTHER_TASK_EVENT: TaskEvent = {
  id: "99999999-9999-4999-8999-999999999999",
  project_id: "89bfee74-42b5-433f-bbf1-260bd9f5e36d",
  task_id: "some-other-task",
  type: "RESULT",
  payload: "a different task's result",
  created_at: "2026-10-01T17:34:00.000000000Z",
  updated_at: "2026-10-01T17:34:00.000000000Z",
};

const NEW_SAME_TASK_EVENT: TaskEvent = {
  id: "88888888-8888-4888-8888-888888888888",
  project_id: "89bfee74-42b5-433f-bbf1-260bd9f5e36d",
  task_id: "task-under-test",
  type: "LOG_CHUNK",
  payload: "tick 1",
  created_at: "2026-10-01T17:33:50.000000000Z",
  updated_at: "2026-10-01T17:33:50.000000000Z",
};

const task: Task = {
  id: "task-under-test",
  title: "list files in the workspace",
  description: "use the shell tool to run ls",
  state: TaskStatus.RUNNING,
  created_at: 1759335210000,
  updated_at: 1759335210000,
  token_usage: 2,
} as Task;

vi.mock("@/lib/api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api")>("@/lib/api");
  return { ...actual, fetchTaskEvents: vi.fn() };
});

const mockFetch = vi.mocked(fetchTaskEvents);

describe("TaskDrawer event log", () => {
  beforeEach(() => {
    mockFetch.mockReset();
    mockFetch.mockResolvedValue([]);
  });

  it("renders the task's own events from the durable log", async () => {
    mockFetch.mockResolvedValue(CAPTURED_EVENTS);

    render(<TaskDrawer task={task} onClose={vi.fn()} />);

    await waitFor(() => expect(screen.getByText("Task events")).toBeInTheDocument());
    expect(screen.getByText("RECOVERY")).toBeInTheDocument();
    expect(screen.getByText("reset ghost task to READY")).toBeInTheDocument();
    expect(mockFetch).toHaveBeenCalledWith("task-under-test");
  });

  it("shows only the open task's events", async () => {
    mockFetch.mockResolvedValue([...CAPTURED_EVENTS, OTHER_TASK_EVENT]);

    render(<TaskDrawer task={task} onClose={vi.fn()} />);

    await waitFor(() => expect(screen.getByText("Task events")).toBeInTheDocument());
    expect(screen.getByText("RECOVERY")).toBeInTheDocument();
    expect(screen.queryByText("a different task's result")).not.toBeInTheDocument();
  });

  // The drawer's events go stale as a task runs, so the page bumps
  // eventRefreshKey to make it re-read. Without the re-fetch the log would freeze
  // at whatever it showed when the drawer opened.
  it("re-fetches when eventRefreshKey changes", async () => {
    mockFetch.mockResolvedValue(CAPTURED_EVENTS);

    const { rerender } = render(
      <TaskDrawer task={task} onClose={vi.fn()} eventRefreshKey={0} />
    );
    await waitFor(() => expect(screen.getByText("Task events")).toBeInTheDocument());
    expect(mockFetch).toHaveBeenCalledTimes(1);

    mockFetch.mockResolvedValue([...CAPTURED_EVENTS, NEW_SAME_TASK_EVENT]);
    rerender(<TaskDrawer task={task} onClose={vi.fn()} eventRefreshKey={1} />);

    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getByText("tick 1")).toBeInTheDocument());
  });

  // A rejected read must clear the log, not merely leave it empty on a fresh
  // mount. So the fetch has to succeed once first: asserting only that a
  // drawer opened with a failing fetch shows no events would pass even if the
  // catch handler were deleted, because a new drawer's events are already [].
  it("clears the list when a refresh rejects, rather than keeping stale events", async () => {
    mockFetch.mockResolvedValue(CAPTURED_EVENTS);

    const { rerender } = render(
      <TaskDrawer task={task} onClose={vi.fn()} eventRefreshKey={0} />
    );
    await waitFor(() => expect(screen.getByText("reset ghost task to READY")).toBeInTheDocument());

    mockFetch.mockRejectedValue(new Error("Failed to fetch task events"));
    rerender(<TaskDrawer task={task} onClose={vi.fn()} eventRefreshKey={1} />);

    await waitFor(() => expect(screen.getByText("No task events yet.")).toBeInTheDocument());
    // The stale log must be gone, not still on screen under the empty state.
    expect(screen.queryByText("reset ghost task to READY")).not.toBeInTheDocument();
    expect(screen.queryByText("Task events")).not.toBeInTheDocument();
  });

  it("shows an empty list when the first fetch rejects", async () => {
    mockFetch.mockRejectedValue(new Error("Failed to fetch task events"));

    render(<TaskDrawer task={task} onClose={vi.fn()} />);

    await waitFor(() => expect(screen.getByText("No task events yet.")).toBeInTheDocument());
  });

  it("does not fetch when no task is open", async () => {
    render(<TaskDrawer task={null} onClose={vi.fn()} />);
    expect(mockFetch).not.toHaveBeenCalled();
  });
});
