import { Task, TaskStatus } from "@/lib/types"; // adjust path if needed

export const mockBoard: { tasks: Task[] } = {
  tasks: [
    {
      id: "t1",
      projectId: "p1",
      title: "Design system UI",
      description: "Create base UI components",
      status: TaskStatus.PENDING,
      dependsOn: [],
      logs: [],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    },
    {
      id: "t2",
      projectId: "p1",
      title: "Build API integration",
      description: "Connect frontend to backend",
      status: TaskStatus.RUNNING,
      dependsOn: ["t1"],
      logs: [
        {
          message: "started execution...",
          timestamp: Date.now(),
        },
      ],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    },
    {
      id: "t3",
      projectId: "p1",
      title: "AI orchestration layer",
      description: "Implement planner logic",
      status: TaskStatus.COMPLETED,
      dependsOn: ["t2"],
      logs: [
        {
          message: "completed successfully",
          timestamp: Date.now(),
        },
      ],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    },
    {
      id: "t4",
      projectId: "p1",
      title: "Deploy system",
      description: "Deploy to production",
      status: TaskStatus.FAILED,
      dependsOn: ["t3"],
      logs: [
        {
          message: "deployment failed: missing env vars",
          timestamp: Date.now(),
        },
      ],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    },
  ],
};