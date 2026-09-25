import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TaskAssignee, TaskStatus } from "@/lib/types";
import { ChatStatusReportView } from "./chat-status-report-view";

const report = {
  message: "One item needs attention",
  totalProjects: 1,
  tasksByState: { BLOCKED: 1 },
  attention: [
    {
      projectId: "p1",
      projectName: "Inventory",
      taskId: "t9",
      taskTitle: "Privileged lookup",
      state: TaskStatus.BLOCKED,
      assignee: TaskAssignee.HUMAN,
      requiredAction: "Open the task and submit the result.",
      explanation: "A human handoff is open.",
    },
  ],
};

describe("ChatStatusReportView", () => {
  it("renders attention and opens the exact task", async () => {
    const onOpenTask = vi.fn();
    render(<ChatStatusReportView report={report} onOpenTask={onOpenTask} />);
    expect(screen.getByText("Privileged lookup")).toBeInTheDocument();
    expect(screen.getByText("A human handoff is open.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /open in board/i }));
    expect(onOpenTask).toHaveBeenCalledWith("t9");
  });

  it("does not render attention actions when there are no items", () => {
    render(
      <ChatStatusReportView
        report={{ ...report, attention: [] }}
        onOpenTask={vi.fn()}
      />
    );
    expect(screen.queryByRole("button", { name: /open in board/i })).not.toBeInTheDocument();
  });
});
