import { Bell, Check, Palette, Search } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { Breadcrumbs } from '@/components/navigation/Breadcrumbs';
import { Button } from '@/components/ui/Button';
import { TextInput } from '@/components/ui/FormField';
import { useUiStore } from '@/store/ui-store';
import { themes, type ThemeId } from '@/store/ui-store';

export function Topbar() {
  const theme = useUiStore((state) => state.theme);
  const setTheme = useUiStore((state) => state.setTheme);
  const [open, setOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    function handleClick(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, [open]);

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
          {/* Theme picker */}
          <div className="relative" ref={dropdownRef}>
            <Button
              variant="ghost"
              size="sm"
              aria-label="Change theme"
              aria-expanded={open}
              onClick={() => setOpen((v) => !v)}
              className="size-9 rounded-xl p-0"
            >
              <Palette className="size-4" aria-hidden="true" />
            </Button>

            {open && (
              <div
                className="absolute right-0 top-full z-50 mt-2 w-52 rounded-xl border p-1.5 shadow-lg"
                style={{
                  borderColor: 'rgb(var(--border))',
                  backgroundColor: 'rgb(var(--bg-panel))',
                }}
              >
                {themes.map((t) => (
                  <button
                    key={t.id}
                    type="button"
                    onClick={() => { setTheme(t.id as ThemeId); setOpen(false); }}
                    className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm transition-colors hover:bg-[rgb(var(--bg-muted))]"
                    style={{ color: 'rgb(var(--fg))' }}
                  >
                    <span
                      className="size-4 shrink-0 rounded-full border border-white/20"
                      style={{ backgroundColor: t.accent }}
                    />
                    <span className="flex-1 text-left font-medium">{t.label}</span>
                    {theme === t.id && (
                      <Check className="size-3.5 text-[rgb(var(--ring))]" />
                    )}
                  </button>
                ))}
              </div>
            )}
          </div>

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
