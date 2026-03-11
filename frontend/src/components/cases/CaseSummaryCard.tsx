import { useState, useEffect } from 'react';
import { Pencil, X, Check, Star, Calendar, DollarSign, User, Briefcase, Radio, Loader2 } from 'lucide-react';
import { usePatchCaseSummary } from '@/hooks/use-case-detail';
import type { CaseDetail, CaseComplexity, UpdateCaseSummaryPayload } from '@/lib/api/types';
import {
    COMPLEXITY_LABELS,
    COMPLEXITY_SLA_DAYS,
} from '@/lib/api/types';
import { formatCurrency } from '@/lib/utils/format';
import { formatDate } from '@/lib/utils/date';

// ---------------------------------------------------------------------------
// Complexity tiers ordered for display
// ---------------------------------------------------------------------------
const COMPLEXITY_ORDER: CaseComplexity[] = [
    'SIMPLE',
    'STANDARD_1',
    'STANDARD_2',
    'COMPLEX',
    'NON_STANDARD',
];

const COMPLEXITY_COLOR: Record<CaseComplexity, string> = {
    SIMPLE: 'cs-chip--simple',
    STANDARD_1: 'cs-chip--std1',
    STANDARD_2: 'cs-chip--std2',
    COMPLEX: 'cs-chip--complex',
    NON_STANDARD: 'cs-chip--nonstandard',
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function addDays(days: number): string {
    const d = new Date();
    d.setDate(d.getDate() + days);
    return d.toISOString().split('T')[0];
}

function todayISO(): string {
    return new Date().toISOString().split('T')[0];
}

// ---------------------------------------------------------------------------
// CaseSummaryCard
// ---------------------------------------------------------------------------
interface CaseSummaryCardProps {
    caseDetail: CaseDetail;
    caseId: string;
}

export function CaseSummaryCard({ caseDetail, caseId }: CaseSummaryCardProps) {
    const patch = usePatchCaseSummary(caseId);
    const [editing, setEditing] = useState(false);

    // Local draft state
    const [draft, setDraft] = useState<UpdateCaseSummaryPayload>({});

    // Initialise draft from caseDetail on open/change
    useEffect(() => {
        if (editing) {
            setDraft({
                complexity: caseDetail.complexity ?? undefined,
                isVip: caseDetail.isVip ?? false,
                targetCloseDate: caseDetail.targetCloseDate ?? undefined,
                loanAmount: caseDetail.loanAmount ?? undefined,
                productType: caseDetail.productType ?? undefined,
                channel: caseDetail.channel ?? undefined,
                officer: caseDetail.officer ?? undefined,
                borrowerName: caseDetail.borrowerName ?? undefined,
            });
        }
    }, [editing, caseDetail]);

    // When user picks a complexity, auto-fill target date unless they've already set one
    function handleComplexityChange(c: CaseComplexity) {
        setDraft((prev) => ({
            ...prev,
            complexity: c,
            // Only auto-compute when the date field hasn't been manually changed
            targetCloseDate:
                prev.targetCloseDate && prev.complexity !== c
                    ? addDays(COMPLEXITY_SLA_DAYS[c])
                    : prev.targetCloseDate ?? addDays(COMPLEXITY_SLA_DAYS[c]),
        }));
    }

    async function handleSave() {
        await patch.mutateAsync(draft);
        setEditing(false);
    }

    function handleCancel() {
        setEditing(false);
        setDraft({});
    }

    return (
        <div className="cs-wrap">
            {/* ── Header row ─────────────────────────────────────────────────────── */}
            <div className="cs-header">
                <div>
                    <p className="cs-title">Case summary</p>
                    <p className="cs-subtitle">Core metadata and projected close path</p>
                </div>
                <div className="cs-header-actions">
                    {caseDetail.isVip && !editing && (
                        <span className="cs-vip-badge" title="VIP case">
                            <Star size={12} /> VIP
                        </span>
                    )}
                    {!editing ? (
                        <button
                            className="cs-edit-btn"
                            onClick={() => setEditing(true)}
                            id="case-summary-edit-btn"
                            title="Edit case summary"
                        >
                            <Pencil size={14} /> Edit
                        </button>
                    ) : (
                        <div className="cs-edit-actions">
                            <button
                                className="cs-cancel-btn"
                                onClick={handleCancel}
                                disabled={patch.isPending}
                                id="case-summary-cancel-btn"
                            >
                                <X size={14} /> Cancel
                            </button>
                            <button
                                className="cs-save-btn"
                                onClick={handleSave}
                                disabled={patch.isPending}
                                id="case-summary-save-btn"
                            >
                                {patch.isPending
                                    ? <><Loader2 size={14} className="cs-spin" /> Saving…</>
                                    : <><Check size={14} /> Save</>}
                            </button>
                        </div>
                    )}
                </div>
            </div>

            {/* ── Complexity picker ─────────────────────────────────────────────── */}
            <div className="cs-complexity-row">
                <p className="cs-label"><Radio size={13} /> Complexity</p>
                {editing ? (
                    <div className="cs-complexity-picker" role="group" aria-label="Select complexity">
                        {COMPLEXITY_ORDER.map((c) => (
                            <button
                                key={c}
                                className={`cs-complexity-btn ${draft.complexity === c ? 'cs-complexity-btn--active' : ''}`}
                                onClick={() => handleComplexityChange(c)}
                                type="button"
                                id={`complexity-${c}`}
                            >
                                {COMPLEXITY_LABELS[c]}
                                <span className="cs-complexity-sla">{COMPLEXITY_SLA_DAYS[c]}d</span>
                            </button>
                        ))}
                    </div>
                ) : (
                    <div className="cs-complexity-display">
                        {caseDetail.complexity ? (
                            <span className={`cs-chip ${COMPLEXITY_COLOR[caseDetail.complexity]}`}>
                                {COMPLEXITY_LABELS[caseDetail.complexity]}
                                <span className="cs-chip-sla">
                                    {COMPLEXITY_SLA_DAYS[caseDetail.complexity]}d SLA
                                </span>
                            </span>
                        ) : (
                            <span className="cs-na">Not classified</span>
                        )}
                    </div>
                )}
            </div>

            {/* ── VIP flag ─────────────────────────────────────────────────────── */}
            <div className="cs-field-row">
                <label className="cs-label" htmlFor="vip-toggle">
                    <Star size={13} /> VIP Flag
                </label>
                {editing ? (
                    <button
                        id="vip-toggle"
                        role="switch"
                        aria-checked={draft.isVip ?? false}
                        className={`cs-toggle ${draft.isVip ? 'cs-toggle--on' : ''}`}
                        onClick={() => setDraft((p) => ({ ...p, isVip: !p.isVip }))}
                        type="button"
                    >
                        <span className="cs-toggle-thumb" />
                        <span className="cs-toggle-label">{draft.isVip ? 'VIP' : 'Standard'}</span>
                    </button>
                ) : (
                    <span className={`cs-field-val ${caseDetail.isVip ? 'cs-vip-on' : ''}`}>
                        {caseDetail.isVip ? '⭐ VIP' : '—'}
                    </span>
                )}
            </div>

            {/* ── Two-column grid: main fields ─────────────────────────────────── */}
            <div className="cs-grid">
                <CSSummaryField
                    icon={<DollarSign size={13} />}
                    label="Loan Amount"
                    editing={editing}
                    value={formatCurrency(caseDetail.loanAmount)}
                    inputType="number"
                    inputValue={draft.loanAmount?.toString() ?? ''}
                    onChange={(v) => setDraft((p) => ({ ...p, loanAmount: v ? Number(v) : undefined }))}
                    id="field-loan-amount"
                />
                <CSSummaryField
                    icon={<Briefcase size={13} />}
                    label="Product"
                    editing={editing}
                    value={caseDetail.productType ?? '—'}
                    inputValue={draft.productType ?? ''}
                    onChange={(v) => setDraft((p) => ({ ...p, productType: v }))}
                    id="field-product"
                />
                <CSSummaryField
                    icon={<Radio size={13} />}
                    label="Channel"
                    editing={editing}
                    value={caseDetail.channel ?? '—'}
                    inputValue={draft.channel ?? ''}
                    onChange={(v) => setDraft((p) => ({ ...p, channel: v }))}
                    id="field-channel"
                />
                <CSSummaryField
                    icon={<User size={13} />}
                    label="Officer"
                    editing={editing}
                    value={caseDetail.officer ?? '—'}
                    inputValue={draft.officer ?? ''}
                    onChange={(v) => setDraft((p) => ({ ...p, officer: v }))}
                    id="field-officer"
                />
                <CSSummaryField
                    icon={<User size={13} />}
                    label="Borrower"
                    editing={editing}
                    value={caseDetail.borrowerName ?? '—'}
                    inputValue={draft.borrowerName ?? ''}
                    onChange={(v) => setDraft((p) => ({ ...p, borrowerName: v }))}
                    id="field-borrower"
                />
                <div className="cs-field">
                    <p className="cs-label"><Calendar size={13} /> Target Close</p>
                    {editing ? (
                        <input
                            id="field-target-date"
                            type="date"
                            className="cs-input"
                            min={todayISO()}
                            value={draft.targetCloseDate ?? ''}
                            onChange={(e) => setDraft((p) => ({ ...p, targetCloseDate: e.target.value || undefined }))}
                        />
                    ) : (
                        <span className="cs-field-val cs-field-val--accent">
                            {caseDetail.targetCloseDate ? formatDate(caseDetail.targetCloseDate) : '—'}
                        </span>
                    )}
                </div>
            </div>

            {/* ── Mini timeline ────────────────────────────────────────────────── */}
            <div className="cs-timeline">
                <p className="cs-label">Mini timeline</p>
                <div className="cs-timeline-track">
                    <span className="cs-timeline-pill cs-timeline-pill--neutral">
                        Created {formatDate(caseDetail.createdAt)}
                    </span>
                    <span className="cs-timeline-arrow">→</span>
                    <span className="cs-timeline-pill cs-timeline-pill--brand">
                        Current: {caseDetail.currentStage ?? caseDetail.stage ?? '—'}
                    </span>
                    <span className="cs-timeline-arrow">→</span>
                    <span className="cs-timeline-pill cs-timeline-pill--accent">
                        Target close {caseDetail.targetCloseDate ? formatDate(caseDetail.targetCloseDate) : '—'}
                    </span>
                </div>
            </div>

            <Style />
        </div>
    );
}

// ---------------------------------------------------------------------------
// Generic field row (view + edit)
// ---------------------------------------------------------------------------
interface CSFieldProps {
    icon: React.ReactNode;
    label: string;
    editing: boolean;
    value: string;
    inputValue: string;
    onChange: (v: string) => void;
    id: string;
    inputType?: string;
}

function CSSummaryField({ icon, label, editing, value, inputValue, onChange, id, inputType = 'text' }: CSFieldProps) {
    return (
        <div className="cs-field">
            <p className="cs-label">{icon} {label}</p>
            {editing ? (
                <input
                    id={id}
                    type={inputType}
                    className="cs-input"
                    value={inputValue}
                    onChange={(e) => onChange(e.target.value)}
                    placeholder={`Enter ${label.toLowerCase()}`}
                />
            ) : (
                <span className="cs-field-val">{value}</span>
            )}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Scoped styles
// ---------------------------------------------------------------------------
function Style() {
    return (
        <style>{`
      .cs-wrap {
        display: flex; flex-direction: column; gap: 16px;
      }

      /* Header */
      .cs-header {
        display: flex; justify-content: space-between; align-items: flex-start; gap: 8px;
      }
      .cs-title {
        font-size: 15px; font-weight: 700; color: #0f172a; margin: 0;
      }
      .cs-subtitle {
        font-size: 12px; color: #64748b; margin: 2px 0 0;
      }
      .cs-header-actions {
        display: flex; align-items: center; gap: 8px; flex-shrink: 0;
      }

      /* VIP badge (view mode) */
      .cs-vip-badge {
        display: inline-flex; align-items: center; gap: 4px;
        padding: 3px 8px; border-radius: 20px;
        background: linear-gradient(135deg, #fde68a, #f59e0b);
        color: #92400e; font-size: 11px; font-weight: 700;
      }

      /* Edit / Save / Cancel buttons */
      .cs-edit-btn {
        display: inline-flex; align-items: center; gap: 5px;
        padding: 5px 12px; border-radius: 8px; border: 1px solid #e2e8f0;
        background: #f8fafc; color: #475569; font-size: 13px; font-weight: 500;
        cursor: pointer; transition: all 0.15s;
      }
      .cs-edit-btn:hover { background: #f1f5f9; border-color: #cbd5e1; }
      .cs-edit-actions { display: flex; gap: 8px; }
      .cs-cancel-btn {
        display: inline-flex; align-items: center; gap: 5px;
        padding: 5px 12px; border-radius: 8px; border: 1px solid #e2e8f0;
        background: #f8fafc; color: #64748b; font-size: 13px; font-weight: 500;
        cursor: pointer;
      }
      .cs-save-btn {
        display: inline-flex; align-items: center; gap: 5px;
        padding: 5px 14px; border-radius: 8px; border: none;
        background: linear-gradient(135deg, #6366f1, #4f46e5);
        color: #fff; font-size: 13px; font-weight: 600;
        cursor: pointer; transition: opacity 0.15s;
      }
      .cs-save-btn:disabled { opacity: 0.6; cursor: not-allowed; }
      .cs-spin { animation: cs-spin 0.8s linear infinite; }
      @keyframes cs-spin { to { transform: rotate(360deg); } }

      /* Complexity row */
      .cs-complexity-row {
        display: flex; flex-direction: column; gap: 8px;
      }
      .cs-complexity-picker {
        display: flex; flex-wrap: wrap; gap: 6px;
      }
      .cs-complexity-btn {
        display: inline-flex; flex-direction: column; align-items: center;
        padding: 8px 14px; border-radius: 10px;
        border: 2px solid #e2e8f0; background: #f8fafc;
        color: #475569; font-size: 12px; font-weight: 600;
        cursor: pointer; transition: all 0.15s; gap: 2px;
      }
      .cs-complexity-btn:hover { border-color: #6366f1; color: #6366f1; background: #f5f3ff; }
      .cs-complexity-btn--active {
        border-color: #6366f1; background: #6366f1; color: #fff;
      }
      .cs-complexity-btn--active .cs-complexity-sla { opacity: 0.8; }
      .cs-complexity-sla { font-size: 10px; font-weight: 400; opacity: 0.6; }

      /* Complexity chip (view) */
      .cs-complexity-display { display: flex; }
      .cs-chip {
        display: inline-flex; align-items: center; gap: 6px;
        padding: 4px 12px; border-radius: 20px;
        font-size: 12px; font-weight: 700;
      }
      .cs-chip-sla { font-size: 10px; font-weight: 400; opacity: 0.75; }
      .cs-chip--simple       { background: #dcfce7; color: #166534; }
      .cs-chip--std1         { background: #dbeafe; color: #1e40af; }
      .cs-chip--std2         { background: #ede9fe; color: #5b21b6; }
      .cs-chip--complex      { background: #fef3c7; color: #92400e; }
      .cs-chip--nonstandard  { background: #fee2e2; color: #991b1b; }
      .cs-na { font-size: 13px; color: #94a3b8; font-style: italic; }

      /* VIP toggle */
      .cs-field-row {
        display: flex; align-items: center; justify-content: space-between;
        padding: 10px 14px;
        background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 10px;
      }
      .cs-toggle {
        position: relative; display: inline-flex; align-items: center;
        width: 80px; height: 28px;
        border-radius: 14px; border: none; cursor: pointer;
        background: #e2e8f0; transition: background 0.2s; gap: 0;
        padding: 0 4px;
      }
      .cs-toggle--on { background: linear-gradient(135deg, #f59e0b, #d97706); }
      .cs-toggle-thumb {
        position: absolute; left: 4px;
        width: 20px; height: 20px; border-radius: 50%;
        background: #fff; box-shadow: 0 1px 3px rgba(0,0,0,0.2);
        transition: transform 0.2s;
      }
      .cs-toggle--on .cs-toggle-thumb { transform: translateX(52px); }
      .cs-toggle-label {
        position: absolute; right: 8px;
        font-size: 10px; font-weight: 700; color: #fff;
        transition: all 0.2s;
      }
      .cs-toggle--on .cs-toggle-label { right: auto; left: 8px; }
      
      .cs-vip-on { color: #d97706; font-weight: 700; }

      /* 2-col field grid */
      .cs-grid {
        display: grid; grid-template-columns: 1fr 1fr;
        gap: 10px;
      }
      .cs-field {
        display: flex; flex-direction: column; gap: 4px;
        padding: 10px 12px;
        background: #f8fafc; border: 1px solid #e2e8f0; border-radius: 10px;
      }
      .cs-label {
        display: inline-flex; align-items: center; gap: 4px;
        font-size: 11px; font-weight: 600; text-transform: uppercase;
        letter-spacing: 0.05em; color: #94a3b8; margin: 0;
      }
      .cs-field-val {
        font-size: 14px; font-weight: 600; color: #1e293b;
      }
      .cs-field-val--accent { color: #d97706; }
      .cs-input {
        font-size: 13px; font-weight: 500; color: #1e293b;
        border: 1px solid #c7d2fe; border-radius: 6px;
        padding: 4px 8px; background: #fff;
        outline: none; transition: border-color 0.15s;
        width: 100%; box-sizing: border-box;
      }
      .cs-input:focus { border-color: #6366f1; box-shadow: 0 0 0 3px rgba(99,102,241,0.12); }

      /* Mini timeline */
      .cs-timeline {
        padding: 12px;
        border: 1px dashed #e2e8f0; border-radius: 10px;
        display: flex; flex-direction: column; gap: 8px;
      }
      .cs-timeline-track {
        display: flex; flex-wrap: wrap; align-items: center; gap: 6px;
        font-size: 12px;
      }
      .cs-timeline-pill {
        padding: 3px 10px; border-radius: 20px;
        font-size: 12px; font-weight: 600;
      }
      .cs-timeline-pill--neutral { background: #f1f5f9; color: #475569; }
      .cs-timeline-pill--brand   { background: #ede9fe; color: #6366f1; }
      .cs-timeline-pill--accent  { background: #fff7ed; color: #c2410c; }
      .cs-timeline-arrow { color: #cbd5e1; font-size: 13px; }

      @media (max-width: 640px) {
        .cs-grid { grid-template-columns: 1fr; }
        .cs-complexity-picker { flex-direction: column; }
      }

      /* Dark mode overrides */
      @media (prefers-color-scheme: dark) {
        .cs-title { color: #f1f5f9; }
        .cs-subtitle { color: #94a3b8; }
        .cs-field, .cs-field-row, .cs-timeline {
          background: rgba(255,255,255,0.04);
          border-color: rgba(255,255,255,0.08);
        }
        .cs-field-val { color: #e2e8f0; }
        .cs-input { background: #1e293b; border-color: #334155; color: #e2e8f0; }
        .cs-edit-btn, .cs-cancel-btn {
          background: #1e293b; border-color: #334155; color: #94a3b8;
        }
        .cs-complexity-btn { background: #1e293b; border-color: #334155; color: #94a3b8; }
        .cs-complexity-btn:hover { background: #312e81; }
        .cs-timeline-pill--neutral { background: #1e293b; color: #94a3b8; }
        .cs-timeline-pill--brand   { background: rgba(99,102,241,0.15); color: #a5b4fc; }
        .cs-timeline-pill--accent  { background: rgba(251,146,60,0.12); color: #fb923c; }
      }
    `}</style>
    );
}
