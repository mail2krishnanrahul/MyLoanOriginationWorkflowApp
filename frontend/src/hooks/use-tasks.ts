import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { apiFetch } from '@/lib/api/client';
import { queryKeys } from '@/hooks/query-keys';
import type { TaskDetail, TaskSummary } from '@/lib/api/types';

export function useCaseTasks(caseId: string) {
  return useQuery({
    queryKey: queryKeys.caseTasks(caseId),
    enabled: Boolean(caseId),
    queryFn: ({ signal }) => apiFetch<TaskSummary[]>(`/api/cases/${caseId}/tasks`, { signal })
  });
}

export function useTask(taskId: string) {
  return useQuery({
    queryKey: queryKeys.task(taskId),
    enabled: Boolean(taskId),
    queryFn: ({ signal }) => apiFetch<TaskDetail>(`/api/tasks/${taskId}`, { signal })
  });
}

interface SaveTaskPayload {
  outputPayload: Record<string, unknown>;
}

interface CompleteTaskPayload {
  outputPayload: Record<string, unknown>;
}

export function useSaveTask(taskId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (payload: SaveTaskPayload) =>
      apiFetch(`/api/tasks/${taskId}`, {
        method: 'PUT',
        body: payload
      }),
    onSuccess: () => {
      toast.success('Draft saved');
      void queryClient.invalidateQueries({ queryKey: queryKeys.task(taskId) });
    },
    onError: (error: Error) => {
      toast.error(error.message || 'Unable to save task draft');
    }
  });
}

export function useCompleteTask(taskId: string, caseId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (payload: CompleteTaskPayload) =>
      apiFetch(`/api/tasks/${taskId}/complete`, {
        method: 'POST',
        body: payload
      }),
    onMutate: async () => {
      await queryClient.cancelQueries({ queryKey: queryKeys.caseTasks(caseId) });
      const previous = queryClient.getQueryData<TaskSummary[]>(queryKeys.caseTasks(caseId));

      queryClient.setQueryData<TaskSummary[]>(queryKeys.caseTasks(caseId), (current) =>
        (current ?? []).map((task) =>
          task.id === taskId
            ? {
                ...task,
                status: 'DONE'
              }
            : task
        )
      );

      return { previous };
    },
    onError: (error: Error, _variables, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKeys.caseTasks(caseId), context.previous);
      }

      toast.error(error.message || 'Unable to complete task');
    },
    onSuccess: () => {
      toast.success('Task completed successfully');
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.caseTasks(caseId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.task(taskId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.caseDetail(caseId) });
    }
  });
}

export function useClaimTask(caseId?: string, workbasketId?: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (taskId: string) =>
      apiFetch(`/api/tasks/${taskId}/claim`, {
        method: 'POST'
      }),
    onSuccess: () => {
      toast.success('Task claimed');
      if (caseId) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.caseTasks(caseId) });
      }
      if (workbasketId) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.workbasketTasks(workbasketId) });
      }
      void queryClient.invalidateQueries({ queryKey: ['cases'] });
    },
    onError: (error: Error) => {
      toast.error(error.message || 'Failed to claim task');
    }
  });
}
