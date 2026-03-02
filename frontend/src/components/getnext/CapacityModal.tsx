import { X, Battery, AlertTriangle } from 'lucide-react';
import type { UserCapacityInfo } from '@/types/getnext';

interface CapacityModalProps {
  capacity: UserCapacityInfo;
  onClose: () => void;
}

/**
 * CapacityModal — Shown when a user is at capacity.
 * Presents workload breakdown and actionable advice.
 */
export function CapacityModal({ capacity, onClose }: CapacityModalProps) {
  const pct = Math.round(capacity.capacityPct * 100);

  // Determine bar fill colour
  const barColour =
    capacity.isAtCapacity ? '#dc2626' :
      capacity.isNearCapacity ? '#d97706' : '#6366f1';

  return (
    <div
      className="cm-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="cm-title"
      id="capacity-modal"
    >
      <div className="cm-panel">
        <div className="cm-header">
          <div className="cm-icon-wrap">
            <Battery size={22} />
          </div>
          <h2 className="cm-title" id="cm-title">Workload Capacity</h2>
          <button className="cm-close" onClick={onClose} aria-label="Close">
            <X size={18} />
          </button>
        </div>

        {capacity.isAtCapacity && (
          <div className="cm-alert" role="alert">
            <AlertTriangle size={16} />
            <span>You are at maximum capacity. Complete or transfer a case before claiming more.</span>
          </div>
        )}

        <div className="cm-gauge">
          <div className="cm-gauge-labels">
            <span>Current load</span>
            <span className="cm-gauge-pct" style={{ color: barColour }}>{pct}%</span>
          </div>
          <div className="cm-gauge-track">
            <div
              className="cm-gauge-fill"
              style={{ width: `${Math.min(pct, 100)}%`, background: barColour }}
            />
          </div>
          <div className="cm-gauge-counts">
            <span><strong>{capacity.activeCases}</strong> active cases</span>
            <span><strong>{capacity.maxActiveCases}</strong> maximum</span>
          </div>
        </div>

        <div className="cm-advice">
          {capacity.isAtCapacity
            ? 'Contact your supervisor to adjust your capacity limit, or complete an existing case first.'
            : capacity.isNearCapacity
              ? 'You are approaching your limit. Consider completing in-flight cases before claiming more.'
              : 'Your workload is within normal range.'}
        </div>

        <div className="cm-actions">
          <button className="cm-close-btn" onClick={onClose} id="capacity-close-btn">
            Close
          </button>
        </div>
      </div>

      <style>{`
        .cm-overlay {
          position: fixed; inset: 0;
          background: rgba(15,23,42,0.5);
          backdrop-filter: blur(4px);
          display: flex; align-items: center; justify-content: center;
          z-index: 1000; padding: 16px;
        }
        .cm-panel {
          background: #fff; border-radius: 16px;
          width: 100%; max-width: 400px;
          box-shadow: 0 20px 60px rgba(0,0,0,0.18);
          padding: 24px; display: flex; flex-direction: column; gap: 18px;
        }
        .cm-header {
          display: flex; align-items: center; gap: 10px;
        }
        .cm-icon-wrap {
          display: flex; align-items: center; justify-content: center;
          width: 40px; height: 40px;
          background: #f5f3ff; color: #6366f1;
          border-radius: 10px; flex-shrink: 0;
        }
        .cm-title { flex: 1; font-size: 18px; font-weight: 700; color: #0f172a; margin: 0; }
        .cm-close {
          background: none; border: none; cursor: pointer; color: #64748b;
          padding: 4px; border-radius: 6px; transition: background 0.15s;
        }
        .cm-close:hover { background: #f1f5f9; }
        .cm-alert {
          display: flex; align-items: flex-start; gap: 8px;
          padding: 12px 14px;
          background: #fef2f2; border: 1px solid #fecaca;
          border-radius: 10px; font-size: 13px; color: #dc2626;
        }
        .cm-gauge { display: flex; flex-direction: column; gap: 8px; }
        .cm-gauge-labels {
          display: flex; justify-content: space-between;
          font-size: 13px; font-weight: 600; color: #475569;
        }
        .cm-gauge-pct { font-size: 22px; font-weight: 800; font-variant-numeric: tabular-nums; }
        .cm-gauge-track {
          height: 12px; background: #f1f5f9; border-radius: 6px; overflow: hidden;
        }
        .cm-gauge-fill {
          height: 100%; border-radius: 6px;
          transition: width 0.6s ease, background 0.3s ease;
        }
        .cm-gauge-counts {
          display: flex; justify-content: space-between;
          font-size: 12px; color: #94a3b8;
        }
        .cm-advice {
          font-size: 13px; color: #475569;
          padding: 12px 14px;
          background: #f8fafc; border-radius: 10px;
          line-height: 1.5;
        }
        .cm-actions { display: flex; justify-content: flex-end; }
        .cm-close-btn {
          padding: 9px 20px;
          background: #f1f5f9; border: 1px solid #e2e8f0;
          border-radius: 8px; font-size: 14px; font-weight: 500; color: #475569;
          cursor: pointer; transition: all 0.15s;
        }
        .cm-close-btn:hover { background: #e2e8f0; color: #1e293b; }
      `}</style>
    </div>
  );
}
