import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { CasesApi, GetCasesParams, CreateCaseRequest, UpdateCaseRequest } from '../api/cases';

export const useCases = (params?: GetCasesParams) => {
    return useQuery({
        queryKey: ['cases', params],
        queryFn: () => CasesApi.list(params),
        staleTime: 30000, // 30 seconds
    });
};

export const useCase = (id: string) => {
    return useQuery({
        queryKey: ['cases', id],
        queryFn: () => CasesApi.get(id),
        enabled: !!id,
    });
};

export const useCreateCase = () => {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: CreateCaseRequest) => CasesApi.create(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['cases'] });
        },
    });
};

export const useUpdateCase = (id: string) => {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: UpdateCaseRequest) => CasesApi.update(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['cases', id] });
            queryClient.invalidateQueries({ queryKey: ['cases'] });
        },
    });
};
