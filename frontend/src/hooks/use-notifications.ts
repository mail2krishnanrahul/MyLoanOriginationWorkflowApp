import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { apiFetch } from '@/lib/api/client';
import { queryKeys } from '@/hooks/query-keys';
import type { CaseNotification } from '@/lib/api/types';

export function useCaseNotifications(caseId: string) {
  return useQuery({
    queryKey: queryKeys.caseNotifications(caseId),
    enabled: Boolean(caseId),
    queryFn: ({ signal }) => apiFetch<CaseNotification[]>(`/api/cases/${caseId}/notifications`, { signal })
  });
}

export function useSendNotification(caseId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (payload: {
      templateId: string;
      recipient: string;
      subject: string;
      body: string;
      channel: 'EMAIL' | 'SMS' | 'IN_APP';
    }) =>
      apiFetch('/api/notifications/send', {
        method: 'POST',
        body: {
          caseId,
          ...payload
        }
      }),
    onSuccess: () => {
      toast.success('Notification sent');
      void queryClient.invalidateQueries({ queryKey: queryKeys.caseNotifications(caseId) });
    },
    onError: (error: Error) => {
      toast.error(error.message || 'Unable to send notification');
    }
  });
}

export function useResendNotification(caseId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (notificationId: string) =>
      apiFetch(`/api/notifications/${notificationId}/resend`, {
        method: 'POST'
      }),
    onSuccess: () => {
      toast.success('Notification re-sent');
      void queryClient.invalidateQueries({ queryKey: queryKeys.caseNotifications(caseId) });
    },
    onError: (error: Error) => {
      toast.error(error.message || 'Unable to resend notification');
    }
  });
}
