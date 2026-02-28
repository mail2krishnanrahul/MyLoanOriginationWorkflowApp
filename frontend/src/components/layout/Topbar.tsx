import { Bell, MoonStar, Search, Sun } from 'lucide-react';
import { Breadcrumbs } from '@/components/navigation/Breadcrumbs';
import { Button } from '@/components/ui/Button';
import { TextInput } from '@/components/ui/FormField';
import { useUiStore } from '@/store/ui-store';

export function Topbar() {
  const theme = useUiStore((state) => state.theme);
  const setTheme = useUiStore((state) => state.setTheme);

  return (
    <header className="sticky top-0 z-30 border-b bg-white/80 px-4 py-3 backdrop-blur-xl dark:border-white/5 dark:bg-[#0a1120]/80 md:px-6"
      style={{ borderColor: 'rgb(var(--border))' }}
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        {/* Left: breadcrumbs + search */}
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <Breadcrumbs />
          <div className="relative w-full max-w-sm">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-neutral-400" aria-hidden="true" />
            <TextInput
              placeholder="Search cases, tasks, borrowers…"
              aria-label="Global search"
              className="h-9 rounded-xl border-neutral-200/80 bg-neutral-50/80 pl-9 text-sm placeholder:text-neutral-400 focus:bg-white dark:border-white/10 dark:bg-white/5 dark:focus:bg-white/8"
            />
          </div>
        </div>

        {/* Right: actions + avatar */}
        <div className="flex items-center gap-1.5">
          <Button
            variant="ghost"
            size="sm"
            aria-label="Toggle colour theme"
            onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
            className="size-9 rounded-xl p-0"
          >
            {theme === 'dark'
              ? <Sun className="size-4 text-amber-400" aria-hidden="true" />
              : <MoonStar className="size-4" aria-hidden="true" />}
          </Button>

          <Button
            variant="ghost"
            size="sm"
            aria-label="Notifications"
            className="relative size-9 rounded-xl p-0"
          >
            <Bell className="size-4" aria-hidden="true" />
            {/* Notification dot */}
            <span className="absolute right-2 top-2 size-1.5 rounded-full bg-sky-400 ring-2 ring-white dark:ring-[#0a1120]" />
          </Button>

          {/* User avatar */}
          <button
            type="button"
            className="ml-1 flex items-center gap-2.5 rounded-xl border border-neutral-200 px-2.5 py-1.5 text-sm font-medium text-neutral-700 transition-all hover:border-neutral-300 hover:bg-neutral-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-300 dark:border-white/10 dark:text-slate-200 dark:hover:bg-white/5"
            aria-label="Open user menu"
          >
            <span
              className="flex size-6 shrink-0 items-center justify-center rounded-lg text-[11px] font-bold text-white"
              style={{ background: 'linear-gradient(135deg, #0f82ff 0%, #7c3aed 100%)' }}
              aria-hidden="true"
            >
              AL
            </span>
            <span className="hidden sm:inline">Alex Lane</span>
          </button>
        </div>
      </div>
    </header>
  );
}
