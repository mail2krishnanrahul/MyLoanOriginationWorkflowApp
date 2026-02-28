export type CasePriority = 'URGENT' | 'HIGH' | 'MEDIUM' | 'LOW';

export type CaseStatus =
    | 'NEW'
    | 'ALLOCATABLE'
    | 'IN_PROGRESS'
    | 'ADDITIONAL_INFO'
    | 'IN_QA'
    | 'REMEDIATION'
    | 'COMPLETED'
    | 'WITHDRAWN'
    | 'ON_HOLD';

export type CaseComplexity = 'SIMPLE' | 'STANDARD_1' | 'STANDARD_2' | 'STANDARD_3' | 'NON_STANDARD';

export type SkillCode = string;

export enum TagCategory {
    COMPLEXITY = 'COMPLEXITY',
    DOCUMENT_ERROR = 'DOCUMENT_ERROR',
    SKILL = 'SKILL',
    VIP_PRIORITY = 'VIP_PRIORITY',
    EXCEPTION = 'EXCEPTION',
    DEFAULT = 'DEFAULT',
}

export interface CaseTagSummary {
    tagCode: string;
    tagValue: string | null;
    category: TagCategory;
}

export interface UserSummary {
    userId: string;
    displayName: string;
    initials: string;
}

export interface CaseListItem {
    id: string;
    reference: string;
    title: string;
    description: string;
    priority: CasePriority;
    status: CaseStatus;
    complexity: CaseComplexity | null;
    requiredSkills: SkillCode[];
    tags: CaseTagSummary[];
    assignedUser: UserSummary | null;
    slaDueAt: string | null;
    createdAt: string;
    updatedAt: string;
    hasBlockingErrors: boolean;
    isVip: boolean;
    stageCode: string;
}

export interface CaseListFilters {
    search: string;
    statuses: CaseStatus[];
    priorities: CasePriority[];
    complexities: CaseComplexity[];
    skillCodes: SkillCode[];
    assignedToMe: boolean;
    hasBlockingErrors: boolean;
    isVip: boolean;
    slaDueBefore: string | null;
    createdAfter: string | null;
    createdBefore: string | null;
    teamId: string | null;
}

export interface CaseListResponse {
    items: CaseListItem[];
    totalCount: number;
    page: number;
    pageSize: number;
    hasNextPage: boolean;
}

export interface CaseSummaryStats {
    totalCases: number;
    activeCases: number;
    resolvedCases: number;
    atRiskCases: number;
    myActiveCases: number;
}

const KNOWN_SKILLS = new Set([
    'CREDIT_ANALYST', 'FRAUD_SPECIALIST', 'SENIOR_UNDERWRITER',
    'LEGAL_REVIEWER', 'KYC_ANALYST', 'COMPLIANCE_OFFICER',
    'VALUATION_EXPERT', 'RELATIONSHIP_MANAGER', 'QUALITY_ASSURANCE'
]);

export function deriveTagCategory(tagCode: string): TagCategory {
    const upper = tagCode.toUpperCase();

    if (upper.startsWith('SIMPLE') || upper.startsWith('STANDARD') || upper.startsWith('NON_STANDARD')) {
        return TagCategory.COMPLEXITY;
    }

    if (upper.startsWith('DOC_') || upper.startsWith('ERROR_')) {
        return TagCategory.DOCUMENT_ERROR;
    }

    if (upper === 'VIP' || upper === 'URGENT' || upper === 'HIGH_NET_WORTH') {
        return TagCategory.VIP_PRIORITY;
    }

    if (upper === 'EXCEPTION' || upper.startsWith('PANEL_') || upper.startsWith('COMPLEX_')) {
        return TagCategory.EXCEPTION;
    }

    if (KNOWN_SKILLS.has(upper) || upper.startsWith('SKILL_')) {
        return TagCategory.SKILL;
    }

    return TagCategory.DEFAULT;
}
