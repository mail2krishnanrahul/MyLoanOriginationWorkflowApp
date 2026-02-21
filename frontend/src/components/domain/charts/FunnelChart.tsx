import { Cell, Funnel, FunnelChart as RechartsFunnelChart, ResponsiveContainer, Tooltip } from 'recharts';
import { ChartContainer } from './ChartContainer';

interface FunnelChartProps {
    title: string;
    description?: string;
    data: Array<{ stage: string; count: number }>;
    colors: string[];
}

export function FunnelChart({ title, description, data, colors }: FunnelChartProps) {
    return (
        <ChartContainer title={title} description={description}>
            <ResponsiveContainer width="100%" height="100%">
                <RechartsFunnelChart>
                    <Tooltip
                        contentStyle={{ borderRadius: '8px', border: 'none', boxShadow: '0 4px 6px -1px rgb(0 0 0 / 0.1)' }}
                    />
                    <Funnel
                        dataKey="count"
                        data={data}
                        isAnimationActive
                    >
                        {data.map((_, index) => (
                            <Cell key={`cell-${index}`} fill={colors[index % colors.length]} />
                        ))}
                    </Funnel>
                </RechartsFunnelChart>
            </ResponsiveContainer>
        </ChartContainer>
    );
}
