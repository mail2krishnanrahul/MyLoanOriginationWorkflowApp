import { formatDistanceToNow, parseISO } from 'date-fns';
import { PriorityBadge } from './PriorityBadge';
import { StatusBadge } from './StatusBadge';
import { TagChip } from './TagChip';
import { UserAvatar } from './UserAvatar';
import { SLAIndicator } from './SLAIndicator';
import { TagCategory, type CaseListItem, type CaseTagSummary } from '@/types/cases';
import { cn } from '@/lib/cn';
import { Calendar } from 'lucide-react';

interface CaseCardProps {
    caseItem: CaseListItem;
    onClick: (id: string) => void;
    onTagClick?: (tagCode: string) => void;
}

export function CaseCard({ caseItem, onClick, onTagClick }: CaseCardProps) {
    const isVip = caseItem.isVip;
    const hasBlockingErrors = caseItem.hasBlockingErrors;
    const isInQa = caseItem.status === 'IN_QA';

    const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            onClick(caseItem.id);
        }
    };

    const title = caseItem.title || caseItem.reference;

    const visibleTags: CaseTagSummary[] = [];

    if (caseItem.complexity) {
        visibleTags.push({
            tagCode: caseItem.complexity,
            tagValue: null,
            category: TagCategory.COMPLEXITY
        });
    }

    const remainingSlots = 4 - visibleTags.length;
    const caseTagsToUse = caseItem.tags || [];
    const tagsToShow = caseTagsToUse.slice(0, remainingSlots);
    visibleTags.push(...tagsToShow);

    const hiddenTagsCount = caseTagsToUse.length - tagsToShow.length;
    const hiddenTagsNames = hiddenTagsCount > 0
        ? caseTagsToUse.slice(tagsToShow.length).map(t => t.tagCode).join(', ')
        : '';

    return (
        <div
            role="button"
            tabIndex={0}
            onClick={() => onClick(caseItem.id)}
            onKeyDown={handleKeyDown}
            className={cn(
                'bg-white rounded-xl border border-gray-100 shadow-sm hover:shadow-md transition-shadow duration-150 cursor-pointer flex flex-col focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:outline-none overflow-hidden relative',
                isVip && !hasBlockingErrors && 'border-l-4 border-l-amber-400',
                hasBlockingErrors && 'border-l-4 border-l-red-500' // If both, errors take precedence by tailwind class overrides
            )}
        >
            <div className={cn('p-5 pb-0', isInQa && 'bg-purple-50/30')}>
                <div className="flex justify-between items-start gap-4">
                    <div className="flex items-center gap-2">
                        <span className="text-xs text-gray-400 font-mono">{caseItem.reference}</span>
                        <PriorityBadge priority={caseItem.priority} size="sm" />
                    </div>
                    <StatusBadge status={caseItem.status} size="sm" />
                </div>

                <h4 className="text-base font-semibold text-gray-900 mt-1 truncate" title={title}>
                    {title}
                </h4>
                <p className="text-sm text-gray-500 mt-1 line-clamp-2" title={caseItem.description}>
                    {caseItem.description || 'No description provided.'}
                </p>
            </div>

            <div className="px-5 mt-3 flex-grow">
                {(visibleTags.length > 0) && (
                    <div className="flex flex-wrap gap-2">
                        {visibleTags.map((tag, idx) => (
                            <span key={`${tag.tagCode}-${idx}`}
                                onClick={(e) => {
                                    if (onTagClick) {
                                        e.stopPropagation();
                                        onTagClick(tag.tagCode);
                                    }
                                }}
                                className={cn(onTagClick && "cursor-pointer")}
                            >
                                <TagChip tag={tag} />
                            </span>
                        ))}
                        {hiddenTagsCount > 0 && (
                            <span
                                className="inline-flex items-center text-xs px-2 py-0.5 rounded-full font-medium bg-gray-100 text-gray-600"
                                title={hiddenTagsNames}
                            >
                                +{hiddenTagsCount} more
                            </span>
                        )}
                    </div>
                )}
            </div>

            <div className="px-5 mt-3 py-3 border-t border-gray-100 flex items-center justify-between mt-auto bg-white">
                <div className="flex items-center gap-2">
                    {caseItem.assignedUser ? (
                        <>
                            <UserAvatar user={caseItem.assignedUser} size="xs" />
                            <span className="text-sm text-gray-600">{caseItem.assignedUser.displayName}</span>
                        </>
                    ) : (
                        <span className="text-sm text-gray-500 italic">Unassigned</span>
                    )}
                </div>
                <div className="flex items-center gap-3">
                    <SLAIndicator slaDueAt={caseItem.slaDueAt} compact={true} />
                    <div className="flex items-center text-gray-400 text-xs" title={caseItem.createdAt}>
                        <Calendar size={14} className="mr-1.5" aria-hidden="true" />
                        {formatDistanceToNow(parseISO(caseItem.createdAt), { addSuffix: true })}
                    </div>
                </div>
            </div>
        </div>
    );
}
