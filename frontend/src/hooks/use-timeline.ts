import { useQuery } from '@tanstack/react-query';
import { apiFetch } from '@/lib/api/client';
import { queryKeys } from '@/hooks/query-keys';
import type { TimelineEvent } from '@/lib/api/types';

export function useCaseTimeline(caseId: string) {
  return useQuery({
    queryKey: queryKeys.caseTimeline(caseId),
    enabled: Boolean(caseId),
    queryFn: ({ signal }) => apiFetch<TimelineEvent[]>(`/api/cases/${caseId}/timeline`, { signal })
  });
}
