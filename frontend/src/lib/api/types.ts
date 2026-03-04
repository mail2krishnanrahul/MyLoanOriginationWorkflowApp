export type Priority = 'CRITICAL' | 'HIGH' | 'NORMAL' | 'LOW';
export type SlaState = 'ON_TRACK' | 'WARNING' | 'BREACHED';

export type CaseComplexity =
  | 'SIMPLE'
  | 'STANDARD_1'
  | 'STANDARD_2'
  | 'COMPLEX'
  | 'NON_STANDARD';

export const COMPLEXITY_LABELS: Record<CaseComplexity, string> = {
  SIMPLE: 'Simple',
  STANDARD_1: 'Standard 1',
  STANDARD_2: 'Standard 2',
  COMPLEX: 'Complex',
  NON_STANDARD: 'Non-Standard',
};

/** Default target-close window in calendar days, matching backend complexitySLADays. */
export const COMPLEXITY_SLA_DAYS: Record<CaseComplexity, number> = {
  SIMPLE: 10,
  STANDARD_1: 20,
  STANDARD_2: 30,
  COMPLEX: 45,
  NON_STANDARD: 60,
};

export type CaseStatus =
  | 'OPEN'
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
  borrowerName?: string | null;
  caseType: string;
  currentStage?: string | null;
  status: CaseStatus;
  priority?: Priority | null;
  // API returns a UUID string (not a nested user object)
  assignedTo?: string | null;
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
  borrowerName?: string | null;
  caseType: string;
  caseTypeVersion?: number;
  status: CaseStatus;
  currentStage?: string | null;
  assignedTo?: string | null;
  priority?: Priority | null;
  productType?: string | null;
  loanAmount?: number;
  // Classifier-set summary fields
  complexity?: CaseComplexity | null;
  isVip?: boolean;
  targetCloseDate?: string | null;
  channel?: string | null;
  officer?: string | null;
  // SLA
  slaStatus: SlaState;
  slaRemainingMinutes: number;
  // Progress
  percentComplete: number;
  tasksTotal: number;
  tasksCompleted: number;
  // Related
  stageHistory: StageHistoryEntry[];
  subCases: SubCaseSummary[];
  activities: ActivitySummary[];
  // Legacy/extra fields (may be absent)
  stage?: string;
  stageDescription?: string;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  metadata?: Record<string, unknown>;
  dealSnapshot?: DealPayload;
}

/** Payload for PATCH /api/cases/:id/summary */
export interface UpdateCaseSummaryPayload {
  complexity?: CaseComplexity | null;
  isVip?: boolean;
  targetCloseDate?: string | null; // YYYY-MM-DD
  loanAmount?: number;
  productType?: string;
  channel?: string;
  officer?: string;
  borrowerName?: string;
}

export interface StageHistoryEntry {
  fromStage?: string | null;
  toStage: string;
  isRegression: boolean;
  reason?: string | null;
  triggeredBy: string;
  transitionAt: string;
}

export interface SubCaseSummary {
  caseId: string;
  referenceNumber: string;
  caseType: string;
  status: CaseStatus;
  currentStage?: string | null;
}

export interface ActivitySummary {
  activityCode: string;
  tasks: TaskSummaryNested[];
  statusCounts: Record<string, number>;
  total: number;
  completed: number;
}

