import { describe, it, expect } from "vitest";
import {
  mapScopeOptions,
  mapStatusReport,
  intakeFromRecord,
  parseStructuredIntake,
  displayContentForChat,
} from "./chat-intake";

describe("chat-intake", () => {
  it("maps scope options with label field", () => {
    const scopes = mapScopeOptions([
      { id: "backend-api", label: "Backend API service" },
    ]);
    expect(scopes).toEqual([{ id: "backend-api", label: "Backend API service" }]);
  });

  it("maps status_report payload", () => {
    const report = mapStatusReport({
      kind: "status_report",
      message: "2 projects active",
      summary: { total_projects: 2, tasks_by_state: { RUNNING: 1, READY: 3 } },
    });
    expect(report.message).toBe("2 projects active");
    expect(report.totalProjects).toBe(2);
    expect(report.tasksByState.RUNNING).toBe(1);
  });

  it("parses status_report from tool_calls", () => {
    const args = JSON.stringify({
      kind: "status_report",
      message: "All clear",
      summary: { total_projects: 0, tasks_by_state: {} },
    });
    const intake = parseStructuredIntake(null, [
      { function: { name: "status_report", arguments: args } },
    ]);
    expect(intake?.statusReport?.message).toBe("All clear");
  });

  it("parses intent_clarification from content", () => {
    const content = JSON.stringify({
      kind: "intent_clarification",
      message: "What would you like to do?",
    });
    const intake = parseStructuredIntake(content);
    expect(intake?.intentClarification?.message).toBe("What would you like to do?");
  });

  it("displayContentForChat avoids raw JSON for structured intake", () => {
    const intake = intakeFromRecord({
      kind: "scope_clarification",
      message: "Pick one scope",
      scopes: [{ id: "a", label: "A" }],
    });
    expect(displayContentForChat('{"kind":"scope_clarification"}', false, intake)).toBe(
      "Pick one scope"
    );
  });
});
