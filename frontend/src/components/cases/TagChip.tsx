import { X } from 'lucide-react';
import { TagCategory, type CaseTagSummary } from '@/types/cases';
import { cn } from '@/lib/cn';

interface TagChipProps {
    tag: CaseTagSummary;
    onRemove?: () => void;
}

export function TagChip({ tag, onRemove }: TagChipProps) {
    let colorClasses = '';

    switch (tag.category) {
        case TagCategory.COMPLEXITY:
            colorClasses = 'bg-purple-50 text-purple-700';
            break;
        case TagCategory.DOCUMENT_ERROR:
            colorClasses = 'bg-red-50 text-red-600';
            break;
        case TagCategory.SKILL:
            colorClasses = 'bg-blue-50 text-blue-700';
            break;
        case TagCategory.VIP_PRIORITY:
            colorClasses = 'bg-amber-50 text-amber-700';
            break;
        case TagCategory.EXCEPTION:
            colorClasses = 'bg-orange-50 text-orange-700';
            break;
        case TagCategory.DEFAULT:
        default:
            colorClasses = 'bg-gray-100 text-gray-600';
            break;
    }

    let displayLabel = tag.tagCode;
    if (tag.tagValue) {
        displayLabel += `: ${tag.tagValue}`;
    }

    const isTruncated = displayLabel.length > 20;
    const truncatedLabel = isTruncated ? `${displayLabel.substring(0, 20)}...` : displayLabel;

    return (
        <span
            className={cn(
                'inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full font-medium',
                colorClasses
            )}
            title={isTruncated ? displayLabel : undefined}
        >
            {truncatedLabel}
            {onRemove && (
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        onRemove();
                    }}
                    className="hover:opacity-75 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-1 focus-visible:ring-blue-500 rounded-full"
                    aria-label={`Remove tag ${tag.tagCode}`}
                >
                    <X size={12} aria-hidden="true" />
                </button>
            )}
        </span>
    );
}
