import { Copy, ShieldAlert } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useParams, useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import { OverviewTab } from '@/components/cases/tabs/OverviewTab';
import { TasksTab } from '@/components/cases/tabs/TasksTab';
import { DocumentsTab } from '@/components/cases/tabs/DocumentsTab';
import { ApprovalsTab } from '@/components/cases/tabs/ApprovalsTab';
import { TimelineTab } from '@/components/cases/tabs/TimelineTab';
import { CommunicationsTab } from '@/components/cases/tabs/CommunicationsTab';
import { Deal360Tab } from '@/components/cases/tabs/Deal360Tab';
import { PriorityBadge } from '@/components/domain/PriorityBadge';
import { SLAIndicator } from '@/components/domain/SLAIndicator';
import { StatusBadge } from '@/components/domain/StatusBadge';
import { Tabs, type TabItem } from '@/components/navigation/Tabs';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { ConfirmDialog } from '@/components/ui/ConfirmDialog';
import { SkeletonCard } from '@/components/ui/LoadingSkeleton';
import { TaskWorkbenchModal } from '@/components/tasks/TaskWorkbenchModal';
import { useCaseAction, useCaseDetail } from '@/hooks/use-case-detail';
import { useCaseTasks } from '@/hooks/use-tasks';

const tabs: TabItem[] = [
  { key: 'overview', label: 'Overview' },
  { key: 'tasks', label: 'Tasks' },
  { key: 'documents', label: 'Documents' },
  { key: 'approvals', label: 'Approvals' },
  { key: 'timeline', label: 'Timeline / History' },
  { key: 'deal360', label: 'Deal 360' },
  { key: 'communications', label: 'Communications' }
];

function slaProgressClass(remainingMinutes: number) {
  if (remainingMinutes <= 0) {
    return 'w-full';
  }

  if (remainingMinutes < 8 * 60) {
    return 'w-5/6';
  }

  if (remainingMinutes < 24 * 60) {
    return 'w-2/3';
  }

  if (remainingMinutes < 48 * 60) {
    return 'w-1/2';
  }

  return 'w-1/3';
}

export default function CaseDetailPage() {
  const { caseId = '' } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get('tab') ?? 'overview';

  const caseDetailQuery = useCaseDetail(caseId);
  const tasksQuery = useCaseTasks(caseId);
  const caseAction = useCaseAction(caseId);

  const [confirmAction, setConfirmAction] = useState<'SUSPEND' | 'WITHDRAW' | 'EMERGENCY_CLOSE' | null>(null);
  const [taskWorkbenchTaskId, setTaskWorkbenchTaskId] = useState<string | undefined>();

  const nextTaskId = useMemo(() => {
    const task = (tasksQuery.data ?? []).find((item) => item.status !== 'DONE') ?? tasksQuery.data?.[0];
    return task?.id;
  }, [tasksQuery.data]);

  if (caseDetailQuery.isLoading) {
    return (
      <div className="space-y-4">
        <SkeletonCard className="h-28" />
        <SkeletonCard className="h-[36rem]" />
      </div>
    );
  }

  if (caseDetailQuery.isError || !caseDetailQuery.data) {
    return <div className="panel-muted p-4 text-sm text-danger-500">{caseDetailQuery.error?.message ?? 'Case not found'}</div>;
  }

  const caseDetail = caseDetailQuery.data;

  return (
    <div className="space-y-4">
      <Card className="sticky top-[4.5rem] z-20 border-brand-100 bg-white/95 backdrop-blur dark:border-brand-500/30 dark:bg-neutral-900/95">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div className="flex items-center gap-2">
              <p className="font-mono text-xl font-semibold text-brand-700 dark:text-brand-200">
                {caseDetail.referenceNumber}
              </p>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  void navigator.clipboard.writeText(caseDetail.referenceNumber);
                  toast.success('Reference copied');
                }}
                aria-label="Copy reference number"
              >
                <Copy className="size-4" aria-hidden="true" />
              </Button>
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-2">
              <StatusBadge status={caseDetail.status} />
              {caseDetail.priority && <PriorityBadge priority={caseDetail.priority} />}
              <span className="text-sm text-neutral-500 dark:text-neutral-300">
                {caseDetail.borrowerName} | {caseDetail.caseType}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={() => setConfirmAction('SUSPEND')}>
              Suspend
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setConfirmAction('WITHDRAW')}>
              Withdraw
            </Button>
            <Button variant="danger" size="sm" onClick={() => setConfirmAction('EMERGENCY_CLOSE')}>
              <ShieldAlert className="size-4" aria-hidden="true" />
              Emergency Close
            </Button>
          </div>
        </div>

        <div className="mt-4">
          <div className="mb-2 flex items-center justify-between text-xs">
            <SLAIndicator status={caseDetail.slaStatus} remainingMinutes={caseDetail.slaRemainingMinutes} />
            <span className="text-neutral-500 dark:text-neutral-300">{Math.max(0, caseDetail.slaRemainingMinutes)} mins remaining</span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-neutral-200 dark:bg-neutral-700">
            <div
              className={`h-full rounded-full bg-gradient-to-r from-brand-500 via-brand-400 to-accent-400 transition-all ${slaProgressClass(caseDetail.slaRemainingMinutes)}`}
            />
          </div>
        </div>
      </Card>

      <Tabs
        value={tab}
        onChange={(nextTab) => {
          const next = new URLSearchParams(searchParams);
          next.set('tab', nextTab);
          setSearchParams(next);
        }}
        items={tabs}
      />

      {tab === 'overview' ? (
        <div id="overview-panel" role="tabpanel" aria-labelledby="overview-tab">
          <OverviewTab
            caseDetail={caseDetail}
            onOpenNextTask={() => {
              if (!nextTaskId) {
                toast.info('No remaining tasks');
                return;
              }
              setTaskWorkbenchTaskId(nextTaskId);
            }}
          />
        </div>
      ) : null}
      {tab === 'tasks' ? <TasksTab caseId={caseId} onOpenTask={(taskId) => setTaskWorkbenchTaskId(taskId)} /> : null}
      {tab === 'documents' ? <DocumentsTab caseId={caseId} /> : null}
      {tab === 'approvals' ? <ApprovalsTab caseId={caseId} /> : null}
      {tab === 'timeline' ? <TimelineTab caseId={caseId} /> : null}
      {tab === 'deal360' ? <Deal360Tab dealPayload={caseDetail.metadata as any} /> : null}
      {tab === 'communications' ? <CommunicationsTab caseId={caseId} /> : null}

      <TaskWorkbenchModal
        taskId={taskWorkbenchTaskId}
        open={Boolean(taskWorkbenchTaskId)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setTaskWorkbenchTaskId(undefined);
          }
        }}
      />

      <ConfirmDialog
        open={Boolean(confirmAction)}
        title={`${confirmAction?.replaceAll('_', ' ') ?? ''} case`}
        description="This action is audited and may impact downstream approvals."
        confirmLabel="Proceed"
        confirmVariant={confirmAction === 'EMERGENCY_CLOSE' ? 'danger' : 'primary'}
        loading={caseAction.isPending}
        onConfirm={async () => {
          if (!confirmAction) {
            return;
          }

          await caseAction.mutateAsync({ action: confirmAction });
          setConfirmAction(null);
        }}
        onCancel={() => setConfirmAction(null)}
      />
    </div>
  );
}
