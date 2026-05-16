import { MessageSquare, LayoutDashboard, Users, BookOpen, Terminal, Settings } from "lucide-react";
import { SidebarItem } from "@/app/components/sidebar-item";
import { motion } from "framer-motion";

interface SidebarProps {
    activeTab: string;
    setActiveTab: React.Dispatch<React.SetStateAction<string>>;
    workforce: {
        activeWorkers: number;
        maxWorkers: number;
    } | null;
}
export const Sidebar = ({ activeTab, setActiveTab, workforce }: SidebarProps) => {
    return (
        <aside className="w-60 border-r border-border bg-panel flex flex-col">
        <div className="p-4">
          <div className="flex items-center gap-3 mb-8 px-2">
            <div className="w-7 h-7 bg-accent rounded flex items-center justify-center text-white font-bold text-sm shadow-[0_0_15px_rgba(35,134,54,0.3)]">A</div>
            <h1 className="font-bold text-lg tracking-tight text-text">agentd</h1>
            <span className="ml-auto text-[9px] font-bold bg-accent/20 text-accent px-1.5 py-0.5 rounded-full border border-accent/30">Stable</span>
          </div>

          <nav className="space-y-1">
            <SidebarItem icon={MessageSquare} label="Intake Console" active={activeTab === 'chat'} onClick={() => setActiveTab('chat')} />
            <SidebarItem icon={LayoutDashboard} label="Workforce Board" active={activeTab === 'board'} onClick={() => setActiveTab('board')} />
            <SidebarItem icon={Users} label="Digital Workers" active={activeTab === 'workforce'} onClick={() => setActiveTab('workforce')} />
            <SidebarItem icon={BookOpen} label="Knowledge Index" active={activeTab === 'knowledge'} onClick={() => setActiveTab('knowledge')} />
            <SidebarItem icon={Terminal} label="System Kernel" active={activeTab === 'logs'} onClick={() => setActiveTab('logs')} />
          </nav>
        </div>

        <div className="mt-auto p-4 space-y-4">
          {workforce && (
            <div className="p-3 bg-bg/50 rounded-lg border border-border">
              <div className="flex justify-between items-center mb-1.5">
                <span className="text-[9px] font-bold text-text-dim uppercase tracking-widest">Load Status</span>
                <span className="text-[10px] font-mono text-text">{workforce.activeWorkers}/{workforce.maxWorkers}</span>
              </div>
              <div className="h-1 bg-border rounded-full overflow-hidden">
                <motion.div
                   initial={{ width: 0 }}
                   animate={{
                     width: `${
                       workforce.maxWorkers > 0
                         ? Math.min(100, (workforce.activeWorkers / workforce.maxWorkers) * 100)
                         : 0
                     }%`
                   }}
                   className="h-full bg-blue"
                />
              </div>
            </div>
          )}
          <SidebarItem icon={Settings} label="System Config" active={activeTab === 'settings'} onClick={() => setActiveTab('settings')} />
        </div>
      </aside>
    )
};
