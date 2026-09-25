import { describe, expect, it, vi } from "vitest";
import { NON_MOCK_ENV, useNonMockMode } from "./api.nonmock.shared";

describe("API (non-mock board and human resolution)", () => {
  const enableNonMock = useNonMockMode();

  it("requests healing tasks with a 200-task page", async () => {
    enableNonMock();
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ data: [{ id: "p1" }] }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          data: [
            { id: "t1", state: "NEEDS_CONTEXT", assignee: "SYSTEM" },
            { id: "t2", state: "BLOCKED", assignee: "HUMAN" },
          ],
        }),
      });
    vi.stubGlobal("fetch", fetchMock);
    const { getBoard } = await NON_MOCK_ENV.importApi();
    const board = await getBoard();
    expect(fetchMock.mock.calls[1][0]).toBe(
      "http://localhost:8765/api/v1/projects/p1/tasks?limit=200&include_healing=true"
    );
    expect(board.tasks.map((task) => task.assignee)).toEqual(["SYSTEM", "HUMAN"]);
    expect(board.tasks[0].state).toBe("NEEDS_CONTEXT");
  });

  it("posts the result, version, and materialize token", async () => {
    enableNonMock();
    process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN = "secret-token";
    vi.resetModules();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          task: { id: "t2", state: "COMPLETED", assignee: "HUMAN" },
          parent: { id: "t1", state: "COMPLETED", assignee: "SYSTEM" },
          result: "operator output",
        },
      }),
    });
    vi.stubGlobal("fetch", fetchMock);
    const { resolveHumanHandoff } = await NON_MOCK_ENV.importApi();
    const resolution = await resolveHumanHandoff("t2", "operator output", 1234);
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe("http://localhost:8765/api/v1/tasks/t2/human-resolution");
    expect(options.method).toBe("POST");
    expect(options.headers["X-Agentd-Materialize-Token"]).toBe("secret-token");
    expect(JSON.parse(options.body)).toEqual({
      result: "operator output",
      expected_updated_at: new Date(1234).toISOString(),
    });
    expect(resolution.task.id).toBe("t2");
    expect(resolution.parent.id).toBe("t1");
    expect(resolution.result).toBe("operator output");
  });
});
