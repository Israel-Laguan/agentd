import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Task, TaskStatus } from "@/lib/types";
import { ApiError } from "@/lib/http";
import { TaskDrawer } from "./task-drawer";

vi.mock("@/lib/api", () => ({
  fetchTaskEvents: vi.fn().mockResolvedValue([]),
  fetchTaskComments: vi.fn().mockResolvedValue([]),
  addTaskComment: vi.fn(),
}));

const task = {
  id: "t1",
  title: "list files",
  description: "run ls",
  state: TaskStatus.RUNNING,
  created_at: 1759335210000,
  updated_at: 1759335210000,
  token_usage: 0,
} as Task;

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
});

describe("TaskDrawer failed edits", () => {
  it("shows the daemon's message and reverts a rejected status change", async () => {
    const onUpdateTask = vi
      .fn()
      .mockRejectedValue(new ApiError("invalid transition RUNNING -> PENDING", 409));
    render(<TaskDrawer task={task} onClose={vi.fn()} onUpdateTask={onUpdateTask} />);

    const select = screen.getByRole("combobox");
    await userEvent.selectOptions(select, TaskStatus.PENDING);

    expect(await screen.findByRole("alert")).toHaveTextContent("invalid transition RUNNING -> PENDING");
    expect(select).toHaveValue(TaskStatus.RUNNING);
  });

  it("shows the daemon's message and reverts a rejected description edit", async () => {
    const onUpdateTask = vi.fn().mockRejectedValue(new ApiError("description is too long", 422));
    render(<TaskDrawer task={task} onClose={vi.fn()} onUpdateTask={onUpdateTask} />);

    await userEvent.click(screen.getByRole("button", { name: "Edit description" }));
    const box = screen.getByDisplayValue("run ls");
    await userEvent.clear(box);
    await userEvent.type(box, "something else");
    await userEvent.tab();

    expect(await screen.findByRole("alert")).toHaveTextContent("description is too long");
    expect(screen.getByRole("button", { name: "Edit description" })).toHaveTextContent("run ls");
  });

  it("clears the message after a later edit succeeds", async () => {
    const onUpdateTask = vi
      .fn()
      .mockRejectedValueOnce(new ApiError("daemon busy", 503))
      .mockResolvedValueOnce(undefined);
    render(<TaskDrawer task={task} onClose={vi.fn()} onUpdateTask={onUpdateTask} />);

    const select = screen.getByRole("combobox");
    await userEvent.selectOptions(select, TaskStatus.PENDING);
    await screen.findByRole("alert");
    await userEvent.selectOptions(select, TaskStatus.READY);

    await vi.waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  });
});
