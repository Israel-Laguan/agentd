import { Dispatch, SetStateAction, useSyncExternalStore } from "react";
import { MessageSquare, LayoutDashboard, Users, BookOpen, Terminal, Settings, PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { SidebarItem } from "@/app/components/sidebar-item";
import { motion } from "framer-motion";
import { WorkforceState } from "@/lib/types";
import { cn } from "@/lib/utils";

// SIDEBAR_COLLAPSED_KEY is the localStorage key that remembers the operator's choice.
export const SIDEBAR_COLLAPSED_KEY = "agentd.sidebar.collapsed";

interface SidebarProps {
    activeTab: string;
    setActiveTab: Dispatch<SetStateAction<string>>;
    workforce: WorkforceState | null;
}

// The collapsed flag lives in localStorage. Storage can be blocked (private
// mode, site settings), so the last value set is also kept here: the toggle
// still works for this page, it just is not remembered.
const collapsedListeners = new Set<() => void>();
let collapsedFallback = false;
let collapsedPersistFailed = false;

const readCollapsed = () => {
    // A failed write still leaves getItem working (quota-exceeded, read-only
    // storage), so prefer the value the operator just set until a write sticks.
    if (collapsedPersistFailed) {
        return collapsedFallback;
    }
    try {
        return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "true";
    } catch {
        return collapsedFallback;
    }
};

const writeCollapsed = (value: boolean) => {
    collapsedFallback = value;
    try {
        window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(value));
        collapsedPersistFailed = false;
    } catch {
        // Not persisted; readCollapsed serves collapsedFallback until a write
        // succeeds, rather than the older value storage still returns.
        collapsedPersistFailed = true;
    }
    collapsedListeners.forEach((listener) => listener());
};

const subscribeCollapsed = (listener: () => void) => {
    collapsedListeners.add(listener);
    window.addEventListener("storage", listener);
    return () => {
        collapsedListeners.delete(listener);
        window.removeEventListener("storage", listener);
    };
};

// useCollapsed renders expanded on the server and during hydration, then the
// stored value, so the server HTML and the first client render match.
const useCollapsed = (): [boolean, () => void] => {
    const collapsed = useSyncExternalStore(subscribeCollapsed, readCollapsed, () => false);
    return [collapsed, () => writeCollapsed(!collapsed)];
};

const loadPercent = (workforce: WorkforceState) =>
    workforce.max_workers > 0 ? Math.min(100, (workforce.active_workers / workforce.max_workers) * 100) : 0;

export const Sidebar = ({ activeTab, setActiveTab, workforce }: SidebarProps) => {
    const [collapsed, toggleCollapsed] = useCollapsed();
    const ToggleIcon = collapsed ? PanelLeftOpen : PanelLeftClose;
    const toggleLabel = collapsed ? "Expand sidebar" : "Collapse sidebar";
    return (
        <aside className={cn(
            "border-r border-border bg-panel flex flex-col transition-all duration-200 overflow-hidden",
            collapsed ? "w-16" : "w-60",
        )}>
        <div className={cn("py-4", collapsed ? "px-2" : "px-4")}>
          <div className={cn("flex items-center gap-3 mb-8", collapsed ? "flex-col" : "px-2")}>
            <div className="w-7 h-7 shrink-0 bg-accent rounded flex items-center justify-center text-white font-bold text-sm shadow-[0_0_15px_rgba(35,134,54,0.3)]">A</div>
            {!collapsed && (
              <>
                <h1 className="font-bold text-lg tracking-tight text-text">agentd</h1>
                <span className="ml-auto text-[9px] font-bold bg-accent/20 text-accent px-1.5 py-0.5 rounded-full border border-accent/30">Stable</span>
              </>
            )}
            <button
              type="button"
              onClick={toggleCollapsed}
              aria-label={toggleLabel}
              aria-expanded={!collapsed}
              title={toggleLabel}
              className="p-1 rounded text-text-dim hover:text-text hover:bg-bg/50 transition-colors"
            >
              <ToggleIcon size={16} />
            </button>
          </div>

          <nav className="space-y-1">
            <SidebarItem icon={MessageSquare} label="Intake Console" collapsed={collapsed} active={activeTab === 'chat'} onClick={() => setActiveTab('chat')} />
            <SidebarItem icon={LayoutDashboard} label="Workforce Board" collapsed={collapsed} active={activeTab === 'board'} onClick={() => setActiveTab('board')} />
            <SidebarItem icon={Users} label="Digital Workers" collapsed={collapsed} active={activeTab === 'workforce'} onClick={() => setActiveTab('workforce')} />
            <SidebarItem icon={BookOpen} label="Knowledge Index" collapsed={collapsed} active={activeTab === 'knowledge'} onClick={() => setActiveTab('knowledge')} />
            <SidebarItem icon={Terminal} label="System Kernel" collapsed={collapsed} active={activeTab === 'logs'} onClick={() => setActiveTab('logs')} />
          </nav>
        </div>

        <div className={cn("mt-auto py-4 space-y-4", collapsed ? "px-2" : "px-4")}>
          {workforce && (collapsed ? (
            <div
              role="status"
              aria-label={`Load Status ${workforce.active_workers}/${workforce.max_workers}`}
              title={`Load Status ${workforce.active_workers}/${workforce.max_workers}`}
              className="mx-auto w-9 py-1 rounded-full border border-border bg-bg/50 text-center text-[10px] font-mono text-text"
            >
              {workforce.active_workers}/{workforce.max_workers}
            </div>
          ) : (
            <div className="p-3 bg-bg/50 rounded-lg border border-border">
              <div className="flex justify-between items-center mb-1.5">
                <span className="text-[9px] font-bold text-text-dim uppercase tracking-widest">Load Status</span>
                <span className="text-[10px] font-mono text-text">{workforce.active_workers}/{workforce.max_workers}</span>
              </div>
              <div className="h-1 bg-border rounded-full overflow-hidden">
                <motion.div
                   initial={{ width: 0 }}
                   animate={{ width: `${loadPercent(workforce)}%` }}
                   className="h-full bg-blue"
                />
              </div>
            </div>
          ))}
          <SidebarItem icon={Settings} label="System Config" collapsed={collapsed} active={activeTab === 'settings'} onClick={() => setActiveTab('settings')} />
        </div>
      </aside>
    )
};
