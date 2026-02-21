import { ApiClient } from './client';

export interface Task {
    id: string;
    caseId: string;
    taskDefinitionId: string;
    status: string; // PENDING, IN_PROGRESS, COMPLETED, FAILED
    assignedTo?: string;
    assignedTeam?: string;
    priority: number;
    createdAt: string;
    dueDate?: string;
    completedAt?: string;
}

export interface GetTasksParams {
    caseId?: string;
    status?: string;
    assignedTo?: string;
    limit?: number;
    offset?: number;
}

export const TasksApi = {
    list: (params?: GetTasksParams) => ApiClient.get<Task[]>('/tasks', params),
    get: (id: string) => ApiClient.get<Task>(`/tasks/${id}`),
    assign: (id: string, userId: string) => ApiClient.post<Task>(`/tasks/${id}/assign`, { userId }),
    complete: (id: string, output: Record<string, any>) => ApiClient.post<Task>(`/tasks/${id}/complete`, { output }),
};
