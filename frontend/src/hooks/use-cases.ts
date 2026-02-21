import { useQuery } from '@tanstack/react-query';
import { paginatedSchema, caseListItemSchema } from '@/lib/api/schemas';
import { apiFetch } from '@/lib/api/client';
import { queryKeys } from '@/hooks/query-keys';
import type { CaseFilters, CaseListItem, PaginatedResponse } from '@/lib/api/types';

function getCasesPath(scope: CaseFilters['scope']) {
  if (scope === 'team') {
    return '/api/cases/team';
  }

  if (scope === 'all') {
    return '/api/cases';
  }

  return '/api/cases';
}

export function useCases(filters: CaseFilters, userId = 'me', teamId = 'team-1') {
  return useQuery({
    queryKey: queryKeys.cases(filters),
    queryFn: ({ signal }) =>
      apiFetch<PaginatedResponse<CaseListItem>>(getCasesPath(filters.scope), {
        signal,
        params: {
          page: filters.page,
          limit: filters.limit,
          assignedTo: filters.scope === 'my' ? userId : filters.assignedTo,
          teamId: filters.scope === 'team' ? teamId : undefined,
          status: filters.status,
          stage: filters.stage,
          caseType: filters.caseType,
          dateFrom: filters.dateFrom,
          dateTo: filters.dateTo,
          query: filters.query,
          slaStatus: filters.slaStatus,
          priority: filters.priority,
          tags: filters.tags
        },
        schema: paginatedSchema(caseListItemSchema)
      }),
    placeholderData: (previousData) => previousData
  });
}
