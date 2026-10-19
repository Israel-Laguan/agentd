import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, requestJSON } from "./http";

function stubFetch(res: Partial<Response> & { json?: () => Promise<unknown> }) {
  const fetchMock = vi.fn().mockResolvedValue(res);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("requestJSON", () => {
  it("returns the envelope's data on success", async () => {
    stubFetch({ ok: true, status: 200, json: async () => ({ status: "ok", data: { id: "t1" } }) });
    await expect(requestJSON<{ id: string }>("http://d/x")).resolves.toEqual({ id: "t1" });
  });

  it("returns the whole body when unwrap is off", async () => {
    const body = { id: "chatcmpl-1", choices: [] };
    stubFetch({ ok: true, status: 200, json: async () => body });
    await expect(requestJSON("http://d/x", { unwrap: false })).resolves.toEqual(body);
  });

  it("throws the daemon's error message, status and code", async () => {
    stubFetch({
      ok: false,
      status: 409,
      json: async () => ({
        status: "error",
        error: { code: "STATE_CONFLICT", message: "invalid transition READY -> COMPLETED" },
      }),
    });
    const err = (await requestJSON("http://d/x").catch((e: unknown) => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.message).toBe("invalid transition READY -> COMPLETED");
    expect(err.status).toBe(409);
    expect(err.code).toBe("STATE_CONFLICT");
  });

  it("appends validation details to the message", async () => {
    stubFetch({
      ok: false,
      status: 422,
      json: async () => ({
        error: { code: "VALIDATION_FAILED", message: "invalid plan", details: ["task 1 has no title", "task 2 depends on itself"] },
      }),
    });
    await expect(requestJSON("http://d/x")).rejects.toThrow(
      "invalid plan: task 1 has no title; task 2 depends on itself"
    );
  });

  // T-054: the paths whose status or code changed still surface the daemon's message.
  it.each([
    [503, "UNAVAILABLE", "event bus is unavailable"],
    [503, "UNAVAILABLE", "breaker reset is not available"],
    [400, "VALIDATION_FAILED", 'unknown task state: "BOGUS"'],
    [409, "STATE_CONFLICT", "invalid task state transition"],
    [404, "NOT_FOUND", 'provider not found: no breaker for "typo"'],
  ])("shows the message of a %i %s response", async (status, code, message) => {
    stubFetch({ ok: false, status, json: async () => ({ status: "error", error: { code, message } }) });
    const err = (await requestJSON("http://d/x").catch((e: unknown) => e)) as ApiError;
    expect(err.message).toBe(message);
    expect(err.status).toBe(status);
    expect(err.code).toBe(code);
  });

  it("falls back to the status when the body is not the daemon's envelope", async () => {
    stubFetch({
      ok: false,
      status: 502,
      json: async () => {
        throw new SyntaxError("Unexpected token <");
      },
    });
    const err = (await requestJSON("http://d/x").catch((e: unknown) => e)) as ApiError;
    expect(err.message).toBe("Request failed (HTTP 502)");
    expect(err.status).toBe(502);
  });

  it("sends a JSON body with a content type and no other header", async () => {
    const fetchMock = stubFetch({ ok: true, status: 200, json: async () => ({ data: {} }) });
    await requestJSON("http://d/x", { method: "POST", body: { a: 1 } });
    const [, init] = fetchMock.mock.calls[0];
    expect(init.method).toBe("POST");
    expect(init.body).toBe('{"a":1}');
    expect(init.headers).toEqual({ "Content-Type": "application/json" });
  });
});
