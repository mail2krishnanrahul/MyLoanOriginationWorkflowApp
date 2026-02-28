import { Clock } from 'lucide-react';
import { formatDistanceToNow, isPast, differenceInHours, parseISO } from 'date-fns';
import { cn } from '@/lib/cn';

interface SLAIndicatorProps {
    slaDueAt: string | null;
    compact?: boolean;
}

export function SLAIndicator({ slaDueAt, compact = false }: SLAIndicatorProps) {
    if (!slaDueAt) {
        return (
            <div className="flex items-center text-gray-400" title="No SLA set">
                <Clock size={14} className={cn(!compact && "mr-1.5")} aria-hidden="true" />
                {!compact && <span className="text-xs">No SLA</span>}
            </div>
        );
    }

    const dueDate = parseISO(slaDueAt);
    const isOverdue = isPast(dueDate);
    const hoursRemaining = differenceInHours(dueDate, new Date());

    let colorClass = 'text-gray-500';
    let isDanger = false;

    if (isOverdue) {
        colorClass = 'text-red-600 font-bold';
        isDanger = true;
    } else if (hoursRemaining < 4) {
        colorClass = 'text-red-600';
        isDanger = true;
    } else if (hoursRemaining < 24) {
        colorClass = 'text-amber-600';
    }

    const timeString = isOverdue ? 'OVERDUE' : formatDistanceToNow(dueDate, { addSuffix: true });

    if (compact) {
        return (
            <div
                className={cn("flex items-center relative", colorClass)}
                title={`Due ${timeString}`}
                aria-label={`SLA due ${timeString}`}
            >
                {isDanger && !isOverdue && (
                    <span className="absolute -top-0.5 -right-0.5 flex h-2 w-2">
                        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-red-400 opacity-75"></span>
                        <span className="relative inline-flex rounded-full h-2 w-2 bg-red-500"></span>
                    </span>
                )}
                <Clock size={16} aria-hidden="true" />
            </div>
        );
    }

    return (
        <div className={cn("flex items-center text-xs w-full", colorClass)} aria-label={`SLA due ${timeString}`}>
            {isDanger && !isOverdue && (
                <span className="relative flex h-2 w-2 mr-1.5 mx-0.5 mt-0.5 shrink-0">
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-red-400 opacity-75"></span>
                    <span className="relative inline-flex rounded-full h-2 w-2 bg-red-500"></span>
                </span>
            )}
            {!isDanger && <Clock size={14} className="mr-1.5 shrink-0" aria-hidden="true" />}
            {isOverdue && <Clock size={14} className="mr-1.5 shrink-0" aria-hidden="true" />}
            <span className="truncate">{isOverdue ? 'OVERDUE' : `Due ${timeString}`}</span>
        </div>
    );
}
