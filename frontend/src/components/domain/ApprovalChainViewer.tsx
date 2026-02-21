import { CheckCircle2, Circle, XCircle } from 'lucide-react';
import type { ApprovalNode } from '@/lib/api/types';
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';

interface ApprovalChainViewerProps {
  chain: ApprovalNode[];
}

function statusColor(status: ApprovalNode['status']) {
  if (status === 'APPROVED') {
    return 'bg-success-50 text-success-700 ring-success-500/20 dark:bg-success-500/15 dark:text-green-300';
  }

  if (status === 'REJECTED') {
    return 'bg-danger-50 text-danger-700 ring-danger-500/20 dark:bg-danger-500/15 dark:text-red-300';
  }

  return 'bg-neutral-100 text-neutral-700 ring-neutral-200 dark:bg-neutral-800 dark:text-neutral-300';
}

export function ApprovalChainViewer({ chain }: ApprovalChainViewerProps) {
  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Approval chain</CardTitle>
          <CardDescription>Tiered approvals and decisions</CardDescription>
        </div>
      </CardHeader>
      <div className="grid gap-3">
        {chain.map((node) => (
          <div key={node.id} className="panel-muted p-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="text-sm font-semibold text-neutral-900 dark:text-neutral-50">Tier {node.tier}</p>
                <p className="mt-1 text-xs text-neutral-500 dark:text-neutral-300">
                  {node.approvers.map((approver) => approver.displayName).join(', ')}
                </p>
              </div>
              <span className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-semibold ring-1 ring-inset ${statusColor(node.status)}`}>
                {node.status === 'APPROVED' ? (
                  <CheckCircle2 className="size-3" aria-hidden="true" />
                ) : node.status === 'REJECTED' ? (
                  <XCircle className="size-3" aria-hidden="true" />
                ) : (
                  <Circle className="size-3" aria-hidden="true" />
                )}
                {node.status}
              </span>
            </div>
          </div>
        ))}
      </div>
    </Card>
  );
}
