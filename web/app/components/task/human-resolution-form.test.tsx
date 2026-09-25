import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TaskAssignee, TaskStatus } from "@/lib/types";
import { extractBlockedCommand, HumanResolutionForm } from "./human-resolution-form";

const task = {
  id: "t9",
  project_id: "p1",
  title: "Manual action required: privileged command",
  description: 'Privilege blocked\n\npattern=privilege command="sudo dmidecode --string system-serial-number" exit=1',
  state: TaskStatus.BLOCKED,
  assignee: TaskAssignee.HUMAN,
  depends_on: ["t1"],
  logs: [],
  created_at: 1,
  updated_at: 2,
};

describe("HumanResolutionForm", () => {
  it("extracts the exact command from daemon payloads", () => {
    expect(extractBlockedCommand(task.description)).toBe("sudo dmidecode --string system-serial-number");
  });

  it("requires a result and resolves the handoff", async () => {
    const onResolve = vi.fn().mockResolvedValue(undefined);
    render(<HumanResolutionForm task={task} onResolve={onResolve} />);
    const resolve = screen.getByRole("button", { name: /resolve handoff/i });
    expect(resolve).toBeDisabled();
    await userEvent.type(screen.getByLabelText(/command output or result/i), "  serial: abc123  ");
    expect(resolve).toBeEnabled();
    await userEvent.click(resolve);
    expect(onResolve).toHaveBeenCalledWith("t9", "serial: abc123");
  });
});
