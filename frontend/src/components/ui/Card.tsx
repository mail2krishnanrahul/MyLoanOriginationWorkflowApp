import { type HTMLAttributes } from 'react';
import { cn } from '@/lib/cn';

/** Shared card component that keeps section styling consistent across workbench pages. */
export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('panel-glass p-5', className)} {...props} />;
}

export function CardHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('mb-4 flex items-start justify-between gap-4', className)} {...props} />;
}

export function CardTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h3
      className={cn('text-sm font-semibold tracking-tight text-neutral-900 dark:text-neutral-50', className)}
      {...props}
    />
  );
}

export function CardDescription({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn('text-sm text-neutral-500 dark:text-neutral-400', className)} {...props} />;
}
