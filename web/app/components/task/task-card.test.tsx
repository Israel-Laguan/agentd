import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { TaskAssignee, TaskStatus } from "@/lib/types";
import { TaskCard } from "./task-card";

describe("TaskCard", () => {
  it("shows human attention on a handoff card", () => {
    render(
      <TaskCard
        task={{
          id: "t9",
          title: "Manual action required: privileged command",
          description: "Run the command locally",
          state: TaskStatus.BLOCKED,
          assignee: TaskAssignee.HUMAN,
        }}
        onClick={vi.fn()}
      />
    );
    expect(screen.getByText("Human action")).toBeInTheDocument();
    expect(screen.getByText("BLOCKED")).toBeInTheDocument();
  });
});
