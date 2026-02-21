import { Cell, Pie, PieChart, ResponsiveContainer } from 'recharts';
import { Card } from '@/components/ui/Card';
import { cn } from '@/lib/cn';

interface GaugeChartProps {
    title: string;
    percentage: number;
    color?: string;
    className?: string;
}

export function GaugeChart({ title, percentage, color = '#3b82f6', className }: GaugeChartProps) {
    const data = [
        { name: 'Value', value: percentage },
        { name: 'Remaining', value: 100 - percentage }
    ];

    return (
        <Card className={cn('p-5 flex flex-col items-center justify-center', className)}>
            <h3 className="text-sm font-medium text-neutral-500 dark:text-neutral-400 mb-2">{title}</h3>
            <div className="relative h-32 w-full max-w-[200px]">
                <ResponsiveContainer width="100%" height="100%">
                    <PieChart>
                        <Pie
                            data={data}
                            cx="50%"
                            cy="100%"
                            startAngle={180}
                            endAngle={0}
                            innerRadius="75%"
                            outerRadius="100%"
                            paddingAngle={0}
                            dataKey="value"
                            stroke="none"
                            cornerRadius={4}
                        >
                            <Cell fill={color} />
                            <Cell fill="rgba(163, 163, 163, 0.2)" />
                        </Pie>
                    </PieChart>
                </ResponsiveContainer>
                <div className="absolute bottom-0 left-0 flex w-full flex-col items-center pb-2">
                    <span className="text-3xl font-bold font-mono text-neutral-900 dark:text-neutral-50">
                        {Math.round(percentage)}%
                    </span>
                </div>
            </div>
        </Card>
    );
}
