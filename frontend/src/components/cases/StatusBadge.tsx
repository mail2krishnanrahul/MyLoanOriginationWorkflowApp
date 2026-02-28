import type { CaseStatus } from '@/types/cases';
import { cn } from '@/lib/cn';

interface StatusBadgeProps {
    status: CaseStatus;
    size?: 'sm' | 'md';
}

export function StatusBadge({ status, size = 'md' }: StatusBadgeProps) {
    const isSm = size === 'sm';

    const baseClasses = cn(
        'inline-flex items-center font-medium rounded-full',
        isSm ? 'text-xs px-2 py-0.5' : 'text-sm px-2.5 py-1'
    );

    let label = '';
    let colorClasses = '';

    switch (status) {
        case 'NEW':
            label = 'New';
            colorClasses = 'bg-blue-100 text-blue-700';
            break;
        case 'ALLOCATABLE':
            label = 'Unassigned';
            colorClasses = 'bg-gray-100 text-gray-700';
            break;
        case 'IN_PROGRESS':
            label = 'In Progress';
            colorClasses = 'bg-amber-100 text-amber-700';
            break;
        case 'ADDITIONAL_INFO':
            label = 'Info Requested';
            colorClasses = 'bg-blue-100 text-blue-700';
            break;
        case 'IN_QA':
            label = 'In QA';
            colorClasses = 'bg-purple-100 text-purple-700';
            break;
        case 'REMEDIATION':
            label = 'Remediation';
            colorClasses = 'bg-red-100 text-red-700';
            break;
        case 'COMPLETED':
            label = 'Completed';
            colorClasses = 'bg-green-100 text-green-700';
            break;
        case 'WITHDRAWN':
            label = 'Withdrawn';
            colorClasses = 'bg-gray-100 text-gray-500';
            break;
        case 'ON_HOLD':
            label = 'On Hold';
            colorClasses = 'bg-orange-100 text-orange-700';
            break;
        default:
            label = status;
            colorClasses = 'bg-gray-100 text-gray-600';
    }

    return (
        <span className={cn(baseClasses, colorClasses)} aria-label={`Status: ${label}`}>
            {label}
        </span>
    );
}
