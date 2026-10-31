import { cn } from "@/lib/utils";

interface SidebarItemProps {
  icon: React.ComponentType<{ size: number; className?: string }>;
  label: string;
  active?: boolean;
  // collapsed shows the icon alone; the label moves to aria-label and a tooltip.
  collapsed?: boolean;
  onClick?: () => void;
}

export const SidebarItem = ({ icon: Icon, label, active, collapsed, onClick }: SidebarItemProps) => (
  <button
    type="button"
    role="button"
    onClick={onClick}
    aria-label={collapsed ? label : undefined}
    title={collapsed ? label : undefined}
    className={cn(
      "w-full flex items-center gap-3 py-2.5 rounded-md transition-all duration-150 text-xs",
      collapsed ? "justify-center px-0" : "px-4",
      active
        ? "bg-text/5 text-text font-semibold border border-border shadow-inner"
        : "text-text-dim hover:bg-panel hover:text-text"
    )}
  >
    <Icon size={16} className={active ? "text-accent" : "text-text-dim"} />
    {!collapsed && <span>{label}</span>}
  </button>
);
