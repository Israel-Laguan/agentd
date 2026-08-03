import { describe, it, expect } from "vitest";
import { renderHook } from "@testing-library/react";
import { useToolEventStream } from "./use-tool-event-stream";

describe("useToolEventStream (mock mode)", () => {
  // In the vitest/jsdom environment NEXT_PUBLIC_USE_MOCK is unset, so USE_MOCK
  // is true and the hook seeds the sample tool events without opening an
  // EventSource (which jsdom doesn't implement).
  it("seeds sample tool activity entries, pairing results with calls", () => {
    const { result } = renderHook(() => useToolEventStream());

    const { entries, connected } = result.current;
    expect(connected).toBe(false);
    expect(entries).toHaveLength(4);
    expect(entries.map((e) => e.kind)).toEqual(["call", "result", "call", "result"]);
    expect(entries[0]).toMatchObject({ kind: "call", call: { tool_name: "bash" } });
    const results = entries.filter((e) => e.kind === "result");
    for (const r of results) {
      if (r.kind === "result") {
        expect(r.call?.call_id).toBe(r.result.call_id);
      }
    }
  });
});
