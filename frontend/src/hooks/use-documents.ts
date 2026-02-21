import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { apiFetch } from '@/lib/api/client';
import { queryKeys } from '@/hooks/query-keys';
import type { DocumentRecord, DocumentRequirement } from '@/lib/api/types';

export interface CaseDocumentsResponse {
  checklist: DocumentRequirement[];
  documents: DocumentRecord[];
}

export function useCaseDocuments(caseId: string) {
  return useQuery({
    queryKey: queryKeys.caseDocuments(caseId),
    enabled: Boolean(caseId),
    queryFn: ({ signal }) => apiFetch<CaseDocumentsResponse>(`/api/cases/${caseId}/documents`, { signal })
  });
}

export function useUploadDocument(caseId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (payload: {
      fileName: string;
      type: string;
      base64Content: string;
    }) =>
      apiFetch('/api/documents/upload', {
        method: 'POST',
        body: {
          caseId,
          ...payload
        }
      }),
    onSuccess: () => {
      toast.success('Document uploaded');
      void queryClient.invalidateQueries({ queryKey: queryKeys.caseDocuments(caseId) });
    },
    onError: (error: Error) => {
      toast.error(error.message || 'Failed to upload document');
    }
  });
}

export function useVerifyDocument(caseId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (documentId: string) =>
      apiFetch(`/api/documents/${documentId}/verify`, {
        method: 'POST'
      }),
    onSuccess: () => {
      toast.success('Document verified');
      void queryClient.invalidateQueries({ queryKey: queryKeys.caseDocuments(caseId) });
    },
    onError: (error: Error) => {
      toast.error(error.message || 'Failed to verify document');
    }
  });
}
