import {
    Bar,
    BarChart,
    ResponsiveContainer,
    Tooltip,
    XAxis,
    YAxis
} from 'recharts';
import { ChartContainer } from './ChartContainer';

interface StackedBarChartProps {
    title: string;
    description?: string;
    data: any[];
    xAxisKey: string;
    bars: Array<{ key: string; color: string; name?: string }>;
}

export function StackedBarChart({ title, description, data, xAxisKey, bars }: StackedBarChartProps) {
    return (
        <ChartContainer title={title} description={description}>
            <ResponsiveContainer width="100%" height="100%">
                <BarChart data={data} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                    <XAxis
                        dataKey={xAxisKey}
                        stroke="#888888"
                        fontSize={12}
                        tickLine={false}
                        axisLine={false}
                    />
                    <YAxis
                        stroke="#888888"
                        fontSize={12}
                        tickLine={false}
                        axisLine={false}
                        tickFormatter={(value) => `${value}`}
                    />
                    <Tooltip
                        contentStyle={{ borderRadius: '8px', border: 'none', boxShadow: '0 4px 6px -1px rgb(0 0 0 / 0.1)' }}
                        cursor={{ fill: 'transparent' }}
                    />
                    {bars.map((bar) => (
                        <Bar
                            key={bar.key}
                            dataKey={bar.key}
                            name={bar.name ?? bar.key}
                            stackId="a"
                            fill={bar.color}
                            radius={[0, 0, 0, 0]}
                        />
                    ))}
                </BarChart>
            </ResponsiveContainer>
        </ChartContainer>
    );
}
