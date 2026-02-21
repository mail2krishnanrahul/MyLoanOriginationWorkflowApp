export type Priority = 'CRITICAL' | 'HIGH' | 'NORMAL' | 'LOW';
export type SlaState = 'ON_TRACK' | 'WARNING' | 'BREACHED';

export type CaseStatus =
  | 'DRAFT'
  | 'IN_PROGRESS'
  | 'PENDING_APPROVAL'
  | 'SUSPENDED'
  | 'WITHDRAWN'
  | 'CLOSED'
  | 'REJECTED';

export interface UserSummary {
  id: string;
  displayName: string;
  email?: string;
  avatarUrl?: string;
}

export interface CaseListItem {
  id: string;
  referenceNumber: string;
  borrowerName: string;
  caseType: string;
  stage: string;
  status: CaseStatus;
  priority: Priority;
  assignedTo?: UserSummary;
  slaStatus: SlaState;
  slaRemainingMinutes: number;
  tags?: string[];
  createdAt: string;
  updatedAt: string;
}

export interface PaginatedResponse<T> {
  items: T[];
  page: number;
  limit: number;
  total: number;
}

export interface CaseFilters {
  scope: 'my' | 'team' | 'all';
  status?: CaseStatus[];
  stage?: string[];
  caseType?: string;
  dateFrom?: string;
  dateTo?: string;
  query?: string;
  slaStatus?: SlaState[];
  priority?: Priority[];
  assignedTo?: string;
  tags?: string[];
  page: number;
  limit: number;
}

export interface CaseDetail {
  id: string;
  referenceNumber: string;
  borrowerName: string;
  caseType: string;
  status: CaseStatus;
  priority: Priority;
  stage: string;
  stageDescription?: string;
  loanAmount: number;
  productType: string;
  channel: string;
  officer: string;
  targetCloseDate?: string;
  createdAt: string;
  updatedAt: string;
  slaStatus: SlaState;
  slaRemainingMinutes: number;
  tasksCompleted: number;
  tasksTotal: number;
  activities: ActivitySummary[];
}

export interface ActivitySummary {
  id: string;
  name: string;
  description?: string;
  tasksCompleted: number;
  tasksTotal: number;
  tasks: TaskSummary[];
}

export type TaskStatus = 'PENDING' | 'IN_PROGRESS' | 'DONE' | 'FAILED' | 'BLOCKED';

export interface TaskSummary {
  id: string;
  name: string;
  activityName?: string;
  status: TaskStatus;
  priority: Priority;
  assignee?: UserSummary;
  dueAt?: string;
  slaStatus: SlaState;
  inputPayload?: Record<string, unknown>;
  outputPayload?: Record<string, unknown>;
}

export interface TaskDefinitionField {
  key: string;
  label: string;
  type: 'text' | 'number' | 'select' | 'date' | 'textarea' | 'file';
  required?: boolean;
  helpText?: string;
  placeholder?: string;
  options?: Array<{ label: string; value: string }>;
  min?: number;
  max?: number;
  dependsOn?: { field: string; equals: string | number | boolean };
  formula?: 'SUM' | 'DIFF' | 'MULTIPLY';
  formulaFields?: string[];
}

export interface TaskDetail extends TaskSummary {
  caseId: string;
  caseReference: string;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  outputSchema: TaskDefinitionField[];
  validationErrors?: string[];
}

export type DocumentStatus = 'PENDING' | 'UPLOADED' | 'VERIFIED' | 'REJECTED' | 'ARCHIVED';

export interface DocumentRequirement {
  id: string;
  name: string;
  status: DocumentStatus;
  uploadedCount: number;
  requiredCount: number;
  dueStage: string;
}

export interface DocumentRecord {
  id: string;
  fileName: string;
  type: string;
  sizeBytes: number;
  uploadedBy: UserSummary;
  uploadedAt: string;
  status: DocumentStatus;
  version: number;
}

export type ApprovalStatus = 'PENDING' | 'APPROVED' | 'REJECTED';

export interface ApprovalNode {
  id: string;
  tier: number;
  approvers: UserSummary[];
  status: ApprovalStatus;
  decision?: 'APPROVE' | 'REJECT';
  decidedAt?: string;
}

export interface ApprovalRequest {
  id: string;
  context: string;
  amount?: number;
  requestedBy: UserSummary;
  expiresAt: string;
  status: ApprovalStatus;
}

export interface TimelineEvent {
  id: string;
  type:
    | 'CASE_CREATED'
    | 'STAGE_CHANGED'
    | 'TASK_COMPLETED'
    | 'DOCUMENT_UPLOADED'
    | 'APPROVAL_GRANTED'
    | 'SLA_WARNING'
    | 'COMMENT'
    | 'NOTIFICATION';
  actor: UserSummary;
  timestamp: string;
  description: string;
  before?: Record<string, unknown>;
  after?: Record<string, unknown>;
}

export interface CaseNotification {
  id: string;
  channel: 'EMAIL' | 'SMS' | 'IN_APP';
  recipient: string;
  subject: string;
  sentAt: string;
  status: 'SENT' | 'FAILED';
  acknowledged: boolean;
  body: string;
}

export interface WorkbasketSummary {
  id: string;
  name: string;
  type: 'GENERAL' | 'SPECIALIST' | 'ESCALATION';
  depth: number;
  oldestTaskAgeMinutes: number;
}

export interface WorkbasketTask {
  id: string;
  taskName: string;
  caseReference: string;
  priority: Priority;
  dueAt?: string;
  waitingSince: string;
  slaStatus: SlaState;
}

export interface ApiErrorPayload {
  message: string;
  code?: string;
  details?: unknown;
}
