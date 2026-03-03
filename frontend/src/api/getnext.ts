// src/api/getnext.ts
// API layer for the GetNext intelligent work distribution engine.

import { ApiClient } from '@/api/client';
import type {
    GetNextResult,
    PreviewResult,
    QueueDepthInfo,
    SkipCaseRequest,
    SupervisorQueueView,
} from '@/types/getnext';

const BASE = '/v1/getnext';

/** Claim the highest-scored eligible case for the current user. */
export async function claimNextCase(caseTypeCode?: string): Promise<GetNextResult> {
    return ApiClient.post<GetNextResult>(`${BASE}/claim`, { caseTypeCode });
}

/** Preview top-N eligible cases without claiming. */
export async function previewNextCases(
    caseTypeCode?: string,
    n: number = 3
): Promise<PreviewResult> {
    return ApiClient.get<PreviewResult>(`${BASE}/preview`, { caseTypeCode, n });
}

/** Record a skip for a specific case. */
export async function skipCase(req: SkipCaseRequest): Promise<{ message: string }> {
    return ApiClient.post<{ message: string }>(`${BASE}/skip`, req);
}

/** Get current queue depth and metrics. */
export async function getQueueDepth(caseTypeCode?: string): Promise<QueueDepthInfo> {
    return ApiClient.get<QueueDepthInfo>(`${BASE}/queue`, { caseTypeCode });
}

/** Supervisor dashboard — team workloads, stalled cases, top queue. */
export async function getSupervisorView(): Promise<SupervisorQueueView> {
    return ApiClient.get<SupervisorQueueView>(`${BASE}/supervisor`);
}

/** Admin: refresh case_user_affinity materialised view. */
export async function refreshAffinityView(): Promise<{ message: string }> {
    return ApiClient.post<{ message: string }>(`${BASE}/refresh-affinity`, {});
}
