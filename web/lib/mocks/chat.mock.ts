import { ChatResponse } from "@/lib/types";

export async function mockChat(
  message: string
): Promise<ChatResponse> {
  await new Promise((r) => setTimeout(r, 500));

  return {
    message: {
      id: `mock-${Date.now()}`,
      role: "assistant",
      content: `Mock response to: ${message}`,
    },

    plan: {
      name: "Build AI Kanban System",
      description: "Generated execution plan",
      tasks: [
        {
          title: "Design architecture",
          description: "Define system structure",
        },
        {
          title: "Build frontend",
          description: "Create Kanban UI",
        },
      ],
    },
  };
}