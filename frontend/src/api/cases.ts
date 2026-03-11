import { ApiClient, api } from './client';
import type { CaseListFilters, CaseListResponse, CaseListItem, CaseTagSummary, CaseSummaryStats, UserSummary } from '@/types/cases';
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

// Shape returned by the Go backend for GET /cases
interface BackendCaseListItem {
    id: string;
    referenceNumber: string;
    caseType: string;
    status: string;
    currentStage: string | null;
    priority: string | null;
    borrowerName: string | null;
    assignedTo: string | null;
    assignedToName: string | null;
    slaStatus: string;
    slaRemainingMinutes: number;
    createdAt: string;
    updatedAt: string;
    tags: { tagCode: string; tagValue: string | null }[];
}

interface BackendCaseListResponse {
    items: BackendCaseListItem[];
    total: number;
    page: number;
    limit: number;
}

function buildUserSummary(userId: string | null, displayName: string | null): UserSummary | null {
    if (!userId) return null;
    const name = displayName || userId.substring(0, 8);
    const parts = name.trim().split(/\s+/);
    const initials = parts.length >= 2
        ? (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
        : name.substring(0, 2).toUpperCase();
    return { userId, displayName: name, initials };
}

function mapBackendItemToFrontend(item: BackendCaseListItem): CaseListItem {
    const tags: CaseTagSummary[] = (item.tags || []).map(t => ({
        tagCode: t.tagCode,
        tagValue: t.tagValue,
        category: deriveTagCategory(t.tagCode),
    }));

    // Derive complexity from tags (first COMPLEXITY-category tag)
    const complexityTag = tags.find(t => t.category === 'COMPLEXITY');
    const complexity = complexityTag ? complexityTag.tagCode as CaseListItem['complexity'] : null;

    // Derive isVip from tags
    const isVip = tags.some(t => t.category === 'VIP_PRIORITY');

    // Derive hasBlockingErrors from tags
    const hasBlockingErrors = tags.some(t => t.category === 'DOCUMENT_ERROR');

    // Compute slaDueAt from slaRemainingMinutes
    const slaDueAt = item.slaRemainingMinutes
        ? new Date(Date.now() + item.slaRemainingMinutes * 60 * 1000).toISOString()
        : null;

    return {
        id: item.id,
        reference: item.referenceNumber,
        title: item.borrowerName || item.referenceNumber,
        description: `${item.caseType} — Stage: ${item.currentStage || 'N/A'}`,
        priority: (item.priority as CaseListItem['priority']) || 'MEDIUM',
        status: item.status as CaseListItem['status'],
        complexity,
        requiredSkills: [],
        tags: tags.filter(t => t.category !== 'COMPLEXITY'),
        assignedUser: buildUserSummary(item.assignedTo, item.assignedToName),
        slaDueAt,
        createdAt: item.createdAt,
        updatedAt: item.updatedAt,
        hasBlockingErrors,
        isVip,
        stageCode: item.currentStage || '',
    };
}

export async function listCases(
    filters: CaseListFilters,
    page: number,
    pageSize: number,
    signal?: AbortSignal,
): Promise<CaseListResponse> {
    const scope = filters.assignedToMe ? 'my' : filters.teamId === 'current' ? 'team' : 'all';
    const params: Record<string, unknown> = {
        scope,
        query: filters.search || undefined,
        status: filters.statuses.length ? filters.statuses.join(',') : undefined,
        priority: filters.priorities.length ? filters.priorities.join(',') : undefined,
        tags: filters.skillCodes.length ? filters.skillCodes.join(',') : undefined,
        dateFrom: filters.createdAfter || undefined,
        dateTo: filters.createdBefore || undefined,
        page,
        limit: pageSize,
    };

    const response = await api.get<BackendCaseListResponse>('/cases', {
        params,
        signal
    });

    const backend = response.data;
    const items = (backend.items || []).map(mapBackendItemToFrontend);

    return {
        items,
        totalCount: backend.total,
        page: backend.page,
        pageSize: backend.limit,
        hasNextPage: backend.page * backend.limit < backend.total,
    };
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
