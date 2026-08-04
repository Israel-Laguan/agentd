import { describe, it, expect } from "vitest";
import { formatTokenCount, formatDurationMs, formatBytes } from "./format";

describe("formatTokenCount", () => {
  it("keeps small counts verbatim", () => {
    expect(formatTokenCount(42)).toBe("42");
  });
  it("compacts thousands and millions", () => {
    expect(formatTokenCount(1500)).toBe("1.5k");
    expect(formatTokenCount(2_000_000)).toBe("2.0M");
  });
});

describe("formatDurationMs", () => {
  it("renders sub-second durations in ms", () => {
    expect(formatDurationMs(412)).toBe("412ms");
    expect(formatDurationMs(0)).toBe("0ms");
  });
  it("renders seconds for <1min", () => {
    expect(formatDurationMs(1500)).toBe("1.5s");
  });
  it("renders minutes and seconds beyond 1min", () => {
    expect(formatDurationMs(90_000)).toBe("1m 30s");
  });
  it("rounds total seconds before splitting minutes and seconds", () => {
    expect(formatDurationMs(119_500)).toBe("2m 0s");
  });
  it("handles negative/invalid as 0ms", () => {
    expect(formatDurationMs(-5)).toBe("0ms");
    expect(formatDurationMs(Number.NaN)).toBe("0ms");
  });
});

describe("formatBytes", () => {
  it("renders bytes and zero", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(184)).toBe("184 B");
  });
  it("renders kilobytes and megabytes", () => {
    expect(formatBytes(2048)).toBe("2.0 KB");
    expect(formatBytes(5 * 1024 * 1024)).toBe("5.0 MB");
  });
  it("handles invalid sizes as 0 B", () => {
    expect(formatBytes(-10)).toBe("0 B");
    expect(formatBytes(Number.NaN)).toBe("0 B");
  });
});
