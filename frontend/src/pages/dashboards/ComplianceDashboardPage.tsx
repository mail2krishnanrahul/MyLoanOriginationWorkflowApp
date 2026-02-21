import { FileWarning, LockKeyhole, MailWarning, ShieldCheck } from 'lucide-react';
import { StatCard } from '@/components/domain/charts/StatCard';
import { Card, CardHeader, CardTitle, CardDescription } from '@/components/ui/Card';
import { useComplianceDashboard } from '@/hooks/use-dashboard';
import { SkeletonCard } from '@/components/ui/LoadingSkeleton';

export default function ComplianceDashboardPage() {
    const { data, isLoading, isError } = useComplianceDashboard();

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
        return <div className="p-4 text-danger-500">Failed to load compliance dashboard.</div>;
    }

    return (
        <div className="space-y-4">
            <div className="flex items-center justify-between">
                <div>
                    <h2 className="text-xl font-bold text-neutral-900 dark:text-neutral-100">Compliance & Audit</h2>
                    <p className="text-sm text-neutral-500 dark:text-neutral-400">Monitor regulatory holds, controls, and system integrity</p>
                </div>
            </div>

            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                <StatCard
                    title="Regulatory Holds"
                    value={data.kpis.casesOnHold}
                    icon={<LockKeyhole className="size-5" />}
                    className="border-warning-200 bg-warning-50/50 dark:border-warning-500/20 dark:bg-warning-500/5"
                />
                <StatCard
                    title="Pending Dual-Control"
                    value={data.kpis.dualControlRequests}
                    icon={<ShieldCheck className="size-5" />}
                />
                <StatCard
                    title="Erasure Requests"
                    value={data.kpis.pendingErasure}
                    icon={<FileWarning className="size-5" />}
                />
                <StatCard
                    title="Audit Log Integrity"
                    value={data.kpis.auditIntegrity}
                    icon={<ShieldCheck className="size-5" />}
                    className={data.kpis.auditIntegrity === 'VALID'
                        ? 'border-success-200 bg-success-50/50 text-success-700'
                        : 'border-danger-200 bg-danger-50/50 text-danger-700'}
                />
            </div>

            <div className="grid gap-4 md:grid-cols-2">
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <MailWarning className="size-5 text-warning-500" /> System Alerts
                        </CardTitle>
                        <CardDescription>Escalations requiring immediate compliance offier review</CardDescription>
                    </CardHeader>
                    <div className="p-4 pt-0 space-y-3">
                        {data.alerts.map((alert) => (
                            <div
                                key={alert.id}
                                className={`flex flex-col gap-1 rounded-lg border p-3 ${alert.level === 'CRITICAL' ? 'border-danger-200 bg-danger-50 dark:border-danger-500/30 dark:bg-danger-500/10' :
                                    alert.level === 'HIGH' ? 'border-warning-200 bg-warning-50 dark:border-warning-500/30 dark:bg-warning-500/10' :
                                        'border-brand-200 bg-brand-50 dark:border-brand-500/30 dark:bg-brand-500/10'
                                    }`}
                            >
                                <div className="flex items-center justify-between">
                                    <span className={`text-[10px] uppercase font-bold tracking-wide px-1.5 py-0.5 rounded-full ${alert.level === 'CRITICAL' ? 'bg-danger-200 text-danger-900' :
                                        alert.level === 'HIGH' ? 'bg-warning-200 text-warning-900' :
                                            'bg-brand-200 text-brand-900'
                                        }`}>
                                        {alert.level}
                                    </span>
                                    <span className="text-xs text-neutral-500">{alert.time}</span>
                                </div>
                                <p className="text-sm font-medium text-neutral-800 dark:text-neutral-200 mt-1">{alert.message}</p>
                            </div>
                        ))}
                    </div>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle>Recent Compliance Actions</CardTitle>
                        <CardDescription>Audit log excerpt of sensitive operations</CardDescription>
                    </CardHeader>
                    <div className="p-4 pt-0">
                        <div className="relative border-l border-neutral-200 dark:border-neutral-700 ml-3 space-y-6 pb-4">
                            {data.recentActions.map((action) => (
                                <div key={action.id} className="relative pl-6">
                                    <div className="absolute -left-1.5 mt-1.5 size-3 rounded-full border-2 border-white bg-brand-500 dark:border-neutral-900" />
                                    <p className="text-xs text-neutral-500">{new Date(action.time).toLocaleString()}</p>
                                    <p className="text-sm font-medium text-neutral-900 dark:text-neutral-100 mt-0.5">
                                        {action.type.replace(/_/g, ' ')}
                                    </p>
                                    <p className="text-xs text-neutral-600 dark:text-neutral-400 mt-1">
                                        Actor: <span className="font-mono bg-neutral-100 p-0.5 rounded px-1">{action.actor}</span> |
                                        Target: <span className="font-mono bg-neutral-100 p-0.5 rounded px-1">{action.target}</span>
                                    </p>
                                </div>
                            ))}
                        </div>
                    </div>
                </Card>
            </div>
        </div>
    );
}
