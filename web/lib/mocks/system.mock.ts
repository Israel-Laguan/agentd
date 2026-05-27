import { SystemStatus } from "@/lib/types";

export const mockSystemStatus: SystemStatus = {
  total_token_usage: 128_450,
  rolling_budget_enabled: true,
  rolling_token_limit: 500_000,
  rolling_token_remaining: 371_550,
  rolling_token_window: "5h0m0s",
  status: {
    kind: "status_report",
    message: "Mock status",
    summary: {
      total_projects: 1,
      tasks_by_state: { RUNNING: 2, COMPLETED: 1 },
    },
  },
};
