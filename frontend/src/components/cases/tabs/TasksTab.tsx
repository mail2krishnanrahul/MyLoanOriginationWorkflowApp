import { useMemo } from 'react';
import { toast } from 'sonner';
import { PriorityBadge } from '@/components/domain/PriorityBadge';
import { SLAIndicator } from '@/components/domain/SLAIndicator';
import { StatusBadge } from '@/components/domain/StatusBadge';
import { Button } from '@/components/ui/Button';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { TableSkeleton } from '@/components/ui/LoadingSkeleton';
import { useCaseTasks, useClaimTask } from '@/hooks/use-tasks';
import type { TaskSummary } from '@/lib/api/types';
import { formatDateTime } from '@/lib/utils/date';

interface TasksTabProps {
  caseId: string;
  onOpenTask: (taskId: string) => void;
}

export function TasksTab({ caseId, onOpenTask }: TasksTabProps) {
  const tasksQuery = useCaseTasks(caseId);
  const claimTask = useClaimTask(caseId);

  const groupedTasks = useMemo(() => {
    const grouped = new Map<string, TaskSummary[]>();

    for (const task of tasksQuery.data ?? []) {
      const key = task.activityName ?? 'General';
      const existing = grouped.get(key) ?? [];
      existing.push(task);
      grouped.set(key, existing);
    }

    return Array.from(grouped.entries());
  }, [tasksQuery.data]);

  if (tasksQuery.isLoading) {
    return <TableSkeleton rows={8} />;
  }

  if (tasksQuery.isError) {
    return <div className="panel-muted p-4 text-sm text-danger-500">{tasksQuery.error.message}</div>;
  }

  return (
    <div className="space-y-4" id="tasks-panel" role="tabpanel" aria-labelledby="tasks-tab">
      {groupedTasks.map(([activityName, tasks]) => (
        <Card key={activityName}>
          <CardHeader>
            <div>
              <CardTitle>{activityName}</CardTitle>
              <CardDescription>{tasks.length} task(s) in this activity</CardDescription>
            </div>
          </CardHeader>

          <div className="space-y-3">
            {tasks.map((task) => (
              <article key={task.id} className="panel-muted p-3">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="space-y-1">
                    <h4 className="text-sm font-semibold text-neutral-900 dark:text-neutral-100">{task.name}</h4>
                    <div className="flex flex-wrap items-center gap-2 text-xs text-neutral-500 dark:text-neutral-300">
                      <StatusBadge status={task.status} />
                      <PriorityBadge priority={task.priority} />
                      <span>Assignee: {task.assignee?.displayName ?? 'Unassigned'}</span>
                      <span>Due: {formatDateTime(task.dueAt)}</span>
                      <SLAIndicator status={task.slaStatus} remainingMinutes={task.dueAt ? Math.round((new Date(task.dueAt).getTime() - Date.now()) / 60000) : 120} />
                    </div>
                  </div>

                  <div className="flex flex-wrap items-center gap-2">
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => claimTask.mutate(task.id)}
                      disabled={task.status !== 'PENDING' || claimTask.isPending}
                    >
                      Claim
                    </Button>
                    <Button variant="secondary" size="sm" onClick={() => onOpenTask(task.id)}>
                      View Details
                    </Button>
                    <Button size="sm" onClick={() => onOpenTask(task.id)} disabled={task.status === 'DONE'}>
                      Complete
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => toast.info('Reassign flow opened')}>
                      Reassign
                    </Button>
                  </div>
                </div>

                <details className="mt-3 rounded-lg border border-neutral-200 bg-white p-2 text-xs dark:border-neutral-700 dark:bg-neutral-900">
                  <summary className="cursor-pointer font-semibold text-brand-700 dark:text-brand-200">Payload details</summary>
                  <div className="mt-2 grid gap-2 lg:grid-cols-2">
                    <pre className="max-h-48 overflow-auto rounded-lg bg-neutral-950 p-3 text-neutral-100">
                      {JSON.stringify(task.inputPayload ?? {}, null, 2)}
                    </pre>
                    <pre className="max-h-48 overflow-auto rounded-lg bg-neutral-950 p-3 text-neutral-100">
                      {JSON.stringify(task.outputPayload ?? {}, null, 2)}
                    </pre>
                  </div>
                </details>
              </article>
            ))}
          </div>
        </Card>
      ))}
    </div>
  );
}
