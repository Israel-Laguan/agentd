import { describe, expect, it, vi } from "vitest";
import { NON_MOCK_ENV, useNonMockMode } from "./api.nonmock.shared";

const ok = (body: unknown) => ({ ok: true, json: async () => body });

// routeFetch answers /projects with the given IDs and each project's tasks call
// with respond(projectId, offset).
function routeFetch(projectIds: string[], respond: (projectId: string, offset: number) => unknown) {
  return vi.fn(async (url: string) => {
    const m = url.match(/projects\/([^/]+)\/tasks\?.*offset=(\d+)/);
    if (!m) return ok({ data: projectIds.map((id) => ({ id })) });
    return respond(m[1], Number(m[2]));
  });
}

const rows = (prefix: string, n: number, from = 0) =>
  Array.from({ length: n }, (_, i) => ({ id: `${prefix}-${from + i}`, state: "READY", assignee: "SYSTEM" }));

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
      "http://localhost:8765/api/v1/projects/p1/tasks?limit=200&offset=0&include_healing=true"
    );
    expect(board.tasks.map((task) => task.assignee)).toEqual(["SYSTEM", "HUMAN"]);
    expect(board.tasks[0].state).toBe("NEEDS_CONTEXT");
  });

  it("reports a project whose tasks fail to load instead of dropping it silently", async () => {
    enableNonMock();
    const fetchMock = routeFetch(["p1", "p2"], (pid) =>
      pid === "p2"
        ? { ok: false, status: 500, json: async () => ({ error: { message: "db locked" } }) }
        : ok({ data: rows("p1", 2), meta: { total: 2 } })
    );
    vi.stubGlobal("fetch", fetchMock);
    const { getBoard } = await NON_MOCK_ENV.importApi();
    const board = await getBoard();
    expect(board.tasks.map((t) => t.id)).toEqual(["p1-0", "p1-1"]);
    expect(board.failedProjects).toEqual([{ projectId: "p2", message: "db locked" }]);
    expect(board.cappedProjects).toEqual([]);
  });

  it("pages past the daemon's 200-task cap", async () => {
    enableNonMock();
    const fetchMock = routeFetch(["p1"], (_, offset) =>
      ok({ data: rows("p1", Math.min(200, 450 - offset), offset), meta: { total: 450 } })
    );
    vi.stubGlobal("fetch", fetchMock);
    const { getBoard } = await NON_MOCK_ENV.importApi();
    const board = await getBoard();
    expect(board.tasks).toHaveLength(450);
    expect(board.cappedProjects).toEqual([]);
  });

  it("flags a project that still has more tasks after the page limit", async () => {
    enableNonMock();
    const fetchMock = routeFetch(["big"], (_, offset) =>
      ok({ data: rows("big", 200, offset), meta: { total: 5000, has_more: true } })
    );
    vi.stubGlobal("fetch", fetchMock);
    const { getBoard } = await NON_MOCK_ENV.importApi();
    const board = await getBoard();
    expect(board.tasks).toHaveLength(1000);
    expect(board.cappedProjects).toEqual(["big"]);
  });

  it("posts the result and version without a materialize token", async () => {
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
    expect(options.headers).toEqual({ "Content-Type": "application/json" });
    expect(JSON.parse(options.body)).toEqual({
      result: "operator output",
      expected_updated_at: new Date(1234).toISOString(),
    });
    expect(resolution.task.id).toBe("t2");
    expect(resolution.parent.id).toBe("t1");
    expect(resolution.result).toBe("operator output");
  });
});
