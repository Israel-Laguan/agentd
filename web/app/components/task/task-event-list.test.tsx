import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { TaskEvent } from "@/lib/types";
import taskEventsFixture from "./__fixtures__/task-events.json";
import { TaskEventList } from "./task-event-list";

// Captured from GET /api/v1/tasks/e6e62f50-.../events on the devenv stack
// (2026-10-01, T-034). The Go test TestTaskEventsResponseMatchesUIFixture
// (internal/api/controllers) checks the real handler still has this shape.
const CAPTURED_EVENTS: TaskEvent[] = taskEventsFixture.data;

describe("TaskEventList", () => {
  it("shows an empty state when the task has no events", () => {
    render(<TaskEventList events={[]} />);
    expect(screen.getByText("No task events yet.")).toBeInTheDocument();
    expect(screen.queryByText("Task events")).not.toBeInTheDocument();
  });

  it("renders every captured event type from a real response", () => {
    render(<TaskEventList events={CAPTURED_EVENTS} />);

    expect(screen.getByText("Task events")).toBeInTheDocument();
    for (const type of ["WARNING", "RECOVERY", "TOKEN_USAGE", "LOG_CHUNK", "RESULT"]) {
      expect(screen.getByText(type)).toBeInTheDocument();
    }
    // The payload is the event's actual content, not just its type.
    expect(screen.getByText("reset ghost task to READY")).toBeInTheDocument();
  });

  // The daemon returns events oldest-first; the drawer shows newest first so the
  // most recent line is visible without scrolling. Asserting the order matters
  // because reversing it is a one-character change that would be invisible.
  it("orders newest event first", () => {
    render(<TaskEventList events={CAPTURED_EVENTS} />);

    const types = screen.getAllByText(/^(WARNING|RECOVERY|TOKEN_USAGE|LOG_CHUNK|RESULT)$/);
    expect(types.map((node) => node.textContent)).toEqual([
      "RESULT",
      "LOG_CHUNK",
      "TOKEN_USAGE",
      "RECOVERY",
      "WARNING",
    ]);
  });

  it("does not mutate the caller's array", () => {
    const events = [...CAPTURED_EVENTS];
    render(<TaskEventList events={events} />);
    expect(events.map((e) => e.type)).toEqual([
      "WARNING",
      "RECOVERY",
      "TOKEN_USAGE",
      "LOG_CHUNK",
      "RESULT",
    ]);
  });

  it("marks a truncated payload and falls back when there is no payload", () => {
    render(
      <TaskEventList
        events={[
          { ...CAPTURED_EVENTS[0], payload: "", payload_truncated: true },
          { ...CAPTURED_EVENTS[1], payload: "" },
        ]}
      />
    );

    expect(screen.getByText("Payload truncated")).toBeInTheDocument();
    expect(screen.getAllByText("No payload")).toHaveLength(2);
  });
});
