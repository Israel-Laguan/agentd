import { ChatMessage } from "@/lib/types";

export async function mockChat(message: string): Promise<ChatMessage> {
  await new Promise((r) => setTimeout(r, 500));

  return {
    role: "assistant",
    content: `Mock response to: ${message}`,
  };
}