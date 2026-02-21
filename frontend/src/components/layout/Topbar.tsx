import { Bell, MoonStar, Search, Sun, UserRound } from 'lucide-react';
import { useState } from 'react';
import { Breadcrumbs } from '@/components/navigation/Breadcrumbs';
import { Button } from '@/components/ui/Button';
import { TextInput } from '@/components/ui/FormField';
import { useUiStore } from '@/store/ui-store';

export function Topbar() {
  const theme = useUiStore((state) => state.theme);
  const setTheme = useUiStore((state) => state.setTheme);
  const [search, setSearch] = useState('');

  return (
    <header className="sticky top-0 z-30 border-b border-neutral-200 bg-white/85 px-4 py-3 backdrop-blur dark:border-neutral-700 dark:bg-neutral-900/80 md:px-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="space-y-2">
          <Breadcrumbs />
          <div className="relative w-full sm:w-96">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-neutral-400" aria-hidden="true" />
            <TextInput
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search case ref, borrower, task"
              aria-label="Search cases"
              className="pl-9"
            />
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            aria-label="Toggle color theme"
            onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
          >
            {theme === 'dark' ? <Sun className="size-4" aria-hidden="true" /> : <MoonStar className="size-4" aria-hidden="true" />}
          </Button>
          <Button variant="ghost" size="sm" aria-label="Notifications">
            <Bell className="size-4" aria-hidden="true" />
          </Button>
          <button
            type="button"
            className="inline-flex items-center gap-2 rounded-lg border border-neutral-200 px-2.5 py-1.5 text-sm font-medium text-neutral-700 transition hover:bg-neutral-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-300 dark:border-neutral-700 dark:text-neutral-200 dark:hover:bg-neutral-800"
          >
            <UserRound className="size-4" aria-hidden="true" />
            <span>Alex Lane</span>
          </button>
        </div>
      </div>
    </header>
  );
}
