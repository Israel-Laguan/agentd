import { Plus } from "lucide-react";
import type { DaemonState } from "@/app/hooks/use-dashboard";
import { SystemStatus } from "@/lib/types";
import { BreakerBadge } from "./breaker-badge";

const DAEMON: Record<DaemonState, { text: string; dot: string }> = {
  connecting: { text: "Connecting to daemon", dot: "bg-text-dim" },
  online: { text: "Daemon online", dot: "bg-accent shadow-[0_0_8px_var(--color-accent)] animate-pulse" },
  unreachable: { text: "Daemon unreachable", dot: "bg-error" },
};

interface HeaderProps {
  daemon: DaemonState;
  // mock is true when the UI shows bundled sample data instead of the daemon.
  mock: boolean;
  status: SystemStatus | null;
  onStartNewIntake: () => void;
}

export const Header = ({ daemon, mock, status, onStartNewIntake }: HeaderProps) => {
    return (
         <header className="h-12 border-b border-border bg-panel px-6 flex items-center justify-between">
          <div className="flex items-center gap-3">
            {mock ? (
              <span
                role="status"
                className="px-2 py-0.5 rounded border border-warning/30 bg-warning/10 text-[10px] font-bold uppercase tracking-[0.15em] text-warning"
              >
                Mock mode — sample data, not the daemon
              </span>
            ) : (
              <div className="flex items-center gap-2">
                <div className={`w-1.5 h-1.5 rounded-full ${DAEMON[daemon].dot}`} />
                <span className="text-[10px] font-bold uppercase tracking-[0.15em] text-text-dim">{DAEMON[daemon].text}</span>
              </div>
            )}
            <BreakerBadge status={status} />
          </div>

          <div className="flex items-center gap-3">
             <button
              type="button"
              onClick={onStartNewIntake}
              className="px-3 py-1 text-[11px] font-bold text-white bg-accent rounded hover:bg-accent-hover transition-all flex items-center gap-1.5 shadow-sm"
            >
                <Plus size={14} /> NEW INTAKE
             </button>
          </div>
        </header>
    )
};
