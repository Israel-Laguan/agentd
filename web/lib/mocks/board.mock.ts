import { Task, TaskStatus } from "@/lib/types"; // adjust path if needed

export const mockBoard: { tasks: Task[] } = {
  tasks: [
    {
      id: "t1",
      project_id: "p1",
      title: "Design system UI",
      description: "Create base UI components",
      state: TaskStatus.PENDING,
      depends_on: [],
      logs: [],
      created_at: Date.now(),
      updated_at: Date.now(),
      token_usage: 1200,
    },
    {
      id: "t2",
      project_id: "p1",
      title: "Build API integration",
      description: "Connect frontend to backend",
      state: TaskStatus.RUNNING,
      depends_on: ["t1"],
      logs: [
        {
          message: "started execution...",
          timestamp: Date.now(),
        },
      ],
      created_at: Date.now(),
      updated_at: Date.now(),
      token_usage: 8450,
    },
    {
      id: "t3",
      project_id: "p1",
      title: "AI orchestration layer",
      description: "Implement planner logic",
      state: TaskStatus.COMPLETED,
      depends_on: ["t2"],
      logs: [
        {
          message: "completed successfully",
          timestamp: Date.now(),
        },
      ],
      created_at: Date.now(),
      updated_at: Date.now(),
    },
    {
      id: "t4",
      project_id: "p1",
      title: "Deploy system",
      description: "Deploy to production",
      state: TaskStatus.FAILED,
      depends_on: ["t3"],
      logs: [
        {
          message: "deployment failed: missing env vars",
          timestamp: Date.now(),
        },
      ],
      created_at: Date.now(),
      updated_at: Date.now(),
    },
    {
      id: "t5",
      project_id: "p1",
      title: "Retry exhausted worker task",
      description: "Evicted after max retries; needs operator review",
      state: TaskStatus.FAILED_REQUIRES_HUMAN,
      depends_on: ["t4"],
      logs: [
        {
          message: "poison pill: max retries exceeded",
          timestamp: Date.now(),
        },
      ],
      created_at: Date.now(),
      updated_at: Date.now(),
    },
  ],
};