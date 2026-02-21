import { useQuery } from '@tanstack/react-query';

export interface DashboardMetrics {
    activeCases: number;
    completedThisWeek: number;
    avgResolutionDays: number;
    slaCompliancePercent: number;
    trends: {
        activeCases: number;
        completedThisWeek: number;
        avgResolutionDays: number;
        slaCompliancePercent: number;
    };
}

export interface ChartDataPoint {
    date: string;
    value: number;
    [key: string]: any;
}

export interface TeamMemberWorkload {
    userId: string;
    name: string;
    activeCases: number;
    tasksInProgress: number;
    tasksCompletedToday: number;
    avgHandleTimeMinutes: number;
    slaCompliancePercent: number;
}

// Mock Personal Dashboard Data
export function usePersonalDashboard() {
    return useQuery({
        queryKey: ['dashboard', 'personal'],
        queryFn: async () => {
            // In a real app, this would be: return api.get('/dashboard/personal');
            // For Phase 2, we mock the data to build the UI immediately.
            await new Promise((resolve) => setTimeout(resolve, 600)); // Simulate async fetch

            return {
                tasksDueToday: 12,
                tasksOverdue: 3,
                casesAwaitingApproval: 5,
                slaPerformance: {
                    currentPercent: 92,
                    trend: 2.5
                },
                slaTrend: [
                    { date: 'Mon', value: 85 },
                    { date: 'Tue', value: 88 },
                    { date: 'Wed', value: 92 },
                    { date: 'Thu', value: 90 },
                    { date: 'Fri', value: 95 }
                ]
            };
        }
    });
}

// Mock Team Dashboard Data
export function useTeamDashboard() {
    return useQuery({
        queryKey: ['dashboard', 'team'],
        queryFn: async () => {
            await new Promise((resolve) => setTimeout(resolve, 800));

            const metrics: DashboardMetrics = {
                activeCases: 142,
                completedThisWeek: 86,
                avgResolutionDays: 4.2,
                slaCompliancePercent: 88.5,
                trends: {
                    activeCases: -5.2,
                    completedThisWeek: 12.4,
                    avgResolutionDays: -0.8,
                    slaCompliancePercent: 1.2
                }
            };

            const throughputTrend: ChartDataPoint[] = Array.from({ length: 30 }).map((_, i) => ({
                date: new Date(Date.now() - (29 - i) * 86400000).toISOString().split('T')[0],
                value: Math.floor(Math.random() * 30) + 10
            }));

            const stageFunnel = [
                { stage: 'Intake', count: 142 },
                { stage: 'Document Collection', count: 98 },
                { stage: 'Credit Review', count: 65 },
                { stage: 'Underwriting', count: 42 },
                { stage: 'Final Approval', count: 28 },
                { stage: 'Disbursement', count: 15 }
            ];

            const slaBreachTrend = [
                { week: 'W1', critical: 2, high: 5, normal: 12 },
                { week: 'W2', critical: 1, high: 6, normal: 8 },
                { week: 'W3', critical: 3, high: 4, normal: 15 },
                { week: 'W4', critical: 0, high: 2, normal: 6 }
            ];

            const teamWorkload: TeamMemberWorkload[] = [
                { userId: 'u1', name: 'Sarah Jenkins', activeCases: 24, tasksInProgress: 8, tasksCompletedToday: 12, avgHandleTimeMinutes: 45, slaCompliancePercent: 98 },
                { userId: 'u2', name: 'Michael Chen', activeCases: 31, tasksInProgress: 12, tasksCompletedToday: 9, avgHandleTimeMinutes: 52, slaCompliancePercent: 92 },
                { userId: 'u3', name: 'Alisha Patel', activeCases: 18, tasksInProgress: 5, tasksCompletedToday: 15, avgHandleTimeMinutes: 38, slaCompliancePercent: 99 },
                { userId: 'u4', name: 'David Thompson', activeCases: 42, tasksInProgress: 15, tasksCompletedToday: 7, avgHandleTimeMinutes: 65, slaCompliancePercent: 82 },
            ];

            return { metrics, throughputTrend, stageFunnel, slaBreachTrend, teamWorkload };
        }
    });
}

// Mock Compliance Dashboard Data
export function useComplianceDashboard() {
    return useQuery({
        queryKey: ['dashboard', 'compliance'],
        queryFn: async () => {
            await new Promise((resolve) => setTimeout(resolve, 700));

            return {
                kpis: {
                    casesOnHold: 14,
                    pendingErasure: 3,
                    dualControlRequests: 8,
                    auditIntegrity: 'VALID' as 'VALID' | 'COMPROMISED'
                },
                alerts: [
                    { id: '1', level: 'HIGH', message: 'Suspicious access pattern detected on case REF-8821', time: '10 mins ago' },
                    { id: '2', level: 'CRITICAL', message: 'Audit log checksum mismatch for record ID #9021', time: '1 hour ago' },
                    { id: '3', level: 'WARNING', message: 'SLA breached by >48 hours on 5 cases', time: '3 hours ago' },
                ],
                recentActions: [
                    { id: '1', type: 'HOLD_PLACED', actor: 'c.officer1', target: 'REF-8890', time: '2026-02-21T02:15:00Z' },
                    { id: '2', type: 'DATA_ERASURE', actor: 'c.officer2', target: 'USR-901', time: '2026-02-20T14:30:00Z' },
                    { id: '3', type: 'DUAL_CONTROL_APPROVED', actor: 'c.officer1', target: 'REQ-551', time: '2026-02-20T09:15:00Z' },
                ]
            };
        }
    });
}
