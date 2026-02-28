import { BellRing, FileUp, Gavel, PlayCircle } from 'lucide-react';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { StatusBadge } from '@/components/domain/StatusBadge';
import type { CaseDetail } from '@/lib/api/types';
import { formatCurrency } from '@/lib/utils/format';
import { formatDate } from '@/lib/utils/date';

interface OverviewTabProps {
  caseDetail: CaseDetail;
  onOpenNextTask: () => void;
}

export function OverviewTab({ caseDetail, onOpenNextTask }: OverviewTabProps) {
  return (
    <div className="space-y-4">
      <div className="grid gap-4 xl:grid-cols-[1.4fr_1fr]">
        <Card>
          <CardHeader>
            <div>
              <CardTitle>Case summary</CardTitle>
              <CardDescription>Core metadata and projected close path</CardDescription>
            </div>
          </CardHeader>
          <div className="grid gap-3 text-sm md:grid-cols-2">
            <div className="panel-muted p-3">
              <p className="text-xs uppercase tracking-wide text-neutral-500 dark:text-neutral-300">Loan amount</p>
              <p className="mt-1 text-base font-semibold text-neutral-900 dark:text-neutral-100">
                {formatCurrency(caseDetail.loanAmount)}
              </p>
            </div>
            <div className="panel-muted p-3">
              <p className="text-xs uppercase tracking-wide text-neutral-500 dark:text-neutral-300">Product</p>
              <p className="mt-1 text-base font-semibold text-neutral-900 dark:text-neutral-100">
                {caseDetail.productType}
              </p>
            </div>
            <div className="panel-muted p-3">
              <p className="text-xs uppercase tracking-wide text-neutral-500 dark:text-neutral-300">Channel</p>
              <p className="mt-1 text-base font-semibold text-neutral-900 dark:text-neutral-100">{caseDetail.channel}</p>
            </div>
            <div className="panel-muted p-3">
              <p className="text-xs uppercase tracking-wide text-neutral-500 dark:text-neutral-300">Officer</p>
              <p className="mt-1 text-base font-semibold text-neutral-900 dark:text-neutral-100">{caseDetail.officer}</p>
            </div>
          </div>

          <div className="mt-4 rounded-xl border border-dashed border-neutral-300 p-4 dark:border-neutral-700">
            <p className="text-xs uppercase tracking-wide text-neutral-500 dark:text-neutral-300">Mini timeline</p>
            <div className="mt-3 flex flex-wrap items-center gap-2 text-sm text-neutral-600 dark:text-neutral-200">
              <span className="rounded-full bg-neutral-100 px-2 py-1 dark:bg-neutral-800">
                Created {formatDate(caseDetail.createdAt)}
              </span>
              <span className="text-neutral-400">&rarr;</span>
              <span className="rounded-full bg-brand-50 px-2 py-1 text-brand-700 dark:bg-brand-500/15 dark:text-brand-200">
                Current: {caseDetail.currentStage ?? caseDetail.stage ?? '--'}
              </span>
              <span className="text-neutral-400">&rarr;</span>
              <span className="rounded-full bg-accent-50 px-2 py-1 text-accent-700 dark:bg-accent-500/15 dark:text-accent-200">
                Target close {formatDate(caseDetail.targetCloseDate)}
              </span>
            </div>
          </div>
        </Card>

        <Card>
          <CardHeader>
            <div>
              <CardTitle>Current stage</CardTitle>
              <CardDescription>{caseDetail.stageDescription ?? 'In-flight stage activities and tasks'}</CardDescription>
            </div>
          </CardHeader>

          <div className="panel-muted mb-4 p-3">
            <p className="text-sm font-semibold text-neutral-900 dark:text-neutral-100">
              {caseDetail.tasksCompleted} of {caseDetail.tasksTotal} tasks completed
            </p>
            <p className="mt-1 text-xs text-neutral-500 dark:text-neutral-300">Activity breakdown</p>
          </div>

          <div className="space-y-2">
            {caseDetail.activities.map((activity) => (
              <details key={activity.activityCode} className="panel-muted p-3" open>
                <summary className="cursor-pointer list-none text-sm font-semibold text-neutral-800 dark:text-neutral-100">
                  {activity.activityCode} ({activity.completed}/{activity.total})
                </summary>

                <ul className="mt-2 space-y-1 text-xs text-neutral-600 dark:text-neutral-200">
                  {activity.tasks.map((task) => (
                    <li key={task.taskId} className="flex items-center justify-between gap-2 rounded-md bg-white px-2 py-1 dark:bg-neutral-900">
                      <span>{task.name}</span>
                      <StatusBadge status={task.status} />
                    </li>
                  ))}
                </ul>
              </details>
            ))}
          </div>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <div>
            <CardTitle>Quick actions</CardTitle>
            <CardDescription>Primary workflows for the current stage</CardDescription>
          </div>
        </CardHeader>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <Button size="lg" onClick={onOpenNextTask} className="justify-start">
            <PlayCircle className="size-4" aria-hidden="true" />
            Next Task
          </Button>
          <Button variant="secondary" className="justify-start">
            <Gavel className="size-4" aria-hidden="true" />
            Request Approval
          </Button>
          <Button variant="secondary" className="justify-start">
            <FileUp className="size-4" aria-hidden="true" />
            Upload Document
          </Button>
          <Button variant="secondary" className="justify-start">
            <BellRing className="size-4" aria-hidden="true" />
            Send Notification
          </Button>
        </div>
      </Card>
    </div>
  );
}
