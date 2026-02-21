import { useQuery } from '@tanstack/react-query';
import { apiFetch } from '@/lib/api/client';
import { queryKeys } from '@/hooks/query-keys';
import type { WorkbasketSummary, WorkbasketTask } from '@/lib/api/types';

export function useWorkbaskets() {
  return useQuery({
    queryKey: queryKeys.workbaskets(),
    queryFn: ({ signal }) => apiFetch<WorkbasketSummary[]>('/api/workbaskets', { signal })
  });
}

export function useWorkbasketTasks(workbasketId: string) {
  return useQuery({
    queryKey: queryKeys.workbasketTasks(workbasketId),
    enabled: Boolean(workbasketId),
    queryFn: ({ signal }) =>
      apiFetch<WorkbasketTask[]>(`/api/workbaskets/${workbasketId}/tasks`, {
        signal
      })
  });
}
