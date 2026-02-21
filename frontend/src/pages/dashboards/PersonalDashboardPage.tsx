import { LayoutDashboard, Users, Workflow, Activity, CheckCircle, Clock } from 'lucide-react';
import { StatCard } from '@/components/domain/charts/StatCard';
import { TrendLineChart } from '@/components/domain/charts/TrendLineChart';
import { GaugeChart } from '@/components/domain/charts/GaugeChart';
import { Card, CardHeader, CardTitle } from '@/components/ui/Card';
import { usePersonalDashboard } from '@/hooks/use-dashboard';
import { SkeletonCard } from '@/components/ui/LoadingSkeleton';

interface Props {
    disableHeader?: boolean;
}

export default function PersonalDashboardPage({ disableHeader }: Props = {}) {
    const { data, isLoading, isError } = usePersonalDashboard();

    if (isLoading) {
        return (
            <div className="space-y-4">
                <div className="grid gap-4 md:grid-cols-4">
                    <SkeletonCard className="h-28" />
                    <SkeletonCard className="h-28" />
                    <SkeletonCard className="h-28" />
                    <SkeletonCard className="h-28" />
                </div>
                <div className="grid gap-4 md:grid-cols-2">
                    <SkeletonCard className="h-80" />
                    <SkeletonCard className="h-80" />
                </div>
            </div>
        );
    }

    if (isError || !data) {
        return <div className="p-4 text-danger-500">Failed to load personal dashboard.</div>;
    }

    return (
        <div className="space-y-4">
            {!disableHeader && (
                <div className="flex items-center justify-between">
                    <div>
                        <h2 className="text-xl font-bold text-neutral-900 dark:text-neutral-100">Personal Dashboard</h2>
                        <p className="text-sm text-neutral-500 dark:text-neutral-400">Your upcoming tasks and recent performance metrics</p>
                    </div>
                </div>
            )}

            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                <StatCard
                    title="Tasks Due Today"
                    value={data.tasksDueToday}
                    icon={<Clock className="size-5" />}
                    className="border-brand-100 bg-brand-50/50 dark:border-brand-500/20 dark:bg-brand-500/5"
                />
                <StatCard
                    title="Tasks Overdue"
                    value={data.tasksOverdue}
                    icon={<Activity className="size-5" />}
                    className="border-danger-100 bg-danger-50/50 dark:border-danger-500/20 dark:bg-danger-500/5"
                />
                <StatCard
                    title="Awaiting Approval"
                    value={data.casesAwaitingApproval}
                    icon={<CheckCircle className="size-5" />}
                />
                <StatCard
                    title="Avg Resolution"
                    value="4.2 days"
                    trend={{ value: 1.5, label: 'vs last week' }}
                    icon={<LayoutDashboard className="size-5" />}
                />
            </div>

            <div className="grid gap-4 lg:grid-cols-3">
                <div className="lg:col-span-1">
                    <GaugeChart
                        title="SLA Performance (This Week)"
                        percentage={data.slaPerformance.currentPercent}
                        color="#22c55e"
                        className="h-full"
                    />
                </div>
                <div className="lg:col-span-2">
                    <TrendLineChart
                        title="SLA Trend (Last 5 Days)"
                        description="Percentage of tasks completed within SLA"
                        data={data.slaTrend}
                        xAxisKey="date"
                        series={[{ key: 'value', color: '#3b82f6', name: 'SLA %' }]}
                    />
                </div>
            </div>

            <div className="grid gap-4 lg:grid-cols-2">
                <Card className="p-4">
                    <CardHeader className="px-0 pt-0">
                        <CardTitle className="text-base flex items-center gap-2">
                            <Workflow className="size-4" /> Recent Activity
                        </CardTitle>
                    </CardHeader>
                    <div className="space-y-3">
                        {[1, 2, 3, 4, 5].map((i) => (
                            <div key={i} className="flex items-center justify-between rounded-md p-2 hover:bg-neutral-50 dark:hover:bg-neutral-800">
                                <div>
                                    <p className="text-sm font-medium text-neutral-900 dark:text-neutral-100">Task Completed: Document Verification</p>
                                    <p className="text-xs text-brand-600 dark:text-brand-400 font-mono">CASE-2026-00{i}</p>
                                </div>
                                <span className="text-xs text-neutral-500">{i} hour{i > 1 ? 's' : ''} ago</span>
                            </div>
                        ))}
                    </div>
                </Card>

                <Card className="p-4">
                    <CardHeader className="px-0 pt-0">
                        <CardTitle className="text-base flex items-center gap-2">
                            <Users className="size-4" /> Team Updates
                        </CardTitle>
                    </CardHeader>
                    <div className="space-y-4">
                        <div className="rounded-lg border border-accent-200 bg-accent-50/50 p-3 dark:border-accent-500/30 dark:bg-accent-500/10">
                            <p className="text-sm font-semibold text-accent-800 dark:text-accent-300">📢 Procedure Update</p>
                            <p className="mt-1 text-xs text-accent-700 dark:text-accent-400">
                                New identity verification guidelines for high-risk profiles take effect tomorrow.
                            </p>
                        </div>
                        <div className="space-y-2">
                            <p className="text-xs font-semibold uppercase tracking-wide text-neutral-500">Top Performers</p>
                            <div className="flex items-center gap-3">
                                <div className="flex size-8 items-center justify-center rounded-full bg-brand-100 text-xs font-bold text-brand-700 dark:bg-brand-500/20 dark:text-brand-300">SJ</div>
                                <div className="text-sm">Sarah Jenkins <span className="text-xs text-neutral-500">(24 cases closed)</span></div>
                            </div>
                        </div>
                    </div>
                </Card>
            </div>

        </div>
    );
}
