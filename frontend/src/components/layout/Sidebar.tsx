import { BriefcaseBusiness, ChevronsLeftRight, LayoutDashboard, ShieldCheck } from 'lucide-react';
import type { ComponentType } from 'react';
import { NavLink } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
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
  { label: 'Workbaskets', path: '/workbaskets', icon: BriefcaseBusiness, badge: 'Live' },
  { label: 'Admin Hub', path: '/admin', icon: ShieldCheck }
];

export function Sidebar() {
  const collapsed = useUiStore((state) => state.sidebarCollapsed);
  const toggleSidebar = useUiStore((state) => state.toggleSidebar);

  return (
    <aside
      className={cn(
        'border-r border-neutral-200 bg-white/90 px-3 py-4 backdrop-blur transition-all dark:border-neutral-700 dark:bg-neutral-900/80',
        collapsed ? 'w-[88px]' : 'w-64'
      )}
    >
      <div className="mb-8 flex items-center justify-between gap-2 px-2">
        <div className="min-w-0">
          <p className={cn('text-xs font-semibold uppercase tracking-wide text-brand-700 dark:text-brand-200', collapsed ? 'sr-only' : '')}>
            LoanFlow
          </p>
          <h1 className={cn('truncate text-sm font-bold text-neutral-900 dark:text-neutral-100', collapsed ? 'sr-only' : '')}>
            Origination Hub
          </h1>
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={toggleSidebar}
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          <ChevronsLeftRight className="size-4" aria-hidden="true" />
        </Button>
      </div>

      <nav aria-label="Primary navigation" className="space-y-1">
        {navItems.map((item) => (
          <NavLink
            key={item.path}
            to={item.path}
            className={({ isActive }) =>
              cn(
                'group flex items-center gap-3 rounded-xl px-3 py-2 text-sm font-medium transition-all',
                isActive
                  ? 'bg-brand-50 text-brand-700 shadow-sm ring-1 ring-brand-100 dark:bg-brand-500/10 dark:text-brand-200 dark:ring-brand-500/20'
                  : 'text-neutral-600 hover:bg-neutral-100 hover:text-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800 dark:hover:text-neutral-100'
              )
            }
          >
            <item.icon className="size-4 shrink-0" aria-hidden="true" />
            <span className={cn('min-w-0 truncate', collapsed && 'hidden')}>{item.label}</span>
            {item.badge && !collapsed ? (
              <span className="ml-auto rounded-md bg-accent-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-accent-900 dark:bg-accent-500/20 dark:text-accent-200">
                {item.badge}
              </span>
            ) : null}
          </NavLink>
        ))}
      </nav>
    </aside>
  );
}
