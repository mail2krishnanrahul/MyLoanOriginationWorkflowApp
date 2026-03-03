// src/hooks/useGetNext.ts
// TanStack Query hooks for GetNext intelligent work distribution.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
    claimNextCase,
    previewNextCases,
    skipCase,
    getQueueDepth,
    getSupervisorView,
} from '@/api/getnext';
import type { SkipCaseRequest } from '@/types/getnext';

// ─── Query keys ───────────────────────────────────────────────────────────────

export const getNextKeys = {
    preview: (caseTypeCode?: string) => ['getnext', 'preview', caseTypeCode] as const,
    queueDepth: (caseTypeCode?: string) => ['getnext', 'queue', caseTypeCode] as const,
    supervisor: () => ['getnext', 'supervisor'] as const,
};

// ─── useClaimNextCase ─────────────────────────────────────────────────────────

/**
 * Claims the highest-scored eligible case for the current user.
 * On success invalidates cases list and queue depth queries.
 */
export function useClaimNextCase() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (caseTypeCode?: string) => claimNextCase(caseTypeCode),
        onSuccess: () => {
            // Invalidate cases list so newly claimed case appears
            queryClient.invalidateQueries({ queryKey: ['cases'] });
            // Invalidate queue depth — it will have decreased by 1
            queryClient.invalidateQueries({ queryKey: ['getnext', 'queue'] });
            queryClient.invalidateQueries({ queryKey: ['getnext', 'preview'] });
        },
    });
}

// ─── useGetNextPreview ────────────────────────────────────────────────────────

/**
 * Shows the top-N scored cases without claiming them.
 * Refreshes every 30 seconds so the list stays current.
 */
export function useGetNextPreview(caseTypeCode?: string, topN: number = 3, enabled = false) {
    return useQuery({
        queryKey: getNextKeys.preview(caseTypeCode),
        queryFn: () => previewNextCases(caseTypeCode, topN),
        enabled,
        staleTime: 30_000,
        refetchInterval: 30_000,
    });
}

// ─── useQueueDepth ────────────────────────────────────────────────────────────

/**
 * Live queue depth metrics including SLA breach counts.
 * Polls every 60 seconds by default.
 */
export function useQueueDepth(caseTypeCode?: string) {
    return useQuery({
        queryKey: getNextKeys.queueDepth(caseTypeCode),
        queryFn: () => getQueueDepth(caseTypeCode),
        staleTime: 60_000,
        refetchInterval: 60_000,
    });
}

// ─── useSkipCase ──────────────────────────────────────────────────────────────

/**
 * Records a skip for a specific case.
 * Does not invalidate any cache — the case remains in the queue for others.
 */
export function useSkipCase() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (req: SkipCaseRequest) => skipCase(req),
        onSuccess: () => {
            // Refresh preview and queue depth after a skip
            queryClient.invalidateQueries({ queryKey: ['getnext', 'preview'] });
        },
    });
}

// ─── useSupervisorView ────────────────────────────────────────────────────────

/** Full supervisor dashboard data. */
export function useSupervisorView() {
    return useQuery({
        queryKey: getNextKeys.supervisor(),
        queryFn: getSupervisorView,
        staleTime: 60_000,
        refetchInterval: 60_000,
    });
}
