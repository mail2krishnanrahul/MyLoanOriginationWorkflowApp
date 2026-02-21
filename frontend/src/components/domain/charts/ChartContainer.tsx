import { type PropsWithChildren, type ReactNode } from 'react';
import { Card, CardHeader, CardTitle, CardDescription } from '@/components/ui/Card';
import { cn } from '@/lib/cn';

interface ChartContainerProps extends PropsWithChildren {
    title: string;
    description?: string;
    className?: string;
    action?: ReactNode;
    height?: number | string;
}

export function ChartContainer({
    title,
    description,
    children,
    className,
    action,
    height = 300
}: ChartContainerProps) {
    return (
        <Card className={cn('flex h-full flex-col', className)}>
            <CardHeader className="flex flex-row items-start justify-between pb-2">
                <div>
                    <CardTitle className="text-base">{title}</CardTitle>
                    {description ? <CardDescription>{description}</CardDescription> : null}
                </div>
                {action ? <div>{action}</div> : null}
            </CardHeader>
            <div className="flex-1 px-4 pb-4" style={{ height }}>
                {children}
            </div>
        </Card>
    );
}
