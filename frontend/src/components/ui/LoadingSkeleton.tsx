import { type HTMLAttributes } from 'react';
import { cn } from '@/lib/cn';

export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        'animate-shimmer rounded-md bg-gradient-to-r from-neutral-200 via-neutral-100 to-neutral-200 bg-[length:220%_100%] dark:from-neutral-800 dark:via-neutral-700 dark:to-neutral-800',
        className
      )}
      {...props}
    />
  );
}

export function SkeletonCard({ className }: { className?: string }) {
  return (
    <div className={cn('panel p-5', className)}>
      <Skeleton className="mb-3 h-4 w-1/4" />
      <Skeleton className="h-20 w-full" />
    </div>
  );
}

export function TableSkeleton({ rows = 8 }: { rows?: number }) {
  return (
    <div className="panel overflow-hidden p-0">
      <div className="grid grid-cols-10 gap-3 border-b border-neutral-200 p-4 dark:border-neutral-700">
        {Array.from({ length: 10 }).map((_, index) => (
          <Skeleton key={index} className="h-3" />
        ))}
      </div>
      <div className="space-y-3 p-4">
        {Array.from({ length: rows }).map((_, index) => (
          <Skeleton key={index} className="h-10 w-full" />
        ))}
      </div>
    </div>
  );
}
