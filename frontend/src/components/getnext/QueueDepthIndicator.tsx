import { Clock, AlertTriangle, TrendingDown, Users } from 'lucide-react';
import { useQueueDepth } from '@/hooks/useGetNext';

interface QueueDepthIndicatorProps {
    caseTypeCode?: string;
    /** compact: single badge. expanded: full stats bar. */
    variant?: 'compact' | 'expanded';
    className?: string;
}

/**
 * QueueDepthIndicator — Live queue depth badge with SLA risk highlighting.
 * Polls every 60 seconds via useQueueDepth hook.
 */
export function QueueDepthIndicator({
    caseTypeCode,
    variant = 'compact',
    className = '',
}: QueueDepthIndicatorProps) {
    const { data, isPending } = useQueueDepth(caseTypeCode);

    if (isPending) {
        return (
            <div className={`qdi-skeleton ${className}`} aria-label="Loading queue depth" />
        );
    }

    if (!data) return null;

    const { totalAllocatable, slaBreachedCount, slaAtRiskCount, avgWaitHours } = data;

    // Choose badge colour based on urgency
    let badgeClass = 'qdi-badge-normal';
    if (slaBreachedCount > 0) badgeClass = 'qdi-badge-critical';
    else if (slaAtRiskCount > 0) badgeClass = 'qdi-badge-warning';

    if (variant === 'compact') {
        return (
            <div
                className={`qdi-compact ${badgeClass} ${className}`}
                id="queue-depth-badge"
                title={`${totalAllocatable} cases waiting • ${slaBreachedCount} SLA breached`}
                aria-label={`${totalAllocatable} cases in queue`}
            >
                <Clock size={12} />
                <span>{totalAllocatable}</span>
                {slaBreachedCount > 0 && (
                    <span className="qdi-breach-dot" aria-label={`${slaBreachedCount} SLA breached`} />
                )}

                <style>{`
          .qdi-compact {
            display: inline-flex;
            align-items: center;
            gap: 4px;
            padding: 3px 8px;
            border-radius: 16px;
            font-size: 12px;
            font-weight: 600;
            cursor: default;
            position: relative;
          }
          .qdi-badge-normal  { background: #eff6ff; color: #2563eb; border: 1px solid #bfdbfe; }
          .qdi-badge-warning { background: #fffbeb; color: #d97706; border: 1px solid #fcd34d; }
          .qdi-badge-critical { background: #fef2f2; color: #dc2626; border: 1px solid #fca5a5; animation: qdi-pulse 2s ease-in-out infinite; }
          .qdi-breach-dot {
            width: 6px; height: 6px;
            background: #dc2626;
            border-radius: 50%;
            display: inline-block;
          }
          @keyframes qdi-pulse {
            0%,100% { box-shadow: 0 0 0 0 rgba(220,38,38,0.3); }
            50%      { box-shadow: 0 0 0 6px rgba(220,38,38,0); }
          }
          .qdi-skeleton {
            width: 60px; height: 24px;
            background: linear-gradient(90deg, #f1f5f9 25%, #e2e8f0 50%, #f1f5f9 75%);
            background-size: 200%;
            border-radius: 12px;
            animation: qdi-shimmer 1.5s linear infinite;
          }
          @keyframes qdi-shimmer { to { background-position: -200% 0; } }
        `}</style>
            </div>
        );
    }

    // expanded variant
    return (
        <div className={`qdi-expanded ${className}`} id="queue-depth-expanded">
            <div className="qdi-stat">
                <Users size={14} />
                <span className="qdi-stat-value">{totalAllocatable}</span>
                <span className="qdi-stat-label">In Queue</span>
            </div>
            {slaBreachedCount > 0 && (
                <div className="qdi-stat qdi-stat-critical">
                    <AlertTriangle size={14} />
                    <span className="qdi-stat-value">{slaBreachedCount}</span>
                    <span className="qdi-stat-label">SLA Breached</span>
                </div>
            )}
            {slaAtRiskCount > 0 && (
                <div className="qdi-stat qdi-stat-warning">
                    <Clock size={14} />
                    <span className="qdi-stat-value">{slaAtRiskCount}</span>
                    <span className="qdi-stat-label">At Risk</span>
                </div>
            )}
            <div className="qdi-stat">
                <TrendingDown size={14} />
                <span className="qdi-stat-value">{avgWaitHours.toFixed(1)}h</span>
                <span className="qdi-stat-label">Avg Wait</span>
            </div>

            <style>{`
        .qdi-expanded {
          display: flex;
          gap: 16px;
          padding: 12px 16px;
          background: #f8fafc;
          border: 1px solid #e2e8f0;
          border-radius: 10px;
        }
        .qdi-stat {
          display: flex;
          align-items: center;
          gap: 5px;
          color: #475569;
          font-size: 13px;
        }
        .qdi-stat-value { font-weight: 700; color: #1e293b; }
        .qdi-stat-label { color: #94a3b8; font-size: 11px; }
        .qdi-stat-critical { color: #dc2626; }
        .qdi-stat-critical .qdi-stat-value { color: #dc2626; }
        .qdi-stat-warning { color: #d97706; }
        .qdi-stat-warning .qdi-stat-value { color: #d97706; }
      `}</style>
        </div>
    );
}
