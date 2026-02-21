import { type HTMLAttributes } from 'react';
import { cn } from '@/lib/cn';

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand';
}

const variantClasses: Record<NonNullable<BadgeProps['variant']>, string> = {
  neutral: 'bg-neutral-100 text-neutral-700 ring-neutral-200 dark:bg-neutral-800 dark:text-neutral-200',
  success: 'bg-success-50 text-success-700 ring-success-500/20 dark:bg-success-500/15 dark:text-green-300',
  warning: 'bg-warning-50 text-warning-700 ring-warning-500/20 dark:bg-warning-500/15 dark:text-amber-300',
  danger: 'bg-danger-50 text-danger-700 ring-danger-500/20 dark:bg-danger-500/15 dark:text-red-300',
  info: 'bg-info-50 text-info-700 ring-info-500/20 dark:bg-info-500/15 dark:text-blue-300',
  brand: 'bg-brand-50 text-brand-700 ring-brand-500/20 dark:bg-brand-500/15 dark:text-brand-200'
};

export function Badge({ className, variant = 'neutral', ...props }: BadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-semibold ring-1 ring-inset',
        variantClasses[variant],
        className
      )}
      {...props}
    />
  );
}
