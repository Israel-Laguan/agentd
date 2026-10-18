import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { BoardNotices } from "./board-notices";

describe("BoardNotices", () => {
  it("names a failed project with its error and a capped project", () => {
    render(
      <BoardNotices
        unreachable={false}
        issues={{ failedProjects: [{ projectId: "pb", message: "db locked" }], cappedProjects: ["big"] }}
      />
    );
    expect(screen.getByRole("alert")).toHaveTextContent("project pb: db locked");
    expect(screen.getByRole("status")).toHaveTextContent("project big has more tasks");
  });

  it("renders nothing when the board is complete", () => {
    const { container } = render(
      <BoardNotices unreachable={false} issues={{ failedProjects: [], cappedProjects: [] }} />
    );
    expect(container).toBeEmptyDOMElement();
  });
});
