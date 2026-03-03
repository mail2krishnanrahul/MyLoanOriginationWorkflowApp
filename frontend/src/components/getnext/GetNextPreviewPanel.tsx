import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ChevronDown, ChevronUp, Zap, SkipForward, Eye } from 'lucide-react';
import { useGetNextPreview, useClaimNextCase } from '@/hooks/useGetNext';
import { ScoreBreakdownTooltip } from './ScoreBreakdownTooltip';
import { SkipReasonModal } from './SkipReasonModal';
import type { GetNextResult } from '@/types/getnext';

interface GetNextPreviewPanelProps {
    caseTypeCode?: string;
    className?: string;
}

/**
 * GetNextPreviewPanel — Expandable panel showing top-3 scored cases.
 * Lets operators see what's coming before they commit.
 */
export function GetNextPreviewPanel({ caseTypeCode, className = '' }: GetNextPreviewPanelProps) {
    const [open, setOpen] = useState(false);
    const [skipTarget, setSkipTarget] = useState<GetNextResult | null>(null);
    const navigate = useNavigate();

    const { data, isPending, refetch } = useGetNextPreview(caseTypeCode, 3, open);
    const { mutate: claim, isPending: isClaiming } = useClaimNextCase();

    function handleClaim(caseId: string) {
        // Use the specific case id by navigating directly after claim
        claim(caseTypeCode, {
            onSuccess: (result) => {
                if (result.case.id === caseId) {
                    navigate(`/cases/${result.case.id}`);
                }
            },
        });
    }

    const slaColour = (caseDueAt: string | null): string => {
        if (!caseDueAt) return '#64748b';
        const h = (new Date(caseDueAt).getTime() - Date.now()) / 3_600_000;
        if (h < 0) return '#dc2626';
        if (h < 2) return '#ea580c';
        if (h < 8) return '#d97706';
        return '#16a34a';
    };

    return (
        <div className={`gnpp-wrapper ${className}`} id="getnext-preview-panel">
            <button
                className="gnpp-toggle"
                onClick={() => setOpen((o) => !o)}
                aria-expanded={open}
                id="getnext-preview-toggle"
            >
                <Eye size={15} />
                <span>Preview Queue</span>
                {data && (
                    <span className="gnpp-depth-badge">{data.queueDepth}</span>
                )}
                {open ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
            </button>

            {open && (
                <div className="gnpp-panel" role="region" aria-label="Queue preview">
                    {isPending && (
                        <div className="gnpp-loading">
                            {[0, 1, 2].map((i) => (
                                <div key={i} className="gnpp-skeleton" />
                            ))}
                        </div>
                    )}

                    {!isPending && (!data?.topCases || data.topCases.length === 0) && (
                        <div className="gnpp-empty">
                            No eligible cases found in queue right now.
                        </div>
                    )}

                    {!isPending && data?.topCases?.map((result, idx) => (
                        <div
                            key={result.case.id}
                            className={`gnpp-case gnpp-case-${idx === 0 ? 'top' : 'normal'}`}
                            id={`preview-case-${idx}`}
                        >
                            <div className="gnpp-case-rank">#{idx + 1}</div>
                            <div className="gnpp-case-body">
                                <div className="gnpp-case-ref">{result.case.referenceNumber}</div>
                                <div className="gnpp-case-meta">
                                    <span className="gnpp-stage">{result.case.currentStageCode}</span>
                                    {result.case.complexity && (
                                        <span className="gnpp-complexity">{result.case.complexity}</span>
                                    )}
                                    {result.case.caseDueAt && (
                                        <span
                                            className="gnpp-sla"
                                            style={{ color: slaColour(result.case.caseDueAt) }}
                                        >
                                            SLA: {new Date(result.case.caseDueAt).toLocaleDateString()}
                                        </span>
                                    )}
                                </div>
                                {result.case.requiredSkills?.length > 0 && (
                                    <div className="gnpp-skills">
                                        {result.case.requiredSkills.slice(0, 3).map((s) => (
                                            <span key={s} className="gnpp-skill-tag">{s}</span>
                                        ))}
                                    </div>
                                )}
                            </div>
                            <div className="gnpp-case-actions">
                                <ScoreBreakdownTooltip
                                    breakdown={result.scoreBreakdown}
                                    compositeScore={result.compositeScore}
                                />
                                <button
                                    className="gnpp-skip-btn"
                                    onClick={() => setSkipTarget(result)}
                                    title="Skip this case"
                                    id={`skip-btn-${idx}`}
                                >
                                    <SkipForward size={14} />
                                </button>
                                {idx === 0 && (
                                    <button
                                        className="gnpp-claim-btn"
                                        onClick={() => handleClaim(result.case.id)}
                                        disabled={isClaiming}
                                        id="preview-claim-btn"
                                    >
                                        <Zap size={14} />
                                        Claim
                                    </button>
                                )}
                            </div>
                        </div>
                    ))}

                    {!isPending && data && (
                        <button className="gnpp-refresh" onClick={() => refetch()}>
                            Refresh preview
                        </button>
                    )}
                </div>
            )}

            {skipTarget && (
                <SkipReasonModal
                    caseId={skipTarget.case.id}
                    caseRef={skipTarget.case.referenceNumber}
                    onClose={() => setSkipTarget(null)}
                    onSkipped={() => {
                        setSkipTarget(null);
                        refetch();
                    }}
                />
            )}

            <style>{`
        .gnpp-wrapper { display: flex; flex-direction: column; }

        .gnpp-toggle {
          display: inline-flex; align-items: center; gap: 6px;
          padding: 7px 14px;
          background: #f8fafc;
          border: 1.5px solid #e2e8f0;
          border-radius: 10px;
          font-size: 13px; font-weight: 600; color: #475569;
          cursor: pointer; transition: all 0.15s; align-self: flex-start;
        }
        .gnpp-toggle:hover { background: #f1f5f9; border-color: #cbd5e1; color: #1e293b; }

        .gnpp-depth-badge {
          background: #dbeafe; color: #2563eb;
          padding: 1px 6px; border-radius: 10px;
          font-size: 11px; font-weight: 700;
        }

        .gnpp-panel {
          margin-top: 8px;
          border: 1.5px solid #e2e8f0;
          border-radius: 12px;
          overflow: hidden;
          background: #fff;
          box-shadow: 0 4px 16px rgba(0,0,0,0.06);
        }

        .gnpp-loading { padding: 12px; display: flex; flex-direction: column; gap: 8px; }
        .gnpp-skeleton {
          height: 64px; border-radius: 8px;
          background: linear-gradient(90deg, #f1f5f9 25%, #e2e8f0 50%, #f1f5f9 75%);
          background-size: 200%;
          animation: gnpp-shimmer 1.5s linear infinite;
        }
        @keyframes gnpp-shimmer { to { background-position: -200% 0; } }

        .gnpp-empty {
          padding: 24px;
          text-align: center;
          font-size: 13px;
          color: #94a3b8;
        }

        .gnpp-case {
          display: flex; align-items: flex-start; gap: 12px;
          padding: 14px 16px;
          border-bottom: 1px solid #f1f5f9;
          transition: background 0.1s;
        }
        .gnpp-case:last-of-type { border-bottom: none; }
        .gnpp-case:hover { background: #f8fafc; }
        .gnpp-case-top { background: #fafbff; border-left: 3px solid #6366f1; }

        .gnpp-case-rank {
          font-size: 12px; font-weight: 800; color: #a5b4fc;
          padding-top: 2px; flex-shrink: 0; width: 20px;
        }
        .gnpp-case-top .gnpp-case-rank { color: #6366f1; }

        .gnpp-case-body { flex: 1; min-width: 0; }
        .gnpp-case-ref { font-size: 14px; font-weight: 700; color: #1e293b; }
        .gnpp-case-meta {
          display: flex; gap: 8px; flex-wrap: wrap;
          margin-top: 4px;
        }
        .gnpp-stage, .gnpp-complexity, .gnpp-sla {
          font-size: 11px; font-weight: 500;
          padding: 1px 6px; border-radius: 4px;
        }
        .gnpp-stage { background: #eff6ff; color: #3b82f6; }
        .gnpp-complexity { background: #f5f3ff; color: #7c3aed; }
        .gnpp-sla { background: transparent; }

        .gnpp-skills { display: flex; gap: 4px; flex-wrap: wrap; margin-top: 5px; }
        .gnpp-skill-tag {
          font-size: 10px; font-weight: 600;
          padding: 1px 6px; border-radius: 4px;
          background: #f0fdf4; color: #16a34a; border: 1px solid #bbf7d0;
        }

        .gnpp-case-actions {
          display: flex; align-items: center; gap: 6px; flex-shrink: 0;
        }

        .gnpp-skip-btn {
          display: flex; align-items: center; justify-content: center;
          width: 28px; height: 28px;
          background: #f8fafc; border: 1px solid #e2e8f0;
          border-radius: 6px; cursor: pointer;
          color: #94a3b8; transition: all 0.15s;
        }
        .gnpp-skip-btn:hover { background: #fef2f2; color: #dc2626; border-color: #fecaca; }

        .gnpp-claim-btn {
          display: inline-flex; align-items: center; gap: 4px;
          padding: 5px 12px;
          background: linear-gradient(135deg, #6366f1 0%, #4f46e5 100%);
          border: none; border-radius: 7px;
          font-size: 12px; font-weight: 600; color: #fff;
          cursor: pointer; transition: all 0.15s;
        }
        .gnpp-claim-btn:hover:not(:disabled) { box-shadow: 0 3px 10px rgba(99,102,241,0.35); }
        .gnpp-claim-btn:disabled { opacity: 0.6; cursor: not-allowed; }

        .gnpp-refresh {
          display: block; width: 100%;
          padding: 10px;
          background: none; border: none; border-top: 1px solid #f1f5f9;
          font-size: 12px; color: #94a3b8; cursor: pointer;
          transition: color 0.15s;
        }
        .gnpp-refresh:hover { color: #6366f1; }
      `}</style>
        </div>
    );
}
