import { useState } from 'react';
import { Clock4, UsersRound } from 'lucide-react';
import { toast } from 'sonner';
import { ApprovalChainViewer } from '@/components/domain/ApprovalChainViewer';
import { Button } from '@/components/ui/Button';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { FormField, TextArea } from '@/components/ui/FormField';
import { Modal } from '@/components/ui/Modal';
import { TableSkeleton } from '@/components/ui/LoadingSkeleton';
import { useApprove, useCaseApprovals, useReject } from '@/hooks/use-approvals';
import { formatDateTime, fromNow } from '@/lib/utils/date';
import { formatCurrency } from '@/lib/utils/format';

interface ApprovalsTabProps {
  caseId: string;
}

export function ApprovalsTab({ caseId }: ApprovalsTabProps) {
  const approvalsQuery = useCaseApprovals(caseId);
  const approveMutation = useApprove(caseId);
  const rejectMutation = useReject(caseId);

  const [rejectingApprovalId, setRejectingApprovalId] = useState<string | null>(null);
  const [rejectionReason, setRejectionReason] = useState('');

  if (approvalsQuery.isLoading) {
    return <TableSkeleton rows={6} />;
  }

  if (approvalsQuery.isError) {
    return <div className="panel-muted p-4 text-sm text-danger-500">{approvalsQuery.error.message}</div>;
  }

  const chain = approvalsQuery.data?.chain ?? [];
  const pending = approvalsQuery.data?.pending ?? [];
  const history = approvalsQuery.data?.history ?? [];

  return (
    <div id="approvals-panel" role="tabpanel" aria-labelledby="approvals-tab" className="space-y-4">
      <ApprovalChainViewer chain={chain} />

      <Card>
        <CardHeader>
          <div>
            <CardTitle>Pending approvals</CardTitle>
            <CardDescription>Requests waiting on your decision</CardDescription>
          </div>
        </CardHeader>

        {pending.length === 0 ? (
          <p className="rounded-xl border border-dashed border-neutral-300 p-4 text-sm text-neutral-500 dark:border-neutral-700 dark:text-neutral-300">
            No pending approvals for your role.
          </p>
        ) : (
          <div className="grid gap-3 lg:grid-cols-2">
            {pending.map((request) => (
              <article key={request.id} className="panel-muted p-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <p className="text-sm font-semibold text-neutral-900 dark:text-neutral-100">{request.context}</p>
                    <p className="text-xs text-neutral-500 dark:text-neutral-300">
                      Requested by {request.requestedBy.displayName}
                    </p>
                  </div>
                  {request.amount ? (
                    <span className="rounded-md bg-neutral-200 px-2 py-1 text-xs font-semibold dark:bg-neutral-700">
                      {formatCurrency(request.amount)}
                    </span>
                  ) : null}
                </div>

                <div className="mt-3 flex items-center gap-4 text-xs text-neutral-500 dark:text-neutral-300">
                  <span className="inline-flex items-center gap-1">
                    <Clock4 className="size-3" aria-hidden="true" />
                    Expires {fromNow(request.expiresAt)}
                  </span>
                  <span className="inline-flex items-center gap-1">
                    <UsersRound className="size-3" aria-hidden="true" />
                    {request.status}
                  </span>
                </div>

                <div className="mt-3 flex flex-wrap gap-2">
                  <Button size="sm" onClick={() => approveMutation.mutate(request.id)} loading={approveMutation.isPending}>
                    Approve
                  </Button>
                  <Button variant="secondary" size="sm" onClick={() => setRejectingApprovalId(request.id)}>
                    Reject
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => toast.info('Delegate flow opened')}>
                    Delegate
                  </Button>
                </div>
              </article>
            ))}
          </div>
        )}
      </Card>

      <Card>
        <CardHeader>
          <div>
            <CardTitle>Approval history</CardTitle>
            <CardDescription>Full decision trail for this case</CardDescription>
          </div>
        </CardHeader>

        <ul className="space-y-2">
          {history.map((entry) => (
            <li key={entry.id} className="panel-muted flex items-center justify-between gap-3 px-3 py-2 text-sm">
              <div>
                <p className="font-medium text-neutral-900 dark:text-neutral-100">{entry.context}</p>
                <p className="text-xs text-neutral-500 dark:text-neutral-300">
                  {entry.requestedBy.displayName} | {formatDateTime(entry.expiresAt)}
                </p>
              </div>
              <span className="text-xs font-semibold text-neutral-500 dark:text-neutral-300">{entry.status}</span>
            </li>
          ))}
        </ul>
      </Card>

      <Modal
        open={Boolean(rejectingApprovalId)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setRejectingApprovalId(null);
            setRejectionReason('');
          }
        }}
        size="sm"
        title="Reject approval"
        description="Provide rationale for audit and rework routing"
      >
        <form
          className="space-y-3"
          onSubmit={async (event) => {
            event.preventDefault();
            if (!rejectingApprovalId) {
              return;
            }

            await rejectMutation.mutateAsync({
              approvalId: rejectingApprovalId,
              reason: rejectionReason
            });
            setRejectingApprovalId(null);
            setRejectionReason('');
          }}
        >
          <FormField id="rejectionReason" label="Reason" required>
            <TextArea
              id="rejectionReason"
              rows={4}
              value={rejectionReason}
              onChange={(event) => setRejectionReason(event.target.value)}
            />
          </FormField>
          <div className="flex justify-end gap-2">
            <Button
              variant="secondary"
              type="button"
              onClick={() => {
                setRejectingApprovalId(null);
                setRejectionReason('');
              }}
            >
              Cancel
            </Button>
            <Button type="submit" loading={rejectMutation.isPending} disabled={!rejectionReason.trim()}>
              Reject
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
