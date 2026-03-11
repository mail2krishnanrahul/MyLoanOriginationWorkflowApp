import { useEffect, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useCaseListStore } from '@/stores/caseListStore';
import { useUiStore } from '@/store/ui-store';
import type { CaseListFilters, CaseStatus, CasePriority, CaseComplexity, SkillCode } from '@/types/cases';

export function useSyncFiltersWithURL() {
    const [searchParams, setSearchParams] = useSearchParams();
    const store = useCaseListStore();
    const caseScope = useUiStore((s) => s.caseScope);
    const isInitialized = useRef(false);

    // Sync URL to Store (on mount)
    useEffect(() => {
        if (isInitialized.current) return;

        const urlPage = parseInt(searchParams.get('page') || '1', 10);
        const search = searchParams.get('search') || '';
        const statuses = searchParams.getAll('status') as CaseStatus[];
        const priorities = searchParams.getAll('priority') as CasePriority[];
        const complexities = searchParams.getAll('complexity') as CaseComplexity[];
        const skillCodes = searchParams.getAll('skill') as SkillCode[];

        // When URL has no assignedToMe/teamId params, derive defaults from caseScope
        const hasAssignedParam = searchParams.has('assignedToMe');
        const hasTeamParam = searchParams.has('teamId');

        let assignedToMe = false;
        let teamId: string | null = null;
        if (hasAssignedParam || hasTeamParam) {
            assignedToMe = searchParams.get('assignedToMe') === 'true';
            teamId = searchParams.get('teamId');
        } else {
            // No URL params — use caseScope defaults
            assignedToMe = caseScope === 'my';
            teamId = caseScope === 'team' ? 'current' : null;
        }

        const filters: CaseListFilters = {
            search,
            statuses,
            priorities,
            complexities,
            skillCodes,
            assignedToMe,
            hasBlockingErrors: searchParams.get('hasBlockingErrors') === 'true',
            isVip: searchParams.get('isVip') === 'true',
            slaDueBefore: searchParams.get('slaDueBefore'),
            createdAfter: searchParams.get('createdAfter'),
            createdBefore: searchParams.get('createdBefore'),
            teamId,
        };

        store.setAllFilters(filters, urlPage);
        isInitialized.current = true;
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // Sync Store to URL (on change)
    useEffect(() => {
        if (!isInitialized.current) return;

        const nextParams = new URLSearchParams();

        if (store.page > 1) {
            nextParams.set('page', store.page.toString());
        }

        if (store.filters.search) {
            nextParams.set('search', store.filters.search);
        }

        store.filters.statuses.forEach(s => nextParams.append('status', s));
        store.filters.priorities.forEach(p => nextParams.append('priority', p));
        store.filters.complexities.forEach(c => nextParams.append('complexity', c));
        store.filters.skillCodes.forEach(s => nextParams.append('skill', s));

        if (store.filters.assignedToMe) nextParams.set('assignedToMe', 'true');
        if (store.filters.hasBlockingErrors) nextParams.set('hasBlockingErrors', 'true');
        if (store.filters.isVip) nextParams.set('isVip', 'true');
        if (store.filters.slaDueBefore) nextParams.set('slaDueBefore', store.filters.slaDueBefore);
        if (store.filters.createdAfter) nextParams.set('createdAfter', store.filters.createdAfter);
        if (store.filters.createdBefore) nextParams.set('createdBefore', store.filters.createdBefore);
        if (store.filters.teamId) nextParams.set('teamId', store.filters.teamId);

        setSearchParams(nextParams, { replace: true });
    }, [store.filters, store.page, setSearchParams]);
}
