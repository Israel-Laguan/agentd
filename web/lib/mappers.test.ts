import { describe, it, expect } from "vitest";
import { mapDaemonComment, mapDaemonTask } from "./mappers";

describe("mapDaemonComment", () => {
  it("maps USER author string to You", () => {
    const comment = mapDaemonComment({
      ID: "c1",
      TaskID: "t1",
      Body: "hello",
      Author: "USER",
      CreatedAt: "2025-01-01T00:00:00.000Z",
    });
    expect(comment.author).toEqual({ id: "USER", name: "You" });
    expect(comment.message).toBe("hello");
  });

  it("maps FRONTDESK author string", () => {
    const comment = mapDaemonComment({
      ID: "c2",
      Author: "FRONTDESK",
      Body: "note",
    });
    expect(comment.author.name).toBe("Frontdesk");
  });

  it("maps object author", () => {
    const comment = mapDaemonComment({
      Author: { ID: "u1", Name: "Alice" },
      Body: "hi",
    });
    expect(comment.author).toEqual({ id: "u1", name: "Alice" });
  });

  it("falls back to system when author missing", () => {
    const comment = mapDaemonComment({ Body: "anon" });
    expect(comment.author).toEqual({ id: "system", name: "System" });
  });
});

describe("mapDaemonTask", () => {
  it("reads lowercase state key", () => {
    const task = mapDaemonTask({ id: "t1", state: "RUNNING" });
    expect(task.state).toBe("RUNNING");
  });
});
