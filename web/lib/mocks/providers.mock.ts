import { Provider } from "@/lib/types";

export const mockProviders: Provider[] = [
  { name: "openai", adapter: "openai", models: ["gpt-4o", "gpt-4.1"] },
  { name: "anthropic", adapter: "anthropic", models: ["claude-3.5-sonnet"] },
  { name: "local", adapter: "ollama", models: ["llama-3", "mistral"] },
];
