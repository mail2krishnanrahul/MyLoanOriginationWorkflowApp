import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { listCases } from '@/api/cases';
import type { CaseListFilters } from '@/types/cases';

export function useCaseList(filters: CaseListFilters, page: number, pageSize: number = 20) {
    return useQuery({
        queryKey: ['cases', 'list', filters, page, pageSize],
        queryFn: async ({ signal }) => {
            return listCases(filters, page, pageSize, signal);
        },
        staleTime: 30 * 1000, // 30 seconds
        gcTime: 5 * 60 * 1000, // 5 minutes
        refetchOnWindowFocus: true,
        placeholderData: keepPreviousData,
    });
}
