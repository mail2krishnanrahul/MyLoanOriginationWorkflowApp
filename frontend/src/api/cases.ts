import { ApiClient, api } from './client';
import type { CaseListFilters, CaseListResponse, CaseSummaryStats } from '@/types/cases';
import { deriveTagCategory } from '@/types/cases';

export interface Case {
    id: string;
    referenceNumber: string;
    caseTypeCode: string;
    status: string;
    currentStageCode: string;
    metadata: Record<string, any>;
    createdAt: string;
    updatedAt: string;
}

export interface GetCasesParams {
    status?: string;
    type?: string;
    limit?: number;
    offset?: number;
}

export interface CreateCaseRequest {
    caseTypeCode: string;
    payload: Record<string, any>;
}

export interface UpdateCaseRequest {
    metadata?: Record<string, any>;
    status?: string;
}

export const CasesApi = {
    list: (params?: GetCasesParams) => ApiClient.get<Case[]>('/cases', params),
    get: (id: string) => ApiClient.get<Case>(`/cases/${id}`),
    create: (data: CreateCaseRequest) => ApiClient.post<Case>('/cases', data),
    update: (id: string, data: UpdateCaseRequest) => ApiClient.patch<Case>(`/cases/${id}`, data),
};

export async function listCases(
    filters: CaseListFilters,
    page: number,
    pageSize: number,
    signal?: AbortSignal,
): Promise<CaseListResponse> {
    const params = {
        q: filters.search || undefined,
        status: filters.statuses.length ? filters.statuses : undefined,
        priority: filters.priorities.length ? filters.priorities : undefined,
        complexity: filters.complexities.length ? filters.complexities : undefined,
        skill_code: filters.skillCodes.length ? filters.skillCodes : undefined,
        assigned_to_me: filters.assignedToMe ? true : undefined,
        has_blocking_errors: filters.hasBlockingErrors ? true : undefined,
        is_vip: filters.isVip ? true : undefined,
        sla_due_before: filters.slaDueBefore || undefined,
        created_after: filters.createdAfter || undefined,
        created_before: filters.createdBefore || undefined,
        team_id: filters.teamId || undefined,
        page,
        page_size: pageSize
    };

    const response = await api.get<CaseListResponse>('/cases', {
        params,
        signal
    });

    // Derive tag categories from tag codes since the backend doesn't store category.
    if (response.data?.items) {
        for (const item of response.data.items) {
            if (item.tags) {
                item.tags = item.tags.map(tag => ({
                    ...tag,
                    category: tag.category || deriveTagCategory(tag.tagCode),
                }));
            }
        }
    }

    return response.data;
}

export async function getCaseSummaryStats(
    tenantId: string,
    signal?: AbortSignal,
): Promise<CaseSummaryStats> {
    const response = await api.get<CaseSummaryStats>('/cases/summary-stats', {
        headers: { 'X-Tenant-ID': tenantId },
        signal
    });
    return response.data;
}
