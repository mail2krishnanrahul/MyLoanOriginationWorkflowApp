import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { apiFetch } from '@/lib/api/client';
import { queryKeys } from '@/hooks/query-keys';
import type { CaseDetail, UpdateCaseSummaryPayload } from '@/lib/api/types';

export function useCaseDetail(caseId: string) {
  return useQuery({
    queryKey: queryKeys.caseDetail(caseId),
    enabled: Boolean(caseId),
    queryFn: ({ signal }) => apiFetch<CaseDetail>(`/api/cases/${caseId}`, { signal })
  });
}

interface CaseActionPayload {
  action: 'SUSPEND' | 'WITHDRAW' | 'EMERGENCY_CLOSE' | 'RELEASE';
  reason?: string;
}

export function useCaseAction(caseId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (payload: CaseActionPayload) =>
      apiFetch(`/api/cases/${caseId}/actions`, {
        method: 'POST',
        body: payload
      }),
    onSuccess: () => {
      toast.success('Case action submitted');
      void queryClient.invalidateQueries({ queryKey: queryKeys.caseDetail(caseId) });
      void queryClient.invalidateQueries({ queryKey: ['cases'] });
    },
    onError: (error: Error) => {
      toast.error(error.message || 'Failed to submit case action');
    }
  });
}

// ---------------------------------------------------------------------------
// Patch case summary (classifier step)
// ---------------------------------------------------------------------------
export function usePatchCaseSummary(caseId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (payload: UpdateCaseSummaryPayload) =>
      apiFetch<CaseDetail>(`/api/cases/${caseId}/summary`, {
        method: 'PATCH',
        body: payload
      }),
    onSuccess: (updated) => {
      // Seed the cache with the server-returned updated detail
      queryClient.setQueryData(queryKeys.caseDetail(caseId), updated);
      void queryClient.invalidateQueries({ queryKey: ['cases'] });
      toast.success('Case summary updated');
    },
    onError: (error: Error) => {
      toast.error(error.message || 'Failed to update case summary');
    }
  });
}
