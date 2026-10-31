import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { TaskAssignee, TaskStatus } from "@/lib/types";
import { TaskCard, splitHandoffCommand } from "./task-card";

// The daemon writes a privileged-command handoff as an intro, a blank line,
// then detectionPayload: pattern, the %q-quoted command, exit, duration and up
// to 1000 characters of output (internal/queue/worker/worker_addons.go).
const handoffDescription =
  'The worker detected a command that requires host privileges. Run it in your own terminal.\n\n' +
  'pattern=privilege command="sudo dmidecode --string system-serial-number" exit=1 duration=12ms\n' +
  "dmidecode: Permission denied\n".repeat(20);

const renderCard = (description: string, assignee = TaskAssignee.HUMAN) =>
  render(
    <TaskCard
      task={{
        id: "t9",
        title: "Manual action required: privileged command",
        description,
        state: TaskStatus.BLOCKED,
        assignee,
      }}
      onClick={vi.fn()}
    />
  );

describe("TaskCard", () => {
  it("shows human attention on a handoff card", () => {
    renderCard("Run the command locally");
    expect(screen.getByText("Human action")).toBeInTheDocument();
    expect(screen.getByText("BLOCKED")).toBeInTheDocument();
  });

  it("puts the Human action badge on its own line, not beside the description", () => {
    renderCard("Run the command locally");
    const badge = screen.getByText("Human action");
    const description = screen.getByText("Run the command locally");
    expect(badge.parentElement).not.toContainElement(description);
  });

  it("clamps the description to three lines", () => {
    renderCard("Run the command locally");
    expect(screen.getByText("Run the command locally")).toHaveClass("line-clamp-3", "break-words");
  });

  it("shows a handoff command as one line of code and leaves its output to the drawer", () => {
    renderCard(handoffDescription);
    const command = screen.getByText("sudo dmidecode --string system-serial-number");
    expect(command.tagName).toBe("CODE");
    expect(command).toHaveAttribute("title", "sudo dmidecode --string system-serial-number");
    expect(screen.getByText(/requires host privileges/)).toBeInTheDocument();
    expect(screen.queryByText(/Permission denied/)).not.toBeInTheDocument();
    expect(screen.queryByText(/pattern=privilege/)).not.toBeInTheDocument();
  });

  it("shows no code line for a description without a command", () => {
    const { container } = renderCard("Run the command locally");
    expect(container.querySelector("code")).toBeNull();
  });
});

describe("splitHandoffCommand", () => {
  it("returns the text before the payload and the unquoted command", () => {
    expect(splitHandoffCommand(handoffDescription)).toEqual({
      text: "The worker detected a command that requires host privileges. Run it in your own terminal.",
      command: "sudo dmidecode --string system-serial-number",
    });
  });

  it("unquotes escaped quotes and backslashes the way Go's %q wrote them", () => {
    const description = 'Intro\n\npattern=privilege command="sh -c \\"echo a\\\\b\\"" exit=1 duration=1ms\n';
    expect(splitHandoffCommand(description).command).toBe('sh -c "echo a\\b"');
  });

  it("reads a payload that starts the description", () => {
    expect(splitHandoffCommand('pattern=privilege command="id" exit=1 duration=1ms\nout')).toEqual({
      text: "",
      command: "id",
    });
  });

  it("leaves a description without a payload line as it is", () => {
    expect(splitHandoffCommand("Run the command locally")).toEqual({ text: "Run the command locally" });
  });

  it("keeps the raw command when its quoting is not JSON-compatible", () => {
    expect(splitHandoffCommand('x\npattern=p command="a\\x00b" exit=1').command).toBe("a\\x00b");
  });
});
