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

// isUUID returns true when s looks like a valid UUID (not a sentinel like 'me').
function isUUID(s: string | undefined): boolean {
  if (!s) return false;
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(s);
}

export function useCases(filters: CaseFilters, teamId = 'team-1') {
  return useQuery({
    queryKey: queryKeys.cases(filters),
    queryFn: ({ signal }) =>
      apiFetch<PaginatedResponse<CaseListItem>>(getCasesPath(filters.scope), {
        signal,
        params: {
          // When scope=my the backend already applies assigned_user_id filter via the
          // scope clause, so we send the scope param and omit assignedTo to avoid the
          // "invalid input syntax for type uuid: 'me'" SQL error.
          scope: filters.scope,
          page: filters.page,
          limit: filters.limit,
          // Only send assignedTo when it is a real UUID – never the 'me' sentinel.
          assignedTo: isUUID(filters.assignedTo) ? filters.assignedTo : undefined,
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
