import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { addTaskComment, fetchTaskComments } from "@/lib/api";
import { ApiError } from "@/lib/http";
import { CommentPanel } from "./comment-panel";

vi.mock("@/lib/api", () => ({ addTaskComment: vi.fn(), fetchTaskComments: vi.fn() }));
const mockAdd = vi.mocked(addTaskComment);

beforeEach(() => {
  vi.mocked(fetchTaskComments).mockReset().mockResolvedValue([]);
  mockAdd.mockReset();
  Element.prototype.scrollIntoView = vi.fn();
});

describe("CommentPanel failed post", () => {
  it("keeps the typed text and shows the daemon's message", async () => {
    mockAdd.mockRejectedValue(new ApiError("comment is too long", 422, "VALIDATION_FAILED"));
    render(<CommentPanel taskId="t1" />);

    const box = screen.getByPlaceholderText("Write a comment...");
    await userEvent.type(box, "a long thought");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("comment is too long");
    expect(box).toHaveValue("a long thought");
    expect(screen.queryByText("a long thought", { selector: "p, div" })).not.toBeInTheDocument();
  });

  it("clears the text and any earlier error after a successful post", async () => {
    mockAdd.mockRejectedValueOnce(new ApiError("daemon busy", 503));
    render(<CommentPanel taskId="t1" />);
    const box = screen.getByPlaceholderText("Write a comment...");
    await userEvent.type(box, "hello");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByRole("alert");

    mockAdd.mockResolvedValueOnce({
      id: "c1",
      task_id: "t1",
      message: "hello",
      created_at: "2026-10-17T00:00:00Z",
      author: { id: "me", name: "You" },
    });
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(box).toHaveValue(""));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("CommentPanel live refresh", () => {
  it("re-reads comments when refreshKey changes", async () => {
    const mockFetch = vi.mocked(fetchTaskComments);
    const { rerender } = render(<CommentPanel taskId="t1" refreshKey={0} />);
    await waitFor(() => expect(mockFetch).toHaveBeenCalledTimes(1));

    mockFetch.mockResolvedValue([
      { id: "c9", task_id: "t1", message: "arrived over SSE", created_at: "2026-10-19T10:00:00Z", author: { id: "u", name: "Ana" } },
    ]);
    rerender(<CommentPanel taskId="t1" refreshKey={1} />);

    expect(await screen.findByText("arrived over SSE")).toBeInTheDocument();
    expect(mockFetch).toHaveBeenCalledTimes(2);
  });
});
