import { useCaseSummaryStats } from '@/hooks/useCaseSummaryStats';
import { useCaseListStore } from '@/stores/caseListStore';
import { cn } from '@/lib/cn';
import { Layers, UserCheck, CheckCircle2, AlertOctagon } from 'lucide-react';

export function CaseSummaryStats() {
    const { data, isLoading, isError } = useCaseSummaryStats();
    const store = useCaseListStore();

    const handleTotalClick = () => {
        store.resetFilters();
    };

    const handleMyActiveClick = () => {
        store.resetFilters();
        store.setAdvancedFilter('assignedToMe', true);
    };

    const handleResolvedClick = () => {
        store.resetFilters();
        store.setStatusFilter(['COMPLETED']);
    };

    const handleAtRiskClick = () => {
        store.resetFilters();
        store.setAdvancedFilter('slaDueBefore', new Date(Date.now() + 4 * 60 * 60 * 1000).toISOString());
    };

    const stats = [
        {
            label: 'Total Cases',
            value: data?.totalCases ?? 0,
            icon: Layers,
            color: 'text-blue-600',
            bgColor: 'bg-blue-100',
            borderColor: 'border-blue-200 hover:border-blue-300',
            onClick: handleTotalClick
        },
        {
            label: 'My Active Cases',
            value: data?.myActiveCases ?? 0,
            icon: UserCheck,
            color: 'text-indigo-600',
            bgColor: 'bg-indigo-100',
            borderColor: 'border-indigo-200 hover:border-indigo-300',
            onClick: handleMyActiveClick
        },
        {
            label: 'Resolved',
            value: data?.resolvedCases ?? 0,
            icon: CheckCircle2,
            color: 'text-emerald-600',
            bgColor: 'bg-emerald-100',
            borderColor: 'border-emerald-200 hover:border-emerald-300',
            onClick: handleResolvedClick
        },
        {
            label: 'At Risk',
            value: data?.atRiskCases ?? 0,
            icon: AlertOctagon,
            color: 'text-rose-600',
            bgColor: 'bg-rose-100',
            borderColor: 'border-rose-200 hover:border-rose-300',
            onClick: handleAtRiskClick
        }
    ];

    return (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
            {stats.map((stat) => (
                <button
                    key={stat.label}
                    onClick={stat.onClick}
                    className={cn(
                        "flex items-center p-4 bg-white rounded-xl border shadow-sm transition-all focus:outline-none focus:ring-2 focus:ring-offset-1 focus:ring-blue-500 text-left",
                        stat.borderColor,
                        isLoading ? "animate-pulse" : ""
                    )}
                    disabled={isLoading || isError}
                >
                    <div className={cn("p-3 rounded-full mr-4", stat.bgColor, stat.color)}>
                        <stat.icon size={20} aria-hidden="true" />
                    </div>
                    <div>
                        <p className="text-sm font-medium text-gray-500">{stat.label}</p>
                        <h3 className="text-2xl font-bold text-gray-900 mt-1">
                            {isLoading ? "-" : isError ? "!" : stat.value}
                        </h3>
                    </div>
                </button>
            ))}
        </div>
    );
}