// TaskSummaryNested is the shape of tasks inside caseDetail.activities[]
export interface TaskSummaryNested {
  taskId: string;
  name: string;
  status: TaskStatus;
  priorityRaw?: number;
  assignedService?: string | null;
  dueAt?: string | null;
  completedAt?: string | null;
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

// --- Deal 360 Complex Structure ---

export interface CurrencyAmount {
  amount: number;
  currency: string;
}

export interface Address {
  line1: string;
  line2?: string;
  suburb: string;
  state: string;
  postcode: string;
  country: string;
}

export interface LegalParty {
  partyId: string;
  partyType: 'COMPANY' | 'INDIVIDUAL' | 'TRUST';
  legalName: string;
  companyType?: string;
  regulatory?: {
    abn?: string;
    acn?: string;
    tfnStatus?: string;
    gstRegistered?: boolean;
  };
  contact?: {
    email?: string;
    phone?: string;
    address?: Address;
  };
  roles: string[];
}

export interface TrustStructure {
  trustId: string;
  trustName: string;
  trustType: string;
  trustDeedDate?: string;
  trustees: Array<{
    partyId: string;
    trusteeType: string;
    capacityStatement?: string;
  }>;
  appointor?: string;
  regulatory?: {
    tfnStatus?: string;
  };
}

export interface Fee {
  feeType: string;
  amount: CurrencyAmount;
}

export interface FacilityPricing {
  rateType: 'VARIABLE' | 'FIXED';
  interestRate: number;
  margin: number;
  index: string;
  fees?: Fee[];
}

export interface Guarantee {
  guaranteeType: string;
  guarantor: {
    guarantorType: string;
    partyId?: string;
    relationshipToBorrower?: string;
  };
  scope: string;
  limitedAmount?: CurrencyAmount;
  notes?: string;
}

export interface Facility {
  facilityId: string;
  product: string;
  status: string;
  purpose: string;
  facilityLimit: CurrencyAmount;
  umbrellaConsumption?: {
    consumptionAmount: CurrencyAmount;
    rationale?: string;
  };
  pricing?: FacilityPricing;
  term?: {
    startDate: string;
    endDate: string;
    termMonths: number;
  };
  repayment?: {
    repaymentType: string;
    frequency: string;
    amortisationMonths: number;
  };
  guarantees?: Guarantee[];
  settledAt?: string | null;
  closedAt?: string | null;
}

export interface BorrowingEntity {
  borrowingEntityId: string;
  borrowingEntityType: 'DIRECT' | 'TRUST';
  trustId?: string;
  displayName: string;
  obligors: Array<{
    partyId: string;
    liabilityType: string;
    capacity: string;
    capacityStatement?: string;
  }>;
  facilities: Facility[];
}

export interface UmbrellaLimit {
  umbrellaLimitId: string;
  approvedLimit: CurrencyAmount;
  currentUtilisation: CurrencyAmount;
  availableHeadroom: CurrencyAmount;
  exposureMethod: string;
  lastReconciledAt: string;
}

export interface AssetValuation {
  value: CurrencyAmount;
  valuationDate: string;
  basis: string;
  valuer: string;
}

export interface Asset {
  assetId: string;
  assetType: 'PROPERTY' | 'GSA' | 'VEHICLE' | 'TERM_DEPOSIT' | 'OTHER';
  propertyType?: string;
  address?: Address;
  titleReference?: string;
  occupancyStatus?: string;
  securedParty?: string;
  coverage?: string;
  ppsrRegistrationNumber?: string;
  valuation?: AssetValuation;
}

export interface Collateral {
  collateralId: string;
  collateralType: string;
  securityProviderPartyIds: string[];
  ranking: string;
  assets: Asset[];
}

export interface PriorityAllocation {
  borrowingEntityId: string;
  facilityId: string;
  collateralId: string;
  allocatedAmount: CurrencyAmount;
  priority: number;
  notes?: string;
}

export interface DealSecurity {
  secured: boolean;
  unsecuredReason?: string | null;
  collaterals?: Collateral[];
  facilityAllocations?: PriorityAllocation[];
}

export interface DealPayload {
  dealId: string;
  dealName: string;
  status: string;
  banker?: {
    bankerId: string;
    channel: string;
    portfolio: string;
  };
  customerReference?: string;
  umbrellaLimit?: UmbrellaLimit;
  legalParties?: LegalParty[];
  trustStructures?: TrustStructure[];
  borrowingEntities?: BorrowingEntity[];
  security?: DealSecurity;
  lifecycleHistory?: Array<{
    operationId: string;
    type: string;
    requestedAt: string;
    requestedBy: string;
    status: string;
    summary: string;
  }>;
  notes?: string;
  createdAt?: string;
  updatedAt?: string;
}
