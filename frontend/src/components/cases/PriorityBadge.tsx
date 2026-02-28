import { AlertTriangle, TrendingDown, TrendingUp, AlertCircle } from 'lucide-react';
import type { CasePriority } from '@/types/cases';
import { cn } from '@/lib/cn';

interface PriorityBadgeProps {
    priority: CasePriority;
    size?: 'sm' | 'md';
}

export function PriorityBadge({ priority, size = 'md' }: PriorityBadgeProps) {
    const isSm = size === 'sm';

    const baseClasses = cn(
        'inline-flex items-center gap-1.5 font-medium rounded',
        isSm ? 'text-xs px-1.5 py-0.5' : 'text-sm px-2 py-1'
    );

    const iconSize = isSm ? 12 : 14;

    switch (priority) {
        case 'URGENT':
            return (
                <span
                    className={cn(baseClasses, 'bg-red-100 text-red-700 font-bold')}
                    aria-label="Priority: URGENT"
                >
                    <AlertCircle size={iconSize} aria-hidden="true" />
                    Urgent
                </span>
            );
        case 'HIGH':
            return (
                <span
                    className={cn(baseClasses, 'bg-red-50 text-red-600')}
                    aria-label="Priority: HIGH"
                >
                    <AlertTriangle size={iconSize} aria-hidden="true" />
                    High
                </span>
            );
        case 'MEDIUM':
            return (
                <span
                    className={cn(baseClasses, 'bg-amber-50 text-amber-600')}
                    aria-label="Priority: MEDIUM"
                >
                    <TrendingUp size={iconSize} aria-hidden="true" />
                    Medium
                </span>
            );
        case 'LOW':
            return (
                <span
                    className={cn(baseClasses, 'bg-green-50 text-green-600')}
                    aria-label="Priority: LOW"
                >
                    <TrendingDown size={iconSize} aria-hidden="true" />
                    Low
                </span>
            );
        default:
            return null;
    }
}
