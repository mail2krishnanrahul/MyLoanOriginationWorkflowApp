import { useRef, type KeyboardEvent } from 'react';
import { cn } from '@/lib/cn';

export interface TabItem {
  key: string;
  label: string;
  disabled?: boolean;
}

interface TabsProps {
  items: TabItem[];
  value: string;
  onChange: (key: string) => void;
}

export function Tabs({ items, value, onChange }: TabsProps) {
  const refs = useRef<Array<HTMLButtonElement | null>>([]);

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const enabledTabs = items.filter((item) => !item.disabled);
    const currentIndex = enabledTabs.findIndex((item) => item.key === value);

    if (event.key === 'ArrowRight') {
      event.preventDefault();
      const next = enabledTabs[(currentIndex + 1) % enabledTabs.length];
      onChange(next.key);
      refs.current[items.findIndex((item) => item.key === next.key)]?.focus();
    }

    if (event.key === 'ArrowLeft') {
      event.preventDefault();
      const next = enabledTabs[(currentIndex - 1 + enabledTabs.length) % enabledTabs.length];
      onChange(next.key);
      refs.current[items.findIndex((item) => item.key === next.key)]?.focus();
    }
  };

  return (
    <div
      role="tablist"
      aria-label="Case detail tabs"
      onKeyDown={onKeyDown}
      className="border-b border-neutral-200 dark:border-neutral-700"
    >
      <div className="flex flex-wrap gap-6">
        {items.map((tab, index) => {
          const active = tab.key === value;

          return (
            <button
              key={tab.key}
              ref={(element) => {
                refs.current[index] = element;
              }}
              role="tab"
              aria-selected={active}
              aria-controls={`${tab.key}-panel`}
              id={`${tab.key}-tab`}
              type="button"
              disabled={tab.disabled}
              onClick={() => onChange(tab.key)}
              className={cn(
                'relative -mb-px border-b-2 pb-3 pt-2 text-sm font-medium transition-colors',
                active
                  ? 'border-brand-500 text-brand-700 dark:border-brand-300 dark:text-brand-200'
                  : 'border-transparent text-neutral-500 hover:text-neutral-900 dark:text-neutral-300 dark:hover:text-white',
                tab.disabled ? 'cursor-not-allowed opacity-50' : ''
              )}
            >
              {tab.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}
