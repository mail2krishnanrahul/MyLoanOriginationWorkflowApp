import { type ReactNode } from 'react';
import { Button } from '@/components/ui/Button';

interface EmptyStateProps {
  title: string;
  description: string;
  icon: ReactNode;
  actionLabel?: string;
  onAction?: () => void;
}

export function EmptyState({ title, description, icon, actionLabel, onAction }: EmptyStateProps) {
  return (
    <div className="panel flex min-h-72 flex-col items-center justify-center gap-4 p-10 text-center">
      <div className="rounded-2xl bg-brand-50 p-4 text-brand-700 dark:bg-brand-500/15 dark:text-brand-200">
        {icon}
      </div>
      <div className="space-y-2">
        <h3 className="text-lg font-semibold text-neutral-900 dark:text-neutral-50">{title}</h3>
        <p className="max-w-md text-sm text-neutral-500 dark:text-neutral-300">{description}</p>
      </div>
      {actionLabel && onAction ? (
        <Button variant="secondary" onClick={onAction}>
          {actionLabel}
        </Button>
      ) : null}
    </div>
  );
}
