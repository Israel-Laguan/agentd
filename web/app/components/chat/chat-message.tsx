"use client";

import { ChatMessage } from "@/lib/types";
import { cn } from "@/lib/utils";

export function ChatMessageView({ message }: { message: ChatMessage }) {
  return (
    <div className={cn("flex gap-3", message.role === "user" ? "flex-row-reverse" : "")}>
      <div
        className={cn(
          "w-7 h-7 rounded flex items-center justify-center shrink-0 font-bold text-[10px]",
          message.role === "user"
            ? "bg-border text-text-dim"
            : "bg-accent text-white"
        )}
      >
        {message.role === "user" ? "USR" : "SYS"}
      </div>

      <div
        className={cn(
          "max-w-[85%] px-4 py-3 rounded-xl text-sm leading-relaxed",
          message.role === 'user'
            ? "bg-panel border border-border text-text"
            : "bg-bg border border-border text-text shadow-sm"
        )}
      >
        {message.content}
      </div>
    </div>
  );
}
