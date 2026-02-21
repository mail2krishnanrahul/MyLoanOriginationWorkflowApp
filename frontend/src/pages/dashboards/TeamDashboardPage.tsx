import { Activity, Clock, ShieldAlert, Target } from 'lucide-react';
import { StatCard } from '@/components/domain/charts/StatCard';
import { TrendLineChart } from '@/components/domain/charts/TrendLineChart';
import { StackedBarChart } from '@/components/domain/charts/StackedBarChart';
import { FunnelChart } from '@/components/domain/charts/FunnelChart';
import { Card, CardHeader, CardTitle, CardDescription } from '@/components/ui/Card';
import { useTeamDashboard } from '@/hooks/use-dashboard';
import { SkeletonCard } from '@/components/ui/LoadingSkeleton';

export default function TeamDashboardPage() {
    const { data, isLoading, isError } = useTeamDashboard();

    if (isLoading) {
        return (
            <div className="space-y-4">
                <div className="grid gap-4 md:grid-cols-4">
                    <SkeletonCard className="h-28" />
                    <SkeletonCard className="h-28" />
                    <SkeletonCard className="h-28" />
                    <SkeletonCard className="h-28" />
                </div>
                <div className="grid gap-4 lg:grid-cols-3">
                    <SkeletonCard className="h-80" />
                    <SkeletonCard className="h-80" />
                    <SkeletonCard className="h-80" />
                </div>
            </div>
        );
    }

    if (isError || !data) {
        return <div className="p-4 text-danger-500">Failed to load team dashboard.</div>;
    }

    return (
        <div className="space-y-4">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-xl font-bold text-neutral-900 dark:text-neutral-100">Team Dashboard</h2>
                    <p className="text-sm text-neutral-500 dark:text-neutral-400">Supervisor overview of workload, throughput, and SLAs</p>
                </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                <StatCard
                    title="Active Cases"
                    value={data.metrics.activeCases}
                    trend={{ value: data.metrics.trends.activeCases, label: 'vs last week' }}
                    icon={<Activity className="size-5" />}
                />
                <StatCard
                    title="Completed (This Week)"
                    value={data.metrics.completedThisWeek}
                    trend={{ value: data.metrics.trends.completedThisWeek, label: 'vs last week' }}
                    icon={<Target className="size-5" />}
                />
                <StatCard
                    title="Avg Resolution"
                    value={`${data.metrics.avgResolutionDays} days`}
                    trend={{ value: data.metrics.trends.avgResolutionDays, label: 'vs last week' }}
                    icon={<Clock className="size-5" />}
                />
                <StatCard
                    title="SLA Compliance"
                    value={`${data.metrics.slaCompliancePercent}%`}
                    trend={{ value: data.metrics.trends.slaCompliancePercent, label: 'vs last week' }}
                    icon={<ShieldAlert className="size-5" />}
                />
            </div>

            <div className="grid gap-4 lg:grid-cols-3">
                <TrendLineChart
                    title="Case Throughput"
                    description="30-day closing volume"
                    data={data.throughputTrend}
                    xAxisKey="date"
                    series={[{ key: 'value', color: '#6366f1', name: 'Volume' }]}
                />
                <FunnelChart
                    title="Active Pipeline"
                    description="Case volume by stage"
                    data={data.stageFunnel}
                    colors={['#3b82f6', '#10b981', '#f59e0b', '#8b5cf6', '#ec4899', '#64748b']}
                />
                <StackedBarChart
                    title="SLA Breach Trend"
                    description="Rolling 4-week violations"
                    data={data.slaBreachTrend}
                    xAxisKey="week"
                    bars={[
                        { key: 'critical', color: '#ef4444', name: 'Critical' },
                        { key: 'high', color: '#f97316', name: 'High' },
                        { key: 'normal', color: '#3b82f6', name: 'Normal' }
                    ]}
                />
            </div>

            <Card>
                <CardHeader>
                    <CardTitle>Team Workload</CardTitle>
                    <CardDescription>Real-time capacity and performance metrics by member</CardDescription>
                </CardHeader>
                <div className="overflow-x-auto p-4 pt-0">
                    <table className="w-full text-left text-sm">
                        <thead className="bg-neutral-50 text-xs uppercase text-neutral-500 dark:bg-neutral-800 dark:text-neutral-400">
                            <tr>
                                <th className="px-4 py-3 font-medium">Team Member</th>
                                <th className="px-4 py-3 font-medium text-right">Active Cases</th>
                                <th className="px-4 py-3 font-medium text-right">Tasks In Progress</th>
                                <th className="px-4 py-3 font-medium text-right">Tasks Completed Today</th>
                                <th className="px-4 py-3 font-medium text-right">Avg Handle (mins)</th>
                                <th className="px-4 py-3 font-medium text-right">SLA Compliance</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-neutral-200 dark:divide-neutral-700">
                            {data.teamWorkload.map((member) => (
                                <tr key={member.userId} className="hover:bg-neutral-50 dark:hover:bg-neutral-800">
                                    <td className="px-4 py-3 font-medium text-neutral-900 dark:text-neutral-100">{member.name}</td>
                                    <td className="px-4 py-3 text-right">{member.activeCases}</td>
                                    <td className="px-4 py-3 text-right">{member.tasksInProgress}</td>
                                    <td className="px-4 py-3 text-right">{member.tasksCompletedToday}</td>
                                    <td className="px-4 py-3 text-right">{member.avgHandleTimeMinutes}</td>
                                    <td className="px-4 py-3 text-right">
                                        <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-semibold ${member.slaCompliancePercent >= 95 ? 'bg-success-100 text-success-700' :
                                                member.slaCompliancePercent >= 90 ? 'bg-warning-100 text-warning-700' :
                                                    'bg-danger-100 text-danger-700'
                                            }`}>
                                            {member.slaCompliancePercent}%
                                        </span>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            </Card>
        </div>
    );
}
