import { Activity, CheckCircle, Clock, Users, Workflow } from 'lucide-react';
import { StatCard } from '@/components/domain/charts/StatCard';
import { TrendLineChart } from '@/components/domain/charts/TrendLineChart';
import { GaugeChart } from '@/components/domain/charts/GaugeChart';
import { Card, CardHeader, CardTitle } from '@/components/ui/Card';
import { usePersonalDashboard } from '@/hooks/use-dashboard';
import { SkeletonCard } from '@/components/ui/LoadingSkeleton';
import { cn } from '@/lib/cn';

interface Props {
    disableHeader?: boolean;
}

function getGreeting() {
    const hour = new Date().getHours();
    if (hour < 12) return 'Good morning';
    if (hour < 17) return 'Good afternoon';
    return 'Good evening';
}

function formatDate() {
    return new Date().toLocaleDateString('en-AU', {
        weekday: 'long',
        year: 'numeric',
        month: 'long',
        day: 'numeric'
    });
}

export default function PersonalDashboardPage({ disableHeader }: Props = {}) {
    const { data, isLoading, isError } = usePersonalDashboard();

    if (isLoading) {
        return (
            <div className="space-y-4">
                <SkeletonCard className="h-28" />
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
        <div className="space-y-6">
            {/* ── Hero banner ── */}
            {!disableHeader && (
                <div
                    className="relative overflow-hidden rounded-2xl px-6 py-6 text-white"
                    style={{
                        background: 'linear-gradient(135deg, #0f82ff 0%, #6d28d9 60%, #0ea5e9 100%)',
                        boxShadow: '0 8px 32px rgb(15 130 255 / 0.28)'
                    }}
                >
                    {/* Subtle circles */}
                    <span className="pointer-events-none absolute -right-10 -top-10 size-48 rounded-full bg-white/5" />
                    <span className="pointer-events-none absolute -bottom-8 right-20 size-32 rounded-full bg-white/5" />

                    <p className="text-sm font-medium text-blue-200">{formatDate()}</p>
                    <h2 className="mt-0.5 text-2xl font-bold tracking-tight">
                        {getGreeting()}, Alex 👋
                    </h2>
                    <p className="mt-1 text-sm text-blue-100/80">
                        Here's a snapshot of your workload and team performance today.
                    </p>
                </div>
            )}

            {/* ── KPI row ── */}
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                <StatCard
                    title="Tasks Due Today"
                    value={data.tasksDueToday}
                    icon={<Clock className="size-5 text-blue-600 dark:text-blue-400" />}
                    className={cn('border stat-blue')}
                />
                <StatCard
                    title="Tasks Overdue"
                    value={data.tasksOverdue}
                    icon={<Activity className="size-5 text-red-500 dark:text-red-400" />}
                    className={cn('border stat-red')}
                />
                <StatCard
                    title="Awaiting Approval"
                    value={data.casesAwaitingApproval}
                    icon={<CheckCircle className="size-5 text-green-600 dark:text-green-400" />}
                    className={cn('border stat-green')}
                />
                <StatCard
                    title="Avg Resolution"
                    value="4.2 days"
                    trend={{ value: 1.5, label: 'vs last week' }}
                    icon={<Users className="size-5 text-amber-600 dark:text-amber-400" />}
                    className={cn('border stat-amber')}
                />
            </div>

            {/* ── Charts ── */}
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
                        series={[{ key: 'value', color: '#0f82ff', name: 'SLA %' }]}
                    />
                </div>
            </div>

            {/* ── Activity & Team ── */}
            <div className="grid gap-4 lg:grid-cols-2">
                <Card className="p-5">
                    <CardHeader className="px-0 pt-0">
                        <CardTitle className="flex items-center gap-2">
                            <Workflow className="size-4 text-brand-500" />
                            Recent Activity
                        </CardTitle>
                    </CardHeader>
                    <div className="space-y-1">
                        {[1, 2, 3, 4, 5].map((i) => (
                            <div
                                key={i}
                                className="flex items-center justify-between rounded-lg px-2 py-2.5 transition hover:bg-neutral-50 dark:hover:bg-white/5"
                            >
                                <div className="flex items-center gap-3">
                                    <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-brand-50 dark:bg-brand-500/10">
                                        <CheckCircle className="size-3.5 text-brand-600 dark:text-brand-400" />
                                    </span>
                                    <div>
                                        <p className="text-sm font-medium text-neutral-900 dark:text-neutral-100">
                                            Document Verification completed
                                        </p>
                                        <p className="font-mono text-[11px] text-brand-600 dark:text-brand-400">
                                            LOAN-2026-00{i}
                                        </p>
                                    </div>
                                </div>
                                <span className="shrink-0 text-xs text-neutral-400">
                                    {i}h ago
                                </span>
                            </div>
                        ))}
                    </div>
                </Card>

                <Card className="p-5">
                    <CardHeader className="px-0 pt-0">
                        <CardTitle className="flex items-center gap-2">
                            <Users className="size-4 text-brand-500" />
                            Team Updates
                        </CardTitle>
                    </CardHeader>
                    <div className="space-y-4">
                        <div className="rounded-xl border border-amber-200 bg-amber-50/60 p-3 dark:border-amber-500/25 dark:bg-amber-500/8">
                            <p className="text-sm font-semibold text-amber-800 dark:text-amber-300">
                                📢 Procedure Update
                            </p>
                            <p className="mt-1 text-xs leading-relaxed text-amber-700 dark:text-amber-400">
                                New identity verification guidelines for high-risk profiles take effect tomorrow.
                            </p>
                        </div>
                        <div className="space-y-2">
                            <p className="text-[11px] font-semibold uppercase tracking-widest text-neutral-400">
                                Top Performers
                            </p>
                            {[
                                { initials: 'SJ', name: 'Sarah Jenkins', count: 24 },
                                { initials: 'MR', name: 'Marcus Reed', count: 19 },
                            ].map((p) => (
                                <div key={p.initials} className="flex items-center gap-3 rounded-lg px-2 py-1.5 transition hover:bg-neutral-50 dark:hover:bg-white/5">
                                    <div
                                        className="flex size-8 shrink-0 items-center justify-center rounded-lg text-xs font-bold text-white"
                                        style={{ background: 'linear-gradient(135deg, #0f82ff 0%, #7c3aed 100%)' }}
                                    >
                                        {p.initials}
                                    </div>
                                    <div className="text-sm">
                                        <span className="font-medium text-neutral-900 dark:text-neutral-100">{p.name}</span>
                                        <span className="ml-1.5 text-xs text-neutral-500">({p.count} cases closed)</span>
                                    </div>
                                </div>
                            ))}
                        </div>
                    </div>
                </Card>
            </div>
        </div>
    );
}
