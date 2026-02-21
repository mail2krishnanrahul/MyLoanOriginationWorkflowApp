import { CircleDot, ShieldCheck, TriangleAlert, Workflow } from 'lucide-react';
import type { ComponentType } from 'react';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import type { TimelineEvent } from '@/lib/api/types';
import { formatDateTime } from '@/lib/utils/date';
import { cn } from '@/lib/cn';

const iconByEvent: Record<TimelineEvent['type'], ComponentType<{ className?: string }>> = {
  CASE_CREATED: CircleDot,
  STAGE_CHANGED: Workflow,
  TASK_COMPLETED: ShieldCheck,
  DOCUMENT_UPLOADED: CircleDot,
  APPROVAL_GRANTED: ShieldCheck,
  SLA_WARNING: TriangleAlert,
  COMMENT: CircleDot,
  NOTIFICATION: CircleDot
};

interface CaseTimelineProps {
  events: TimelineEvent[];
}

export function CaseTimeline({ events }: CaseTimelineProps) {
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Timeline</CardTitle>
          <CardDescription>Audit-ready sequence of case events</CardDescription>
        </div>
      </CardHeader>
      <ol className="space-y-4">
        {events.map((event) => {
          const Icon = iconByEvent[event.type] ?? CircleDot;
          return (
            <li key={event.id} className="relative pl-9">
              <span className="absolute left-0 top-1 flex size-6 items-center justify-center rounded-full bg-brand-50 text-brand-700 ring-1 ring-brand-100 dark:bg-brand-500/15 dark:text-brand-200 dark:ring-brand-500/30">
                <Icon className="size-3.5" aria-hidden="true" />
              </span>
              <div className="panel-muted p-3">
                <p className="text-sm font-medium text-neutral-800 dark:text-neutral-100">{event.description}</p>
                <p className="mt-1 text-xs text-neutral-500 dark:text-neutral-300">
                  {event.actor.displayName} | {formatDateTime(event.timestamp)}
                </p>
                {(event.before || event.after) && (
                  <details className="mt-2 text-xs text-neutral-600 dark:text-neutral-300">
                    <summary className="cursor-pointer font-medium text-brand-700 dark:text-brand-200">
                      View before/after changes
                    </summary>
                    <div className={cn('mt-2 grid gap-2 rounded-lg bg-neutral-950 p-3 font-mono text-neutral-100')}>
                      {event.before ? <pre className="overflow-auto">{JSON.stringify(event.before, null, 2)}</pre> : null}
                      {event.after ? <pre className="overflow-auto">{JSON.stringify(event.after, null, 2)}</pre> : null}
                    </div>
                  </details>
                )}
              </div>
            </li>
          );
        })}
      </ol>
    </Card>
  );
}
