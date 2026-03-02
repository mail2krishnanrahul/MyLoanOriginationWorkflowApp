// src/types/getnext.ts
// TypeScript type definitions for the GetNext intelligent work distribution engine.

export interface GetNextWeights {
    wSla: number;
    wSkill: number;
    wAge: number;
    wComplexity: number;
    wValue: number;
    wAffinity: number;
    wWorkload: number;
}

export interface ScoreFactor {
    rawScore: number;
    weight: number;
    weightedScore: number;
    explanation: string;
}

export interface ScoreBreakdown {
    sla: ScoreFactor;
    skill: ScoreFactor;
    age: ScoreFactor;
    complexity: ScoreFactor;
    value: ScoreFactor;
    affinity: ScoreFactor;
    workload: ScoreFactor;
    weightsUsed: GetNextWeights;
}

export interface UserCapacityInfo {
    activeCases: number;
    maxActiveCases: number;
    capacityPct: number;   // 0.0 – 1.0
    isAtCapacity: boolean;
    isNearCapacity: boolean;
}

export interface CaseSummary {
    id: string;
    referenceNumber: string;
    caseTypeCode: string;
    currentStageCode: string;
    status: string;
    complexity: string;
    requiredSkills: string[];
    caseDueAt: string | null;   // ISO8601 or null
    createdAt: string;
}

export interface GetNextResult {
    case: CaseSummary;
    compositeScore: number;
    scoreBreakdown: ScoreBreakdown;
    queueDepth: number;
}

export interface PreviewResult {
    topCases: GetNextResult[];
    queueDepth: number;
    userCapacity: UserCapacityInfo;
}

export interface QueueDepthInfo {
    totalAllocatable: number;
    eligibleForUser: number;
    avgWaitHours: number;
    maxWaitHours: number;
    slaBreachedCount: number;
    slaAtRiskCount: number;
    byComplexity: Record<string, number>;
    bySkill: Record<string, number>;
    userCapacity: UserCapacityInfo;
}

export type SkipReason =
    | 'FREE_TEXT'
    | 'CONFLICT_OF_INTEREST'
    | 'TOO_COMPLEX'
    | 'WRONG_SKILL'
    | 'OTHER';

export interface SkipCaseRequest {
    caseId: string;
    reason: SkipReason;
    notes?: string;
}

export interface TeamWorkloadRow {
    teamId: string;
    teamName: string;
    members: number;
    activeCases: number;
    maxCases: number;
    capacityPct: number;
}

export interface SupervisorQueueView {
    queueDepth: QueueDepthInfo;
    topCases: GetNextResult[];
    teamWorkloads: TeamWorkloadRow[];
    idleOperators: unknown[];
    atCapacityOps: unknown[];
    stalledCases: CaseSummary[];
}

// Response envelope for claim when no eligible cases exist
export interface NoEligibleCasesResponse {
    message: string;
    noEligibleCases: true;
}
