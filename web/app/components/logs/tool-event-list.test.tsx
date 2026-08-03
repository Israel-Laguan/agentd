import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ToolEventList } from "./tool-event-list";
import { ToolCallRecord, ToolResultRecord } from "@/lib/tool-events";

const call: ToolCallRecord = {
  tool_name: "bash",
  call_id: "c1",
  arguments_summary: "ls -la",
};

const result: ToolResultRecord = {
  tool_name: "bash",
  call_id: "c1",
  exit_code: 0,
  duration_ms: 412,
  output_summary: "total 5",
  stdout_bytes: 184,
  stderr_bytes: 0,
};

describe("ToolEventList", () => {
  it("renders empty state when no entries", () => {
    render(<ToolEventList entries={[]} />);
    expect(screen.getByText(/no tool activity yet/i)).toBeInTheDocument();
  });

  it("renders a tool call with tool name and arguments", () => {
    render(<ToolEventList entries={[{ kind: "call", call }]} />);
    expect(screen.getByTestId("tool-call")).toBeInTheDocument();
    expect(screen.getByText("bash")).toBeInTheDocument();
    expect(screen.getByText(/ls -la/)).toBeInTheDocument();
  });

  it("renders a result with exit code, duration, and bytes", () => {
    render(<ToolEventList entries={[{ kind: "result", result }]} />);
    expect(screen.getByTestId("tool-result")).toBeInTheDocument();
    expect(screen.getByText(/exit 0/)).toBeInTheDocument();
    expect(screen.getByText(/412ms/)).toBeInTheDocument();
    expect(screen.getByText(/stdout 184 B/)).toBeInTheDocument();
    expect(screen.queryByText(/stderr/)).not.toBeInTheDocument();
  });

  it("renders paired call arguments on the result", () => {
    render(<ToolEventList entries={[{ kind: "result", result, call }]} />);
    expect(screen.getByText(/args:/)).toBeInTheDocument();
    expect(screen.getByText(/ls -la/)).toBeInTheDocument();
  });

  it("marks a non-zero exit code as an error state", () => {
    const failed = { ...result, exit_code: 1, stderr_bytes: 4 };
    render(<ToolEventList entries={[{ kind: "result", result: failed }]} />);
    expect(screen.getByText("Error")).toBeInTheDocument();
    expect(screen.getByText(/exit 1/)).toBeInTheDocument();
    expect(screen.getByText(/stderr 4 B/)).toBeInTheDocument();
  });
});

describe("ToolEventList expansion", () => {
  const longResult: ToolResultRecord = {
    ...result,
    call_id: "c2",
    output_summary: "y".repeat(300),
  };

  it("shows truncated preview and expands on click", async () => {
    const user = userEvent.setup();
    render(<ToolEventList entries={[{ kind: "result", result: longResult }]} />);
    const button = screen.getByRole("button", { name: /show more/i });
    expect(button).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByText(/y{140}…/)).toBeInTheDocument();

    await user.click(button);
    expect(screen.getByRole("button", { name: /show less/i })).toBeInTheDocument();
  });

  it("does not offer expansion for short summaries", () => {
    render(<ToolEventList entries={[{ kind: "result", result }]} />);
    expect(screen.queryByRole("button", { name: /show more/i })).not.toBeInTheDocument();
  });
});
