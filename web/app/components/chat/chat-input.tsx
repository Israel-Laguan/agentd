"use client";

import { Send, ChevronRight } from "lucide-react";

export function ChatInput({
  value,
  setValue,
  onSend,
  isTyping,
  inputRef,
}: {
  value: string;
  setValue: (v: string) => void;
  onSend: () => void;
  isTyping: boolean;
  inputRef?: React.RefObject<HTMLInputElement | null>;
}) {
  return (
    <div className="p-1 px-1.5 bg-panel border border-border rounded-xl shadow-2xl flex items-center gap-2 backdrop-blur-sm">
      <div className="w-8 h-8 flex items-center justify-center text-text-dim/50">
        <ChevronRight size={18} />
      </div>

      <input
        ref={inputRef}
        type="text"
        aria-label="Chat input"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key !== "Enter") return;
          if (e.nativeEvent.isComposing || isTyping) return;
          onSend();
        }}
        placeholder="Input organizational goal..."
        className="flex-1 bg-transparent border-none focus:ring-0 text-sm py-3 text-text placeholder:text-text-dim/30"
      />

      <button
        type="button"
        aria-label="Send message"
        onClick={() => !isTyping && onSend()}
        disabled={isTyping || !value.trim()}
        className="h-9 px-4 bg-accent text-white rounded-lg flex items-center justify-center hover:bg-accent-hover transition-all mr-1 shadow-md disabled:opacity-50 disabled:cursor-not-allowed"
      >
        <Send size={16} />
      </button>
    </div>
  );
}
