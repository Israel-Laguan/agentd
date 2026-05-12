import { cn } from "@/lib/types";

interface SidebarItemProps {
  icon: React.ComponentType<{ size: number; className?: string }>;
  label: string;
  active?: boolean;
  onClick?: () => void;
}

export const SidebarItem = ({ icon: Icon, label, active, onClick }: SidebarItemProps) => (
  <button
    type="button"
    role="button"
    onClick={onClick}
    className={cn(
      "w-full flex items-center gap-3 px-4 py-2.5 rounded-md transition-all duration-150 text-xs",
      active 
        ? "bg-text/5 text-text font-semibold border border-border shadow-inner" 
        : "text-text-dim hover:bg-panel hover:text-text"
    )}
  >
    <Icon size={16} className={active ? "text-accent" : "text-text-dim"} />
    <span>{label}</span>
  </button>
);
