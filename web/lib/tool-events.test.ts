import { describe, it, expect } from "vitest";
import {
  parseToolEventFromSseData,
  buildToolEventGroups,
  truncateForPreview,
  ToolCallRecord,
  ToolResultRecord,
} from "./tool-events";

describe("parseToolEventFromSseData", () => {
  it("parses a nested TOOL_CALL payload", () => {
    const data = JSON.stringify({
      topic: "task:abc",
      type: "TOOL_CALL",
      payload: JSON.stringify({
        tool_name: "bash",
        call_id: "call_1",
        arguments_summary: "ls -la",
      }),
    });
    const evt = parseToolEventFromSseData(data);
    expect(evt).toEqual({
      tool_name: "bash",
      call_id: "call_1",
      arguments_summary: "ls -la",
    } satisfies ToolCallRecord);
  });

  it("parses a nested TOOL_RESULT payload", () => {
    const data = JSON.stringify({
      topic: "task:abc",
      type: "TOOL_RESULT",
      payload: JSON.stringify({
        tool_name: "bash",
        call_id: "call_1",
        exit_code: 0,
        duration_ms: 412,
        output_summary: "done",
        stdout_bytes: 12,
        stderr_bytes: 0,
      }),
    });
    expect(parseToolEventFromSseData(data)).toEqual({
      tool_name: "bash",
      call_id: "call_1",
      exit_code: 0,
      duration_ms: 412,
      output_summary: "done",
      stdout_bytes: 12,
      stderr_bytes: 0,
    } satisfies ToolResultRecord);
  });

  it("returns null for unrecognized event types", () => {
    const data = JSON.stringify({
      topic: "task:abc",
      type: "LOG",
      payload: JSON.stringify({ message: "hi" }),
    });
    expect(parseToolEventFromSseData(data)).toBeNull();
  });

  it("returns null for malformed JSON", () => {
    expect(parseToolEventFromSseData("not json")).toBeNull();
  });

  it("rejects TOOL_RESULT with empty call_id", () => {
    const data = JSON.stringify({
      type: "TOOL_RESULT",
      payload: JSON.stringify({ tool_name: "bash" }),
    });
    expect(parseToolEventFromSseData(data)).toBeNull();
  });

  it("coerces missing numeric fields to zero while preserving call_id", () => {
    const data = JSON.stringify({
      type: "TOOL_RESULT",
      payload: JSON.stringify({ tool_name: "bash", call_id: "c1" }),
    });
    const evt = parseToolEventFromSseData(data);
    expect(evt).toEqual({
      tool_name: "bash",
      call_id: "c1",
      exit_code: 0,
      duration_ms: 0,
      output_summary: "",
      stdout_bytes: 0,
      stderr_bytes: 0,
    } satisfies ToolResultRecord);
  });
});

describe("buildToolEventGroups", () => {
  const call: ToolCallRecord = {
    tool_name: "read_file",
    call_id: "c1",
    arguments_summary: "path=x",
  };
  const result: ToolResultRecord = {
    tool_name: "read_file",
    call_id: "c1",
    exit_code: 0,
    duration_ms: 18,
    output_summary: "contents",
    stdout_bytes: 8,
    stderr_bytes: 0,
  };

  it("pairs a result with its earlier call by call_id", () => {
    const entries = buildToolEventGroups([call, result]);
    expect(entries).toHaveLength(2);
    expect(entries[1]).toEqual({ kind: "result", result, call });
  });

  it("keeps a call standalone when its result never arrives", () => {
    const entries = buildToolEventGroups([call]);
    expect(entries).toEqual([{ kind: "call", call }]);
  });

  it("keeps a result standalone when its call was never seen", () => {
    const entries = buildToolEventGroups([result]);
    expect(entries).toEqual([{ kind: "result", result }]);
  });

  it("preserves arrival order", () => {
    const secondCall: ToolCallRecord = {
      tool_name: "bash",
      call_id: "c2",
      arguments_summary: "pwd",
    };
    const secondResult: ToolResultRecord = {
      ...result,
      tool_name: "bash",
      call_id: "c2",
    };
    const entries = buildToolEventGroups([call, result, secondCall, secondResult]);
    expect(entries.map((e) => e.kind)).toEqual(["call", "result", "call", "result"]);
    const lastResult = entries.find(
      (e) => e.kind === "result" && e.result.call_id === "c2"
    );
    expect(lastResult).toBeDefined();
    expect(lastResult!.kind).toBe("result");
    expect(lastResult!.call?.tool_name).toBe("bash");
  });
});

describe("truncateForPreview", () => {
  it("returns full text when short enough", () => {
    expect(truncateForPreview("hello", 10)).toEqual({ preview: "hello", needsExpansion: false });
  });

  it("truncates long text and flags for expansion", () => {
    const long = "x".repeat(200);
    expect(truncateForPreview(long, 50).preview).toBe(`${"x".repeat(50)}…`);
    expect(truncateForPreview(long, 50).needsExpansion).toBe(true);
  });

  it("defaults preview length to 140", () => {
    const text = "a".repeat(150);
    expect(truncateForPreview(text).preview).toBe(`${"a".repeat(140)}…`);
  });
});
