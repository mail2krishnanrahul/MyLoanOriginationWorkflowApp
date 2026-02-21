import type { CaseFilters } from '@/lib/api/types';

export const queryKeys = {
  cases: (filters: CaseFilters) => ['cases', filters] as const,
  caseDetail: (caseId: string) => ['case-detail', caseId] as const,
  caseTasks: (caseId: string) => ['case-tasks', caseId] as const,
  task: (taskId: string) => ['task', taskId] as const,
  caseDocuments: (caseId: string) => ['case-documents', caseId] as const,
  caseApprovals: (caseId: string) => ['case-approvals', caseId] as const,
  caseTimeline: (caseId: string) => ['case-timeline', caseId] as const,
  caseNotifications: (caseId: string) => ['case-notifications', caseId] as const,
  workbaskets: () => ['workbaskets'] as const,
  workbasketTasks: (workbasketId: string) => ['workbasket-tasks', workbasketId] as const
};
