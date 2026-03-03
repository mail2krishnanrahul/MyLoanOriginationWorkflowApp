import { AlertTriangle, Clock, Users, TrendingUp } from 'lucide-react';
import { useSupervisorView } from '@/hooks/useGetNext';
import type { CaseSummary, TeamWorkloadRow } from '@/types/getnext';

/**
 * SupervisorQueuePanel — Full supervisor dashboard.
 * Shows queue depth, team workloads, stalled cases, and at-capacity operators.
 */
export function SupervisorQueuePanel() {
    const { data, isPending, isError, refetch } = useSupervisorView();

    if (isPending) {
        return (
            <div className="sqp-loading" aria-label="Loading supervisor dashboard">
                {[0, 1, 2].map((i) => (
                    <div key={i} className="sqp-skeleton-row" />
                ))}
            </div>
        );
    }

    if (isError || !data) {
        return (
            <div className="sqp-error" role="alert">
                <AlertTriangle size={18} />
                <span>Failed to load supervisor dashboard.</span>
                <button onClick={() => refetch()}>Retry</button>
            </div>
        );
    }

    const { queueDepth, teamWorkloads, stalledCases, topCases } = data;

    return (
        <div className="sqp-wrapper" id="supervisor-queue-panel">
            {/* ── Summary Stats ───────────────────────────────────────────── */}
            <div className="sqp-stats-row">
                <div className="sqp-stat">
                    <Clock size={16} className="sqp-stat-icon" />
                    <span className="sqp-stat-val">{queueDepth.totalAllocatable}</span>
                    <span className="sqp-stat-label">In Queue</span>
                </div>
                <div className="sqp-stat sqp-critical">
                    <AlertTriangle size={16} className="sqp-stat-icon" />
                    <span className="sqp-stat-val">{queueDepth.slaBreachedCount}</span>
                    <span className="sqp-stat-label">SLA Breached</span>
                </div>
                <div className="sqp-stat sqp-warning">
                    <Clock size={16} className="sqp-stat-icon" />
                    <span className="sqp-stat-val">{queueDepth.slaAtRiskCount}</span>
                    <span className="sqp-stat-label">At Risk</span>
                </div>
                <div className="sqp-stat">
                    <TrendingUp size={16} className="sqp-stat-icon" />
                    <span className="sqp-stat-val">{queueDepth.avgWaitHours.toFixed(1)}h</span>
                    <span className="sqp-stat-label">Avg Wait</span>
                </div>
            </div>

            <div className="sqp-grid">
                {/* ── Team Workloads ────────────────────────────────────────── */}
                <section className="sqp-section" id="supervisor-team-workloads">
                    <div className="sqp-section-header">
                        <Users size={14} />
                        <h3>Team Workloads</h3>
                    </div>
                    {teamWorkloads.length === 0 && (
                        <div className="sqp-empty">No team data available.</div>
                    )}
                    {teamWorkloads.map((team: TeamWorkloadRow) => (
                        <TeamWorkloadCard key={team.teamId} team={team} />
                    ))}
                </section>

                {/* ── Stalled Cases ─────────────────────────────────────────── */}
                <section className="sqp-section" id="supervisor-stalled-cases">
                    <div className="sqp-section-header">
                        <AlertTriangle size={14} />
                        <h3>Stalled Cases (&gt;24h in queue)</h3>
                    </div>
                    {stalledCases.length === 0 && (
                        <div className="sqp-empty">No stalled cases — great work! 🎉</div>
                    )}
                    {stalledCases.slice(0, 10).map((c: CaseSummary) => (
                        <div key={c.id} className="sqp-stalled-row">
                            <span className="sqp-stalled-ref">{c.referenceNumber}</span>
                            <span className="sqp-stalled-stage">{c.currentStageCode}</span>
                            {c.caseDueAt && (
                                <span className="sqp-stalled-sla">
                                    Due: {new Date(c.caseDueAt).toLocaleDateString()}
                                </span>
                            )}
                        </div>
                    ))}
                </section>

                {/* ── Top Scored Cases ──────────────────────────────────────── */}
                <section className="sqp-section" id="supervisor-top-cases">
                    <div className="sqp-section-header">
                        <TrendingUp size={14} />
                        <h3>Highest Priority Cases</h3>
                    </div>
                    {topCases.slice(0, 5).map((result, idx) => (
                        <div key={result.case.id} className="sqp-top-case">
                            <span className="sqp-rank">#{idx + 1}</span>
                            <span className="sqp-top-ref">{result.case.referenceNumber}</span>
                            <span className="sqp-top-score">
                                {result.compositeScore.toFixed(1)}
                            </span>
                        </div>
                    ))}
                </section>
            </div>

            <style>{`
        .sqp-wrapper { display: flex; flex-direction: column; gap: 20px; }

        .sqp-stats-row {
          display: flex; gap: 12px; flex-wrap: wrap;
        }

        .sqp-stat {
          display: flex; flex-direction: column; align-items: center;
          padding: 14px 20px;
          background: #f8fafc; border: 1px solid #e2e8f0;
          border-radius: 12px; min-width: 100px; gap: 4px;
          color: #475569;
        }
        .sqp-stat-icon { color: #94a3b8; }
        .sqp-stat-val { font-size: 24px; font-weight: 800; color: #1e293b; font-variant-numeric: tabular-nums; }
        .sqp-stat-label { font-size: 11px; font-weight: 500; text-align: center; }
        .sqp-critical .sqp-stat-val { color: #dc2626; }
        .sqp-critical .sqp-stat-icon { color: #dc2626; }
        .sqp-warning .sqp-stat-val { color: #d97706; }
        .sqp-warning .sqp-stat-icon { color: #d97706; }

        .sqp-grid {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
          gap: 16px;
        }

        .sqp-section {
          background: #fff; border: 1px solid #e2e8f0;
          border-radius: 12px; padding: 16px; display: flex; flex-direction: column; gap: 10px;
        }

        .sqp-section-header {
          display: flex; align-items: center; gap: 6px;
          color: #64748b;
        }
        .sqp-section-header h3 {
          font-size: 13px; font-weight: 700;
          text-transform: uppercase; letter-spacing: 0.06em;
          margin: 0; color: #475569;
        }

        .sqp-empty { font-size: 13px; color: #94a3b8; padding: 8px 0; }

        /* Team workload card */
        .sqp-team-card { display: flex; flex-direction: column; gap: 5px; }
        .sqp-team-name { font-size: 13px; font-weight: 600; color: #1e293b; }
        .sqp-team-meta {
          display: flex; justify-content: space-between;
          font-size: 11px; color: #94a3b8;
        }
        .sqp-team-bar-track {
          height: 6px; background: #f1f5f9; border-radius: 3px; overflow: hidden;
        }
        .sqp-team-bar-fill { height: 100%; border-radius: 3px; transition: width 0.4s ease; }

        /* Stalled cases */
        .sqp-stalled-row {
          display: flex; align-items: center; gap: 8px;
          padding: 7px 10px;
          background: #fef9f0; border: 1px solid #fef3c7;
          border-radius: 8px; font-size: 12px;
        }
        .sqp-stalled-ref { font-weight: 700; color: #1e293b; flex: 1; }
        .sqp-stalled-stage { color: #64748b; }
        .sqp-stalled-sla { color: #d97706; font-weight: 600; }

        /* Top cases */
        .sqp-top-case {
          display: flex; align-items: center; gap: 8px;
          padding: 6px 10px;
          border-radius: 8px; font-size: 12px;
          background: #f8fafc;
        }
        .sqp-rank { font-weight: 800; color: #a5b4fc; width: 24px; }
        .sqp-top-ref { flex: 1; font-weight: 600; color: #1e293b; }
        .sqp-top-score { font-weight: 800; color: #6366f1; font-variant-numeric: tabular-nums; }

        /* Loading & error */
        .sqp-loading { display: flex; flex-direction: column; gap: 12px; }
        .sqp-skeleton-row {
          height: 80px; border-radius: 10px;
          background: linear-gradient(90deg, #f1f5f9 25%, #e2e8f0 50%, #f1f5f9 75%);
          background-size: 200%;
          animation: sqp-shimmer 1.5s linear infinite;
        }
        @keyframes sqp-shimmer { to { background-position: -200% 0; } }
        .sqp-error {
          display: flex; align-items: center; gap: 8px;
          padding: 14px 16px;
          background: #fef2f2; border: 1px solid #fecaca;
          border-radius: 10px; font-size: 13px; color: #dc2626;
        }
        .sqp-error button {
          margin-left: 8px; padding: 4px 10px;
          background: #fff; border: 1px solid #fca5a5;
          border-radius: 6px; font-size: 12px; color: #dc2626;
          cursor: pointer;
        }
      `}</style>
        </div>
    );
}

function TeamWorkloadCard({ team }: { team: TeamWorkloadRow }) {
    const pct = Math.min(Math.round(team.capacityPct * 100), 100);
    const colour =
        pct >= 90 ? '#dc2626' :
            pct >= 75 ? '#d97706' : '#6366f1';

    return (
        <div className="sqp-team-card">
            <div className="sqp-team-name">{team.teamName}</div>
            <div className="sqp-team-meta">
                <span>{team.activeCases}/{team.maxCases} cases</span>
                <span>{pct}% capacity</span>
            </div>
            <div className="sqp-team-bar-track">
                <div
                    className="sqp-team-bar-fill"
                    style={{ width: `${pct}%`, background: colour }}
                />
            </div>
        </div>
    );
}
