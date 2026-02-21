import { Clock3, Flame } from 'lucide-react';
import { Badge } from '@/components/ui/Badge';
import { formatMinutesRemaining } from '@/lib/utils/date';
import type { SlaState } from '@/lib/api/types';

interface SLAIndicatorProps {
  status: SlaState;
  remainingMinutes: number;
}

const statusText: Record<SlaState, string> = {
  ON_TRACK: 'On track',
  WARNING: 'Warning',
  BREACHED: 'Breached'
};

const statusVariant: Record<SlaState, 'success' | 'warning' | 'danger'> = {
  ON_TRACK: 'success',
  WARNING: 'warning',
  BREACHED: 'danger'
};

export function SLAIndicator({ status, remainingMinutes }: SLAIndicatorProps) {
  return (
    <span title={formatMinutesRemaining(remainingMinutes)}>
      <Badge variant={statusVariant[status]}>
        {status === 'BREACHED' ? <Flame className="size-3" aria-hidden="true" /> : <Clock3 className="size-3" aria-hidden="true" />}
        {statusText[status]}
      </Badge>
    </span>
  );
}
