export const Footer = () => {
  return (
    <footer className="fixed bottom-0 right-0 left-60 h-8 border-t border-border bg-panel flex items-center justify-between px-6 z-10">
         <div className="flex gap-6 items-center">
            <div className="flex items-center gap-1.5">
               <span className="text-[9px] font-bold text-text-dim uppercase tracking-widest">Daemon</span>
               <span className="text-[9px] font-bold text-accent">RUNNING</span>
            </div>
            <div className="flex items-center gap-1.5">
               <span className="text-[9px] font-bold text-text-dim uppercase tracking-widest">Workspace</span>
               <span className="text-[9px] font-mono text-text">~/.agentd/projects/</span>
            </div>
         </div>
         <div className="flex gap-6 items-center text-[9px] font-mono text-text-dim">
            <span>THREADS: 6/8</span>
            <span>UPTIME: 14h 22m</span>
            <span>STORAGE: 1.2GB Free</span>
         </div>
      </footer>
  )
};