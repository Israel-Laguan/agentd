import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { TaskEvent } from "@/lib/types";
import { TaskEventList } from "./task-event-list";

// Captured verbatim from GET /api/v1/tasks/e6e62f50-.../events on the devenv
// stack (2026-10-01, T-034), after TestJ08_UncleanKillRecovery ran. Types and
// payload strings are the real wire values, so the test fails if the shape
// drifts rather than merely if the markup changes. The RESULT row is real
// except for its id and payload, which were trimmed to keep the fixture short.
const CAPTURED_EVENTS: TaskEvent[] = [
  {
    id: "59919cd9-183e-4bd2-aef8-25219c819cfd",
    project_id: "89bfee74-42b5-433f-bbf1-260bd9f5e36d",
    task_id: "e6e62f50-587e-4cdc-b67d-aa7e6e85f45b",
    type: "WARNING",
    payload:
      "workspace empty at dispatch time; consider using source_path on materialize or calling POST /workspace/ready after seeding",
    created_at: "2026-10-01T17:33:35.908161133Z",
    updated_at: "2026-10-01T17:33:35.908161133Z",
  },
  {
    id: "4d3ea4ab-43c1-44a1-81de-44f366e54057",
    project_id: "89bfee74-42b5-433f-bbf1-260bd9f5e36d",
    task_id: "e6e62f50-587e-4cdc-b67d-aa7e6e85f45b",
    type: "RECOVERY",
    payload: "reset ghost task to READY",
    created_at: "2026-10-01T17:33:37.188144663Z",
    updated_at: "2026-10-01T17:33:37.188144663Z",
  },
  {
    id: "c78814d4-14a2-49dd-ac58-41e341abd002",
    project_id: "89bfee74-42b5-433f-bbf1-260bd9f5e36d",
    task_id: "e6e62f50-587e-4cdc-b67d-aa7e6e85f45b",
    type: "TOKEN_USAGE",
    payload: '{"tokens":2}',
    created_at: "2026-10-01T17:33:40.205145409Z",
    updated_at: "2026-10-01T17:33:40.205145409Z",
  },
  {
    id: "78832ae1-0031-4b05-993b-254271a714dc",
    project_id: "89bfee74-42b5-433f-bbf1-260bd9f5e36d",
    task_id: "e6e62f50-587e-4cdc-b67d-aa7e6e85f45b",
    type: "LOG_CHUNK",
    payload: "tick 0",
    created_at: "2026-10-01T17:33:40.211024831Z",
    updated_at: "2026-10-01T17:33:40.211024831Z",
  },
  {
    id: "b1c0ffee-0000-4000-8000-000000000001",
    project_id: "89bfee74-42b5-433f-bbf1-260bd9f5e36d",
    task_id: "e6e62f50-587e-4cdc-b67d-aa7e6e85f45b",
    type: "RESULT",
    payload: "exit=0 duration=1.2s\ntick 0\ntick 1",
    created_at: "2026-10-01T17:33:44.000000000Z",
    updated_at: "2026-10-01T17:33:44.000000000Z",
  },
];

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
