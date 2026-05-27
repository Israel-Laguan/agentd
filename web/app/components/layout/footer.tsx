import { formatTokenCount } from "@/lib/format";

interface FooterProps {
  daemonReachable: boolean;
  totalTokens: number | null;
  runningTasks: number | null;
  rollingBudget?: {
    limit: number;
    remaining: number;
  } | null;
}

export const Footer = ({
  daemonReachable,
  totalTokens,
  runningTasks,
  rollingBudget,
}: FooterProps) => {
  const tokenLabel =
    totalTokens === null ? "—" : formatTokenCount(totalTokens);
  const runningLabel = runningTasks === null ? "—" : String(runningTasks);
  const budgetLabel =
    rollingBudget && rollingBudget.limit > 0
      ? `${formatTokenCount(rollingBudget.remaining)}/${formatTokenCount(rollingBudget.limit)}`
      : null;

  return (
    <footer className="fixed bottom-0 right-0 left-60 h-8 border-t border-border bg-panel flex items-center justify-between px-6 z-10">
      <div className="flex gap-6 items-center">
        <div className="flex items-center gap-1.5">
          <span className="text-[9px] font-bold text-text-dim uppercase tracking-widest">Daemon</span>
          <span className={`text-[9px] font-bold ${daemonReachable ? "text-accent" : "text-error"}`}>
            {daemonReachable ? "RUNNING" : "UNREACHABLE"}
          </span>
        </div>
      </div>
      <div className="flex gap-6 items-center text-[9px] font-mono text-text-dim">
        <span>RUNNING: {runningLabel}</span>
        <span>TOKENS: {tokenLabel}</span>
        {budgetLabel && <span>BUDGET: {budgetLabel}</span>}
      </div>
    </footer>
  );
};
