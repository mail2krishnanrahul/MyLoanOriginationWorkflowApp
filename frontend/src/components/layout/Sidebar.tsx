import {
  BriefcaseBusiness,
  ChevronsLeft,
  ChevronsRight,
  LayoutDashboard,
  ShieldCheck,
  Layers
} from 'lucide-react';
import type { ComponentType } from 'react';
import { NavLink } from 'react-router-dom';
import { cn } from '@/lib/cn';
import { useUiStore } from '@/store/ui-store';

interface NavItem {
  label: string;
  path: string;
  icon: ComponentType<{ className?: string }>;
  badge?: string;
}

const navItems: NavItem[] = [
  { label: 'Dashboard', path: '/dashboard', icon: LayoutDashboard },
  { label: 'My Cases', path: '/cases', icon: BriefcaseBusiness },
  { label: 'Workbaskets', path: '/workbaskets', icon: Layers, badge: 'Live' },
  { label: 'Admin Hub', path: '/admin', icon: ShieldCheck }
];

export function Sidebar() {
  const collapsed = useUiStore((state) => state.sidebarCollapsed);
  const toggleSidebar = useUiStore((state) => state.toggleSidebar);

  return (
    <aside
      className={cn(
        'relative flex flex-col py-5 transition-all duration-300 ease-in-out',
        'border-r border-white/5',
        collapsed ? 'w-[72px]' : 'w-60'
      )}
      style={{ background: 'linear-gradient(180deg, var(--sidebar-grad-from) 0%, var(--sidebar-grad-to) 100%)' }}
    >
      {/* Brand */}
      <div className={cn('mb-8 flex items-center gap-3 px-4', collapsed && 'justify-center px-2')}>
        <div
          className="flex size-8 shrink-0 items-center justify-center rounded-xl"
          style={{
            background: 'linear-gradient(135deg, #0f82ff 0%, #7c3aed 100%)',
            boxShadow: '0 0 16px rgb(15 130 255 / 0.45)'
          }}
        >
          <BriefcaseBusiness className="size-4 text-white" aria-hidden="true" />
        </div>
        {!collapsed && (
          <div className="min-w-0">
            <p className="text-[10px] font-bold uppercase tracking-widest" style={{ color: `rgb(var(--sidebar-accent))` }}>
              LoanFlow
            </p>
            <p className="truncate text-sm font-semibold" style={{ color: `rgb(var(--sidebar-fg))` }}>
              Origination Hub
            </p>
          </div>
        )}
      </div>

      {/* Nav */}
      <nav aria-label="Primary navigation" className="flex-1 space-y-0.5 px-2">
        {navItems.map((item) => (
          <NavLink
            key={item.path}
            to={item.path}
            className={({ isActive }) =>
              cn(
                'group relative flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition-all duration-150',
                collapsed && 'justify-center px-0',
                isActive
                  ? 'bg-white/10'
                  : 'hover:bg-white/5'
              )
            }
            style={({ isActive }) => ({
              color: isActive
                ? `rgb(var(--sidebar-fg))`
                : `rgb(var(--sidebar-fg) / 0.55)`,
            })}
          >
            {({ isActive }) => (
              <>
                {/* Active indicator */}
                {isActive && (
                  <span
                    className="absolute left-0 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-r-full"
                    style={{ background: 'linear-gradient(180deg, var(--sidebar-active-from) 0%, var(--sidebar-active-to) 100%)' }}
                  />
                )}
                <item.icon
                  className={cn(
                    'size-4 shrink-0 transition-colors',
                    collapsed && 'size-5'
                  )}
                  aria-hidden="true"
                />
                {!collapsed && (
                  <>
                    <span className="min-w-0 flex-1 truncate">{item.label}</span>
                    {item.badge && (
                      <span
                        className="rounded-md px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide"
                        style={{
                          backgroundColor: `rgb(var(--sidebar-accent) / 0.15)`,
                          color: `rgb(var(--sidebar-accent))`,
                        }}
                      >
                        {item.badge}
                      </span>
                    )}
                  </>
                )}
              </>
            )}
          </NavLink>
        ))}
      </nav>

      {/* Divider */}
      <div className="mx-4 mb-3 mt-4 h-px bg-white/5" />

      {/* Collapse toggle */}
      <div className={cn('px-2', collapsed && 'flex justify-center')}>
        <button
          type="button"
          onClick={toggleSidebar}
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          className="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm transition hover:bg-white/5"
          style={{ color: `rgb(var(--sidebar-fg) / 0.45)` }}
        >
          {collapsed ? (
            <ChevronsRight className="size-4" />
          ) : (
            <>
              <ChevronsLeft className="size-4 shrink-0" />
              <span className="text-xs">Collapse</span>
            </>
          )}
        </button>
      </div>
    </aside>
  );
}
