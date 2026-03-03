import React from 'react';
import { Info } from 'lucide-react';
import type { ScoreBreakdown } from '@/types/getnext';

interface ScoreBreakdownTooltipProps {
    breakdown: ScoreBreakdown;
    compositeScore: number;
}

const FACTOR_LABELS: Record<keyof Omit<ScoreBreakdown, 'weightsUsed'>, string> = {
    sla: 'SLA Risk',
    skill: 'Skill Match',
    age: 'Queue Age',
    complexity: 'Complexity',
    value: 'Business Value',
    affinity: 'Affinity',
    workload: 'Workload',
};

const FACTOR_COLORS: Record<keyof Omit<ScoreBreakdown, 'weightsUsed'>, string> = {
    sla: '#ef4444',
    skill: '#3b82f6',
    age: '#f59e0b',
    complexity: '#8b5cf6',
    value: '#10b981',
    affinity: '#ec4899',
    workload: '#6b7280',
};

/**
 * ScoreBreakdownTooltip — Explains the 7-factor composite score.
 * Renders as an inline expandable section. Deploy inside a positioned parent.
 */
export function ScoreBreakdownTooltip({ breakdown, compositeScore }: ScoreBreakdownTooltipProps) {
    const [open, setOpen] = React.useState(false);

    const factors = (
        Object.keys(FACTOR_LABELS) as Array<keyof Omit<ScoreBreakdown, 'weightsUsed'>>
    ).map((key) => ({
        key,
        label: FACTOR_LABELS[key],
        color: FACTOR_COLORS[key],
        factor: breakdown[key],
    }));

    const maxContribution = Math.max(...factors.map((f) => Math.abs(f.factor.weightedScore)));

    return (
        <div className="sbt-wrapper" id="score-breakdown-tooltip">
            <button
                className="sbt-trigger"
                onClick={() => setOpen((o) => !o)}
                aria-expanded={open}
                aria-controls="sbt-panel"
                title="Show score breakdown"
            >
                <span className="sbt-score">
                    {compositeScore.toFixed(1)}
                </span>
                <Info size={14} />
            </button>

            {open && (
                <div className="sbt-panel" id="sbt-panel" role="region" aria-label="Score breakdown">
                    <div className="sbt-header">Score Breakdown</div>
                    <div className="sbt-factors">
                        {factors.map(({ key, label, color, factor }) => {
                            const barPct = maxContribution > 0
                                ? Math.max(0, Math.abs(factor.weightedScore) / maxContribution) * 100
                                : 0;
                            const isNegative = factor.weightedScore < 0;
                            return (
                                <div key={key} className="sbt-factor">
                                    <div className="sbt-factor-header">
                                        <span className="sbt-factor-label" style={{ color }}>
                                            {label}
                                        </span>
                                        <span className={`sbt-factor-score ${isNegative ? 'sbt-negative' : ''}`}>
                                            {factor.weightedScore >= 0 ? '+' : ''}
                                            {factor.weightedScore.toFixed(2)}
                                        </span>
                                    </div>
                                    <div className="sbt-bar-track">
                                        <div
                                            className="sbt-bar-fill"
                                            style={{
                                                width: `${barPct}%`,
                                                background: isNegative ? '#f87171' : color,
                                                opacity: 0.85,
                                            }}
                                        />
                                    </div>
                                    <div className="sbt-explanation">{factor.explanation}</div>
                                </div>
                            );
                        })}
                    </div>
                    <div className="sbt-total">
                        <span>Composite Score</span>
                        <strong>{compositeScore.toFixed(2)}</strong>
                    </div>
                </div>
            )}

            <style>{`
        .sbt-wrapper { position: relative; display: inline-block; }

        .sbt-trigger {
          display: inline-flex;
          align-items: center;
          gap: 4px;
          padding: 4px 8px;
          background: #f1f5f9;
          border: 1px solid #e2e8f0;
          border-radius: 6px;
          font-size: 13px;
          font-weight: 600;
          color: #1e293b;
          cursor: pointer;
          transition: background 0.15s;
        }
        .sbt-trigger:hover { background: #e2e8f0; }

        .sbt-score { font-variant-numeric: tabular-nums; }

        .sbt-panel {
          position: absolute;
          top: calc(100% + 8px);
          left: 0;
          z-index: 200;
          width: 320px;
          background: #fff;
          border: 1px solid #e2e8f0;
          border-radius: 12px;
          box-shadow: 0 8px 32px rgba(0,0,0,0.12);
          padding: 16px;
        }

        .sbt-header {
          font-size: 12px;
          font-weight: 700;
          text-transform: uppercase;
          letter-spacing: 0.08em;
          color: #64748b;
          margin-bottom: 12px;
        }

        .sbt-factors { display: flex; flex-direction: column; gap: 10px; }

        .sbt-factor-header {
          display: flex;
          justify-content: space-between;
          align-items: center;
          margin-bottom: 3px;
        }

        .sbt-factor-label {
          font-size: 12px;
          font-weight: 600;
        }

        .sbt-factor-score {
          font-size: 12px;
          font-weight: 700;
          font-variant-numeric: tabular-nums;
          color: #16a34a;
        }
        .sbt-factor-score.sbt-negative { color: #dc2626; }

        .sbt-bar-track {
          height: 4px;
          background: #f1f5f9;
          border-radius: 2px;
          overflow: hidden;
          margin-bottom: 3px;
        }

        .sbt-bar-fill {
          height: 100%;
          border-radius: 2px;
          transition: width 0.4s ease;
        }

        .sbt-explanation {
          font-size: 11px;
          color: #64748b;
          line-height: 1.4;
        }

        .sbt-total {
          display: flex;
          justify-content: space-between;
          align-items: center;
          margin-top: 14px;
          padding-top: 12px;
          border-top: 1px solid #e2e8f0;
          font-size: 13px;
          color: #64748b;
        }
        .sbt-total strong {
          font-size: 15px;
          color: #1e293b;
          font-variant-numeric: tabular-nums;
        }
      `}</style>
        </div>
    );
}
