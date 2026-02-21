import { CheckCircle2, Circle, PauseCircle, ShieldAlert, XCircle } from 'lucide-react';
import type { ComponentType } from 'react';
import { Badge } from '@/components/ui/Badge';
import type { CaseStatus, TaskStatus } from '@/lib/api/types';

type SupportedStatus = CaseStatus | TaskStatus;

const variantMap: Record<SupportedStatus, 'neutral' | 'brand' | 'warning' | 'danger' | 'success'> = {
  DRAFT: 'neutral',
  IN_PROGRESS: 'brand',
  PENDING_APPROVAL: 'warning',
  SUSPENDED: 'warning',
  WITHDRAWN: 'danger',
  CLOSED: 'success',
  REJECTED: 'danger',
  PENDING: 'neutral',
  DONE: 'success',
  FAILED: 'danger',
  BLOCKED: 'warning'
};

const iconMap: Record<SupportedStatus, ComponentType<{ className?: string }>> = {
  DRAFT: Circle,
  IN_PROGRESS: Circle,
  PENDING_APPROVAL: PauseCircle,
  SUSPENDED: PauseCircle,
  WITHDRAWN: XCircle,
  CLOSED: CheckCircle2,
  REJECTED: XCircle,
  PENDING: Circle,
  DONE: CheckCircle2,
  FAILED: XCircle,
  BLOCKED: ShieldAlert
};

interface StatusBadgeProps {
  status: SupportedStatus;
}

export function StatusBadge({ status }: StatusBadgeProps) {
  const Icon = iconMap[status];

  return (
    <Badge variant={variantMap[status]}>
      <Icon className="size-3" aria-hidden="true" />
      {status.replaceAll('_', ' ')}
    </Badge>
  );
}
