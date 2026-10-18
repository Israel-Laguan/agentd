import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Footer } from "./footer";

const base = { totalTokens: 0, runningTasks: 0 };

describe("Footer daemon label", () => {
  it.each([
    [true, undefined, "RUNNING"],
    [true, "CLOSED", "RUNNING"],
    [true, "OPEN", "PAUSED: BREAKER OPEN"],
    [true, "HALF_OPEN", "RECOVERING"],
    [false, "OPEN", "UNREACHABLE"],
  ] as const)("reachable=%s breaker=%s shows %s", (daemonReachable, breakerState, label) => {
    render(<Footer {...base} daemonReachable={daemonReachable} breakerState={breakerState} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });
});
