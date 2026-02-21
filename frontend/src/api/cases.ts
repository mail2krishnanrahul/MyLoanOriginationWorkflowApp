import { ApiClient } from './client';

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
