import React, { useState } from 'react';
import { X, AlertCircle } from 'lucide-react';
import { useSkipCase } from '@/hooks/useGetNext';
import type { SkipReason } from '@/types/getnext';

interface SkipReasonModalProps {
    caseId: string;
    caseRef: string;
    onClose: () => void;
    onSkipped: () => void;
}

const REASONS: Array<{ value: SkipReason; label: string; description: string }> = [
    {
        value: 'CONFLICT_OF_INTEREST',
        label: 'Conflict of Interest',
        description: 'I have a personal or financial interest in this case',
    },
    {
        value: 'TOO_COMPLEX',
        label: 'Too Complex',
        description: 'This case requires expertise I currently do not have',
    },
    {
        value: 'WRONG_SKILL',
        label: 'Wrong Skill Set',
        description: 'I am not trained for the specific requirements of this case',
    },
    {
        value: 'FREE_TEXT',
        label: 'Enter custom reason',
        description: '',
    },
    {
        value: 'OTHER',
        label: 'Other',
        description: 'Another reason not listed above',
    },
];

/**
 * SkipReasonModal — Captures why an operator is skipping a case.
 * Uses useSkipCase mutation.
 */
export function SkipReasonModal({ caseId, caseRef, onClose, onSkipped }: SkipReasonModalProps) {
    const [selectedReason, setSelectedReason] = useState<SkipReason | null>(null);
    const [notes, setNotes] = useState('');
    const { mutate, isPending, isError } = useSkipCase();

    function handleSubmit(e: React.FormEvent) {
        e.preventDefault();
        if (!selectedReason) return;
        mutate(
            { caseId, reason: selectedReason, notes },
            {
                onSuccess: () => {
                    onSkipped();
                    onClose();
                },
            }
        );
    }

    const isNoteRequired = selectedReason === 'FREE_TEXT';
    const canSubmit = selectedReason !== null && (!isNoteRequired || notes.trim().length > 0);

    return (
        <div
            className="srm-overlay"
            role="dialog"
            aria-modal="true"
            aria-labelledby="srm-title"
            id="skip-reason-modal"
        >
            <div className="srm-panel">
                <div className="srm-header">
                    <h2 className="srm-title" id="srm-title">
                        Skip Case <span className="srm-ref">{caseRef}</span>
                    </h2>
                    <button className="srm-close" onClick={onClose} aria-label="Close">
                        <X size={18} />
                    </button>
                </div>

                <p className="srm-description">
                    This case will remain in the queue for another operator. Please select a reason.
                </p>

                <form onSubmit={handleSubmit} className="srm-form">
                    <div className="srm-reasons" role="radiogroup" aria-label="Skip reason">
                        {REASONS.map(({ value, label, description }) => (
                            <label
                                key={value}
                                className={`srm-reason ${selectedReason === value ? 'srm-reason-selected' : ''}`}
                            >
                                <input
                                    type="radio"
                                    name="skip-reason"
                                    value={value}
                                    checked={selectedReason === value}
                                    onChange={() => setSelectedReason(value)}
                                    className="srm-radio"
                                />
                                <div>
                                    <div className="srm-reason-label">{label}</div>
                                    {description && (
                                        <div className="srm-reason-description">{description}</div>
                                    )}
                                </div>
                            </label>
                        ))}
                    </div>

                    {(isNoteRequired || selectedReason === 'OTHER') && (
                        <div className="srm-notes-group">
                            <label htmlFor="srm-notes" className="srm-notes-label">
                                Notes {isNoteRequired && <span className="srm-required">*</span>}
                            </label>
                            <textarea
                                id="srm-notes"
                                className="srm-notes"
                                rows={3}
                                value={notes}
                                onChange={(e) => setNotes(e.target.value)}
                                placeholder="Please provide details…"
                                required={isNoteRequired}
                            />
                        </div>
                    )}

                    {isError && (
                        <div className="srm-error" role="alert">
                            <AlertCircle size={14} />
                            <span>Failed to record skip. Please try again.</span>
                        </div>
                    )}

                    <div className="srm-actions">
                        <button type="button" className="srm-cancel" onClick={onClose}>
                            Cancel
                        </button>
                        <button
                            type="submit"
                            className="srm-submit"
                            disabled={!canSubmit || isPending}
                            id="skip-confirm-btn"
                        >
                            {isPending ? 'Recording…' : 'Confirm Skip'}
                        </button>
                    </div>
                </form>
            </div>

            <style>{`
        .srm-overlay {
          position: fixed; inset: 0;
          background: rgba(15,23,42,0.5);
          backdrop-filter: blur(4px);
          display: flex; align-items: center; justify-content: center;
          z-index: 1000;
          padding: 16px;
        }
        .srm-panel {
          background: #fff;
          border-radius: 16px;
          width: 100%; max-width: 480px;
          box-shadow: 0 20px 60px rgba(0,0,0,0.2);
          padding: 24px;
        }
        .srm-header {
          display: flex; justify-content: space-between; align-items: flex-start;
          margin-bottom: 8px;
        }
        .srm-title { font-size: 18px; font-weight: 700; color: #0f172a; margin: 0; }
        .srm-ref { color: #6366f1; font-weight: 500; }
        .srm-close {
          background: none; border: none; cursor: pointer; color: #64748b;
          padding: 4px; border-radius: 6px;
          transition: background 0.15s;
        }
        .srm-close:hover { background: #f1f5f9; color: #1e293b; }
        .srm-description { font-size: 13px; color: #64748b; margin-bottom: 16px; }
        .srm-form { display: flex; flex-direction: column; gap: 16px; }
        .srm-reasons { display: flex; flex-direction: column; gap: 8px; }
        .srm-reason {
          display: flex; align-items: flex-start; gap: 10px;
          padding: 10px 12px;
          border: 1.5px solid #e2e8f0;
          border-radius: 10px;
          cursor: pointer;
          transition: all 0.15s;
        }
        .srm-reason:hover { border-color: #a5b4fc; background: #f8f7ff; }
        .srm-reason-selected { border-color: #6366f1; background: #f5f3ff; }
        .srm-radio { margin-top: 2px; accent-color: #6366f1; flex-shrink: 0; }
        .srm-reason-label { font-size: 13px; font-weight: 600; color: #1e293b; }
        .srm-reason-description { font-size: 12px; color: #64748b; margin-top: 2px; }
        .srm-notes-group { display: flex; flex-direction: column; gap: 4px; }
        .srm-notes-label { font-size: 12px; font-weight: 600; color: #475569; }
        .srm-required { color: #dc2626; margin-left: 2px; }
        .srm-notes {
          border: 1.5px solid #e2e8f0; border-radius: 8px;
          padding: 8px 10px; font-size: 13px; color: #1e293b;
          resize: vertical; outline: none; font-family: inherit;
          transition: border-color 0.15s;
        }
        .srm-notes:focus { border-color: #6366f1; }
        .srm-error {
          display: flex; align-items: center; gap: 6px;
          padding: 8px 12px;
          background: #fef2f2; border: 1px solid #fecaca;
          border-radius: 8px; font-size: 13px; color: #dc2626;
        }
        .srm-actions { display: flex; gap: 8px; justify-content: flex-end; }
        .srm-cancel {
          padding: 9px 18px; border: 1.5px solid #e2e8f0; border-radius: 8px;
          background: #fff; font-size: 14px; font-weight: 500; color: #64748b;
          cursor: pointer; transition: all 0.15s;
        }
        .srm-cancel:hover { background: #f1f5f9; border-color: #cbd5e1; color: #1e293b; }
        .srm-submit {
          padding: 9px 18px;
          background: linear-gradient(135deg, #6366f1 0%, #4f46e5 100%);
          border: none; border-radius: 8px; font-size: 14px; font-weight: 600;
          color: #fff; cursor: pointer; transition: all 0.15s;
        }
        .srm-submit:hover:not(:disabled) { box-shadow: 0 4px 12px rgba(99,102,241,0.4); }
        .srm-submit:disabled { opacity: 0.55; cursor: not-allowed; }
      `}</style>
        </div>
    );
}
