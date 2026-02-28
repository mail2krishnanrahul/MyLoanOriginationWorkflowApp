import { useQuery } from '@tanstack/react-query';
import { getCaseSummaryStats } from '@/api/cases';

export function useCaseSummaryStats() {
    const tenantId = import.meta.env.VITE_TENANT_ID || 'DEFAULT';

    return useQuery({
        queryKey: ['cases', 'summary-stats', tenantId],
        queryFn: async ({ signal }) => {
            return getCaseSummaryStats(tenantId, signal);
        },
        staleTime: 60 * 1000, // 60 seconds
        refetchInterval: 60 * 1000, // 60 seconds
    });
}
