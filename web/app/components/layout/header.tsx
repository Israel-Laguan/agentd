import { Plus } from "lucide-react";

export const Header = () => {
    return (
         <header className="h-12 border-b border-border bg-panel px-6 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              <div className="w-1.5 h-1.5 rounded-full bg-accent shadow-[0_0_8px_var(--color-accent)] animate-pulse" />
              <span className="text-[10px] font-bold uppercase tracking-[0.15em] text-text-dim">Daemon Online</span>
            </div>
            <div className="h-4 w-px bg-border mx-1" />
            <span className="text-[10px] font-mono text-text-dim">REF: EXPR-API-V2</span>
          </div>
          
          <div className="flex items-center gap-3">
             <div className="text-[10px] text-text-dim font-mono hidden md:block">~/.agentd/projects/</div>
             <button className="px-3 py-1 text-[11px] font-bold text-white bg-accent rounded hover:bg-accent-hover transition-all flex items-center gap-1.5 shadow-sm">
                <Plus size={14} /> NEW INTAKE
             </button>
          </div>
        </header>
    )
};