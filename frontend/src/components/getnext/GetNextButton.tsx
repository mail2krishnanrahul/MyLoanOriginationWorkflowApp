import { Loader2, Zap, AlertCircle } from 'lucide-react';
import { useClaimNextCase } from '@/hooks/useGetNext';
import type { GetNextResult } from '@/types/getnext';

interface GetNextButtonProps {
    /** Optional case type filter (e.g. 'HOME_LOAN'). */
    caseTypeCode?: string;
    /** Called with the claimed case on success. */
    onCaseClaimed?: (result: GetNextResult) => void;
    /** Called when the queue is empty or user is at capacity. */
    onNoCase?: (message: string) => void;
    /** 'full' renders a large primary button; 'compact' renders icon-only. */
    variant?: 'full' | 'compact';
    className?: string;
}

/**
 * GetNextButton — One-click intelligent case assignment.
 * Claims the highest-scored eligible case for the current operator.
 */
export function GetNextButton({
    caseTypeCode,
    onCaseClaimed,
    onNoCase,
    variant = 'full',
    className = '',
}: GetNextButtonProps) {
    const { mutate, isPending, isError, error } = useClaimNextCase();

    function handleClick() {
        mutate(caseTypeCode, {
            onSuccess: (result) => {
                onCaseClaimed?.(result);
            },
            onError: (err: unknown) => {
                const msg = err instanceof Error ? err.message : 'No eligible cases found';
                onNoCase?.(msg);
            },
        });
    }

    if (variant === 'compact') {
        return (
            <button
                id="getnext-compact-btn"
                onClick={handleClick}
                disabled={isPending}
                title="Get Next Case"
                className={`getnext-compact-btn ${className}`}
                aria-label="Get next case"
            >
                {isPending ? (
                    <Loader2 size={16} className="animate-spin" />
                ) : (
                    <Zap size={16} />
                )}
            </button>
        );
    }

    return (
        <div className={`getnext-btn-wrapper ${className}`}>
            <button
                id="getnext-claim-btn"
                onClick={handleClick}
                disabled={isPending}
                className="getnext-btn"
                aria-busy={isPending}
            >
                <span className="getnext-btn-icon">
                    {isPending ? (
                        <Loader2 size={20} className="animate-spin" />
                    ) : (
                        <Zap size={20} />
                    )}
                </span>
                <span className="getnext-btn-label">
                    {isPending ? 'Finding your next case…' : 'Get Next Case'}
                </span>
            </button>

            {isError && (
                <div className="getnext-error" role="alert" id="getnext-error-banner">
                    <AlertCircle size={14} />
                    <span>
                        {error instanceof Error ? error.message : 'Could not claim a case. Please try again.'}
                    </span>
                </div>
            )}

            <style>{`
        .getnext-btn-wrapper {
          display: flex;
          flex-direction: column;
          gap: 8px;
        }

        .getnext-btn {
          display: inline-flex;
          align-items: center;
          gap: 10px;
          padding: 12px 24px;
          background: linear-gradient(135deg, #6366f1 0%, #4f46e5 100%);
          color: #fff;
          border: none;
          border-radius: 12px;
          font-size: 15px;
          font-weight: 600;
          cursor: pointer;
          transition: all 0.2s ease;
          box-shadow: 0 4px 14px rgba(99, 102, 241, 0.4);
          position: relative;
          overflow: hidden;
        }

        .getnext-btn:hover:not(:disabled) {
          transform: translateY(-1px);
          box-shadow: 0 6px 20px rgba(99, 102, 241, 0.5);
          background: linear-gradient(135deg, #818cf8 0%, #6366f1 100%);
        }

        .getnext-btn:active:not(:disabled) {
          transform: translateY(0);
          box-shadow: 0 2px 8px rgba(99, 102, 241, 0.3);
        }

        .getnext-btn:disabled {
          opacity: 0.7;
          cursor: not-allowed;
          transform: none;
        }

        .getnext-btn::after {
          content: '';
          position: absolute;
          inset: 0;
          background: linear-gradient(135deg, rgba(255,255,255,0.15) 0%, transparent 60%);
          border-radius: inherit;
          pointer-events: none;
        }

        .getnext-btn-icon {
          display: flex;
          align-items: center;
          flex-shrink: 0;
        }

        .getnext-btn-label {
          white-space: nowrap;
        }

        .getnext-error {
          display: flex;
          align-items: center;
          gap: 6px;
          padding: 8px 12px;
          background: #fef2f2;
          border: 1px solid #fecaca;
          border-radius: 8px;
          font-size: 13px;
          color: #dc2626;
        }

        .getnext-compact-btn {
          display: inline-flex;
          align-items: center;
          justify-content: center;
          width: 36px;
          height: 36px;
          background: linear-gradient(135deg, #6366f1 0%, #4f46e5 100%);
          color: #fff;
          border: none;
          border-radius: 8px;
          cursor: pointer;
          transition: all 0.2s ease;
          box-shadow: 0 2px 8px rgba(99, 102, 241, 0.3);
        }

        .getnext-compact-btn:hover:not(:disabled) {
          transform: translateY(-1px);
          box-shadow: 0 4px 12px rgba(99, 102, 241, 0.45);
        }

        .getnext-compact-btn:disabled {
          opacity: 0.7;
          cursor: not-allowed;
        }

        @keyframes spin {
          to { transform: rotate(360deg); }
        }
        .animate-spin {
          animation: spin 1s linear infinite;
        }
      `}</style>
        </div>
    );
}
