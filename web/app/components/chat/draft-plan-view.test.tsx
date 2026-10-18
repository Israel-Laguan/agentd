import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ChatMessage, DraftPlan, MaterializeResult } from "@/lib/types";
import type { ChatSettings } from "@/lib/chat-settings";
import { DraftPlanView } from "./draft-plan-view";
import { useChatSession } from "./use-chat-session";

const postApprovePlan = vi.fn<(plan: DraftPlan) => Promise<MaterializeResult>>();

vi.mock("@/lib/api", () => ({
  sendChat: vi.fn(),
  postApprovePlan: (plan: DraftPlan) => postApprovePlan(plan),
}));

const settings: ChatSettings = { provider: "", model: "", effort: "medium" };

const plan: DraftPlan = {
  name: "Inventory",
  description: "Track stock",
  tasks: [{ id: "t1", title: "Schema", description: "Tables" }],
};

function Harness() {
  const [, setMessages] = useState<ChatMessage[]>([]);
  const [draftPlan, setDraftPlan] = useState<DraftPlan | null>(plan);
  const { approvePlan, approving } = useChatSession(
    settings,
    setMessages,
    draftPlan,
    setDraftPlan,
    () => {},
  );
  if (!draftPlan) return <p>deployed</p>;
  return (
    <DraftPlanView plan={draftPlan} approving={approving} onApprove={approvePlan} onReplan={() => {}} />
  );
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("DraftPlanView approve", () => {
  beforeEach(() => postApprovePlan.mockReset());

  it("sends one approval for a double click and disables the button while in flight", async () => {
    const pending = deferred<MaterializeResult>();
    postApprovePlan.mockReturnValue(pending.promise);
    render(<Harness />);

    const button = screen.getByRole("button", { name: /execute strategy/i });
    fireEvent.click(button);
    fireEvent.click(button);

    expect(postApprovePlan).toHaveBeenCalledTimes(1);
    const busy = await screen.findByRole("button", { name: /deploying/i });
    expect(busy).toBeDisabled();

    pending.resolve({ projectId: "p1", taskIds: [] });
    await screen.findByText("deployed");
  });

  it("re-enables the button when the approval fails", async () => {
    postApprovePlan.mockRejectedValueOnce(new Error("daemon down"));
    vi.spyOn(console, "error").mockImplementation(() => {});
    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: /execute strategy/i }));

    const button = await screen.findByRole("button", { name: /execute strategy/i });
    await waitFor(() => expect(button).toBeEnabled());
    postApprovePlan.mockResolvedValueOnce({ projectId: "p1", taskIds: [] });
    fireEvent.click(button);
    expect(postApprovePlan).toHaveBeenCalledTimes(2);
  });
});
