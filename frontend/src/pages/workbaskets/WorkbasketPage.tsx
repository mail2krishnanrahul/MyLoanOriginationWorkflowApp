import { useMemo, useState } from 'react';
import { type ColumnDef } from '@tanstack/react-table';
import { CheckCircle2, Flame, Inbox } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { PriorityBadge } from '@/components/domain/PriorityBadge';
import { SLAIndicator } from '@/components/domain/SLAIndicator';
import { Button } from '@/components/ui/Button';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { DataTable } from '@/components/ui/DataTable';
import { EmptyState } from '@/components/ui/EmptyState';
import { TableSkeleton } from '@/components/ui/LoadingSkeleton';
import { TaskWorkbenchModal } from '@/components/tasks/TaskWorkbenchModal';
import { useClaimTask } from '@/hooks/use-tasks';
import { useWorkbaskets, useWorkbasketTasks } from '@/hooks/use-workbaskets';
import type { WorkbasketSummary, WorkbasketTask } from '@/lib/api/types';
import { formatDateTime, fromNow } from '@/lib/utils/date';

function depthClass(depth: number) {
  if (depth > 50) {
    return 'text-danger-500';
  }

  if (depth >= 10) {
    return 'text-warning-500';
  }

  return 'text-success-500';
}

function queueTheme(type: WorkbasketSummary['type']) {
  if (type === 'ESCALATION') {
    return 'border-danger-200 bg-danger-50/80 dark:border-danger-500/40 dark:bg-danger-500/10';
  }

  if (type === 'SPECIALIST') {
    return 'border-brand-200 bg-brand-50/70 dark:border-brand-500/30 dark:bg-brand-500/10';
  }

  return 'border-neutral-200 bg-white dark:border-neutral-700 dark:bg-neutral-900';
}

function priorityWeight(priority: WorkbasketTask['priority']) {
  if (priority === 'CRITICAL') {
    return 4;
  }
  if (priority === 'HIGH') {
    return 3;
  }
  if (priority === 'NORMAL') {
    return 2;
  }
  return 1;
}

export default function WorkbasketPage() {
  const navigate = useNavigate();
  const workbasketsQuery = useWorkbaskets();
  const [selectedWorkbasketId, setSelectedWorkbasketId] = useState<string>('');
  const [activeTaskId, setActiveTaskId] = useState<string | undefined>();

  const workbasketTasksQuery = useWorkbasketTasks(selectedWorkbasketId);
  const claimTask = useClaimTask(undefined, selectedWorkbasketId);

  const sortedTasks = useMemo(() => {
    return [...(workbasketTasksQuery.data ?? [])].sort((a, b) => {
      const priorityScore = priorityWeight(b.priority) - priorityWeight(a.priority);
      if (priorityScore !== 0) {
        return priorityScore;
      }

      if (!a.dueAt || !b.dueAt) {
        return 0;
      }

      return new Date(a.dueAt).getTime() - new Date(b.dueAt).getTime();
    });
  }, [workbasketTasksQuery.data]);

  const columns = useMemo<Array<ColumnDef<WorkbasketTask>>>(
    () => [
      {
        accessorKey: 'taskName',
        header: 'Task Name',
        cell: ({ row }) => <span className="font-medium">{row.original.taskName}</span>
      },
      {
        accessorKey: 'caseReference',
        header: 'Case Reference',
        cell: ({ row }) => <span className="font-mono text-xs">{row.original.caseReference}</span>
      },
      {
        accessorKey: 'priority',
        header: 'Priority',
        cell: ({ row }) => <PriorityBadge priority={row.original.priority} />
      },
      {
        accessorKey: 'dueAt',
        header: 'Due Date',
        cell: ({ row }) => formatDateTime(row.original.dueAt)
      },
      {
        accessorKey: 'slaStatus',
        header: 'SLA Status',
        cell: ({ row }) => (
          <SLAIndicator
            status={row.original.slaStatus}
            remainingMinutes={row.original.dueAt ? Math.round((new Date(row.original.dueAt).getTime() - Date.now()) / 60000) : 120}
          />
        )
      },
      {
        accessorKey: 'waitingSince',
        header: 'Waiting Since',
        cell: ({ row }) => fromNow(row.original.waitingSince)
      },
      {
        id: 'actions',
        header: 'Action',
        enableSorting: false,
        cell: ({ row }) => (
          <Button
            size="sm"
            onClick={async (event) => {
              event.stopPropagation();
              await claimTask.mutateAsync(row.original.id);
              setActiveTaskId(row.original.id);
            }}
            loading={claimTask.isPending}
          >
            Claim Task
          </Button>
        )
      }
    ],
    [claimTask]
  );

  if (workbasketsQuery.isLoading) {
    return <TableSkeleton rows={8} />;
  }

  if (workbasketsQuery.isError) {
    return <div className="panel-muted p-4 text-sm text-danger-500">{workbasketsQuery.error.message}</div>;
  }

  const workbaskets = workbasketsQuery.data ?? [];

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <div>
            <CardTitle>Workbasket queues</CardTitle>
            <CardDescription>Claim queue work based on skill routing and SLA urgency</CardDescription>
          </div>
        </CardHeader>

        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          {workbaskets.map((workbasket) => (
            <button
              key={workbasket.id}
              type="button"
              onClick={() => setSelectedWorkbasketId(workbasket.id)}
              className={`rounded-xl border p-3 text-left transition hover:shadow-hover ${queueTheme(workbasket.type)} ${selectedWorkbasketId === workbasket.id ? 'ring-2 ring-brand-400' : ''}`}
            >
              <div className="flex items-center justify-between gap-2">
                <p className="text-sm font-semibold text-neutral-900 dark:text-neutral-100">{workbasket.name}</p>
                {workbasket.type === 'ESCALATION' ? <Flame className="size-4 text-danger-500" aria-hidden="true" /> : null}
              </div>
              <p className="mt-1 text-xs text-neutral-500 dark:text-neutral-300">{workbasket.type}</p>
              <div className="mt-3 flex items-center justify-between text-xs">
                <span className={depthClass(workbasket.depth)}>Depth: {workbasket.depth}</span>
                <span className="text-neutral-500 dark:text-neutral-300">
                  Oldest {Math.round(workbasket.oldestTaskAgeMinutes / 60)}h
                </span>
              </div>
            </button>
          ))}
        </div>
      </Card>

      {!selectedWorkbasketId ? (
        <EmptyState
          icon={<Inbox className="size-8" aria-hidden="true" />}
          title="Select a workbasket"
          description="Choose a queue to view tasks sorted by priority and due date."
        />
      ) : workbasketTasksQuery.isLoading ? (
        <TableSkeleton rows={8} />
      ) : sortedTasks.length === 0 ? (
        <EmptyState
          icon={<CheckCircle2 className="size-8" aria-hidden="true" />}
          title="All caught up!"
          description="No pending tasks in this workbasket right now."
        />
      ) : (
        <Card>
          <CardHeader>
            <div>
              <CardTitle>Queue tasks</CardTitle>
              <CardDescription>Sorted by Priority DESC and Due Date ASC</CardDescription>
            </div>
            <Button variant="secondary" onClick={() => navigate('/cases')}>
              Back to cases
            </Button>
          </CardHeader>
          <DataTable
            data={sortedTasks}
            columns={columns}
            rowId={(row) => row.id}
            onRowClick={(row) => setActiveTaskId(row.id)}
            height={560}
          />
        </Card>
      )}

      <TaskWorkbenchModal
        taskId={activeTaskId}
        open={Boolean(activeTaskId)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setActiveTaskId(undefined);
          }
        }}
      />
    </div>
  );
}
