import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { apiFetch } from '@/lib/api/client';
import { queryKeys } from '@/hooks/query-keys';
import type { ApprovalNode, ApprovalRequest } from '@/lib/api/types';

export interface ApprovalsResponse {
  chain: ApprovalNode[];
  pending: ApprovalRequest[];
  history: ApprovalRequest[];
}

export function useCaseApprovals(caseId: string) {
  return useQuery({
    queryKey: queryKeys.caseApprovals(caseId),
    enabled: Boolean(caseId),
    queryFn: ({ signal }) => apiFetch<ApprovalsResponse>(`/api/cases/${caseId}/approvals`, { signal })
  });
}

export function useApprove(caseId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (approvalId: string) =>
      apiFetch(`/api/approvals/${approvalId}/approve`, {
        method: 'POST'
      }),
    onSuccess: () => {
      toast.success('Approval submitted');
      void queryClient.invalidateQueries({ queryKey: queryKeys.caseApprovals(caseId) });
    },
    onError: (error: Error) => {
      toast.error(error.message || 'Unable to approve request');
    }
  });
}

export function useReject(caseId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ approvalId, reason }: { approvalId: string; reason: string }) =>
      apiFetch(`/api/approvals/${approvalId}/reject`, {
        method: 'POST',
        body: { reason }
      }),
    onSuccess: () => {
      toast.success('Approval rejected');
      void queryClient.invalidateQueries({ queryKey: queryKeys.caseApprovals(caseId) });
    },
    onError: (error: Error) => {
      toast.error(error.message || 'Unable to reject approval request');
    }
  });
}
