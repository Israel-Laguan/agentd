import { describe, it, expect } from "vitest";
import { mapDaemonComment, mapDaemonTask, mapDaemonDraftPlan } from "./mappers";

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

  it("maps snake_case content field to message", () => {
    const comment = mapDaemonComment({
      id: "c3",
      task_id: "t1",
      content: "from api",
      created_at: "2025-06-01T00:00:00.000Z",
    });
    expect(comment.message).toBe("from api");
  });
});

describe("mapDaemonTask", () => {
  it("reads lowercase state key", () => {
    const task = mapDaemonTask({ id: "t1", state: "RUNNING" });
    expect(task.state).toBe("RUNNING");
  });
});

describe("mapDaemonDraftPlan", () => {
  it("maps snake_case project_name to name", () => {
    const plan = mapDaemonDraftPlan({
      project_name: "My API",
      description: "A cool API",
      tasks: [
        { title: "Setup", description: "init project", ref_id: "t1" },
        { title: "Build", description: "write code", temp_id: "t2" },
      ],
    });
    expect(plan.name).toBe("My API");
    expect(plan.description).toBe("A cool API");
    expect(plan.tasks).toHaveLength(2);
    expect(plan.tasks[0]).toEqual({ id: "t1", title: "Setup", description: "init project" });
    expect(plan.tasks[1]).toEqual({ id: "t2", title: "Build", description: "write code" });
  });

  it("maps PascalCase legacy fields", () => {
    const plan = mapDaemonDraftPlan({
      ProjectName: "Legacy Project",
      Description: "Old style",
      Tasks: [{ Title: "Do thing", Description: "desc", TempID: "tmp1" }],
    });
    expect(plan.name).toBe("Legacy Project");
    expect(plan.tasks[0].title).toBe("Do thing");
    expect(plan.tasks[0].id).toBe("tmp1");
  });

  it("omits task id when neither ref_id nor temp_id present", () => {
    const plan = mapDaemonDraftPlan({
      project_name: "No IDs",
      description: "",
      tasks: [{ title: "Task A", description: "desc" }],
    });
    expect(plan.tasks[0].id).toBeUndefined();
    expect(plan.tasks[0].title).toBe("Task A");
  });

  it("prefers ref_id over temp_id", () => {
    const plan = mapDaemonDraftPlan({
      project_name: "P",
      description: "",
      tasks: [{ title: "T", description: "", ref_id: "ref1", temp_id: "tmp1" }],
    });
    expect(plan.tasks[0].id).toBe("ref1");
  });

  it("handles missing tasks field", () => {
    const plan = mapDaemonDraftPlan({ project_name: "Empty", description: "" });
    expect(plan.tasks).toEqual([]);
  });
});
