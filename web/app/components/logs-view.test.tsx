import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { useToolEventStream } from "@/app/hooks/use-tool-event-stream";
import { LogsView } from "./logs-view";

vi.mock("@/app/hooks/use-tool-event-stream", () => ({ useToolEventStream: vi.fn() }));
const mockStream = vi.mocked(useToolEventStream);

describe("LogsView stream indicator", () => {
  it.each([
    [true, "Stream live"],
    [false, "Stream disconnected"],
  ])("connected=%s shows %s, never a fixed System Live", (connected, text) => {
    mockStream.mockReturnValue({ rawEvents: [], entries: [], connected });
    render(<LogsView tasks={[]} />);
    expect(screen.getByText(text, { exact: false })).toBeInTheDocument();
    expect(screen.queryByText(/System Live/)).not.toBeInTheDocument();
  });
});
