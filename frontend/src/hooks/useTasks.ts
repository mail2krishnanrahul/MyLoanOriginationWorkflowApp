import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { TasksApi, GetTasksParams } from '../api/tasks';

export const useTasks = (params?: GetTasksParams) => {
    return useQuery({
        queryKey: ['tasks', params],
        queryFn: () => TasksApi.list(params),
        staleTime: 15000,
    });
};

export const useTask = (id: string) => {
    return useQuery({
        queryKey: ['tasks', id],
        queryFn: () => TasksApi.get(id),
        enabled: !!id,
    });
};

export const useAssignTask = (id: string) => {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (userId: string) => TasksApi.assign(id, userId),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['tasks', id] });
            queryClient.invalidateQueries({ queryKey: ['tasks'] });
        },
    });
};

export const useCompleteTask = (id: string) => {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (output: Record<string, any>) => TasksApi.complete(id, output),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['tasks', id] });
            queryClient.invalidateQueries({ queryKey: ['tasks'] });
            // Potentially invalidate cases too, as a case might progress
            queryClient.invalidateQueries({ queryKey: ['cases'] });
        },
    });
};
