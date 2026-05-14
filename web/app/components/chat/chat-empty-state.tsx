import { MessageSquare } from "lucide-react";

export function ChatEmptyState({
  setInput,
}: {
  setInput: (v: string) => void;
}) {
  const suggestions = [
    "Set up a personal website",
    "Audit my server security",
    "Write a marketing script",
  ];

  return (
    <div className="flex flex-col items-center justify-center h-full text-center py-20">
      <div className="w-16 h-16 bg-panel border border-border rounded-3xl flex items-center justify-center text-accent mb-6 shadow-xl">
        <MessageSquare size={32} />
      </div>

      <h2 className="text-2xl font-bold text-text mb-2">
        Digital Workforce Portal
      </h2>

      <p className="text-text-dim text-sm max-w-sm">
        Enter your business intent. Our agent logic will plan, delegate, and execute autonomously.
      </p>

      <div className="mt-8 grid grid-cols-1 sm:grid-cols-3 gap-2 w-full max-w-2xl px-4">
        {suggestions.map((t) => (
          <button
            key={t}
            onClick={() => setInput(t)}
            className="px-4 py-2 bg-panel border border-border rounded-lg text-[11px] text-text-dim hover:border-accent hover:text-text transition-all text-center uppercase tracking-wider font-bold"
          >
            {t}
          </button>
        ))}
      </div>
    </div>
  );
}
