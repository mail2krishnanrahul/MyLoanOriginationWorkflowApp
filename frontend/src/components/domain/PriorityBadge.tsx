import { AlertOctagon } from 'lucide-react';
import { Badge } from '@/components/ui/Badge';
import type { Priority } from '@/lib/api/types';

const variantMap: Record<Priority, 'neutral' | 'info' | 'warning' | 'danger'> = {
  LOW: 'neutral',
  NORMAL: 'info',
  HIGH: 'warning',
  CRITICAL: 'danger'
};

interface PriorityBadgeProps {
  priority: Priority;
}

export function PriorityBadge({ priority }: PriorityBadgeProps) {
  return (
    <Badge variant={variantMap[priority]}>
      {(priority === 'HIGH' || priority === 'CRITICAL') && <AlertOctagon className="size-3" aria-hidden="true" />}
      {priority}
    </Badge>
  );
}
