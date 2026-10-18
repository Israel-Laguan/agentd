import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { SystemStatus } from "@/lib/types";
import { BreakerBadge } from "./breaker-badge";

const status = (extra: Partial<SystemStatus>): SystemStatus => ({ total_token_usage: 0, ...extra });

describe("BreakerBadge", () => {
  it("renders nothing while every breaker is closed", () => {
    const { container } = render(
      <BreakerBadge
        status={status({
          breaker: { state: "CLOSED", failure_count: 0 },
          provider_breakers: { openai: { state: "CLOSED", failure_count: 0 } },
        })}
      />
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the global breaker open with its last error", () => {
    render(<BreakerBadge status={status({ breaker: { state: "OPEN", failure_count: 3, last_error: "provider 503" } })} />);
    const badge = screen.getByRole("alert");
    expect(badge).toHaveTextContent("Breaker open");
    expect(badge).toHaveAttribute("title", expect.stringContaining("provider 503"));
  });

  it("names each provider whose breaker is not closed", () => {
    render(
      <BreakerBadge
        status={status({
          breaker: { state: "CLOSED", failure_count: 0 },
          provider_breakers: {
            anthropic: { state: "HALF_OPEN", failure_count: 2 },
            openai: { state: "OPEN", failure_count: 5, last_error: "429" },
            ollama: { state: "CLOSED", failure_count: 0 },
          },
        })}
      />
    );
    const badge = screen.getByRole("alert");
    expect(badge).toHaveTextContent("anthropic half-open");
    expect(badge).toHaveTextContent("openai open");
    expect(badge).not.toHaveTextContent("ollama");
  });
});
