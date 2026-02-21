import { ArrowDownRight, ArrowUpRight, Minus } from 'lucide-react';
import { Card } from '@/components/ui/Card';
import { cn } from '@/lib/cn';

interface StatCardProps {
    title: string;
    value: string | number;
    trend?: {
        value: number;
        label: string;
    };
    icon?: React.ReactNode;
    className?: string;
}

export function StatCard({ title, value, trend, icon, className }: StatCardProps) {
    const isPositive = trend ? trend.value > 0 : false;
    const isNegative = trend ? trend.value < 0 : false;
    const isNeutral = trend ? trend.value === 0 : true;

    return (
        <Card className={cn('p-5', className)}>
            <div className="flex items-center justify-between">
                <p className="text-sm font-medium text-neutral-500 dark:text-neutral-400">{title}</p>
                {icon ? <div className="text-neutral-400 dark:text-neutral-500">{icon}</div> : null}
            </div>
            <div className="mt-2 flex items-baseline gap-2">
                <h3 className="text-3xl font-semibold text-neutral-900 dark:text-neutral-50">{value}</h3>
                {trend ? (
                    <div
                        className={cn('flex items-center text-xs font-medium', {
                            'text-success-600 dark:text-success-400': isPositive,
                            'text-danger-600 dark:text-danger-400': isNegative,
                            'text-neutral-500 dark:text-neutral-400': isNeutral
                        })}
                    >
                        {isPositive ? (
                            <ArrowUpRight className="mr-0.5 size-3" />
                        ) : isNegative ? (
                            <ArrowDownRight className="mr-0.5 size-3" />
                        ) : (
                            <Minus className="mr-0.5 size-3" />
                        )}
                        {Math.abs(trend.value)}%
                        <span className="ml-1 text-neutral-500 dark:text-neutral-400">{trend.label}</span>
                    </div>
                ) : null}
            </div>
        </Card>
    );
}
