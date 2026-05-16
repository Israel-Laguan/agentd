export function ChatTyping() {
  return (
    <div className="flex gap-3">
      <div className="w-7 h-7 rounded bg-accent text-white flex items-center justify-center animate-pulse text-[10px] font-bold">
        SYS
      </div>

      <div className="px-4 py-2 rounded-xl bg-bg border border-border shadow-sm flex items-center">
        <div className="flex gap-1">
          <div className="w-1 h-1 bg-accent rounded-full animate-bounce [animation-delay:-0.3s]" />
          <div className="w-1 h-1 bg-accent rounded-full animate-bounce [animation-delay:-0.15s]" />
          <div className="w-1 h-1 bg-accent rounded-full animate-bounce" />
        </div>
      </div>
    </div>
  );
}
