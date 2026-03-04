import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useSyncFiltersWithURL } from '@/hooks/useSyncFiltersWithURL';
import { CaseSummaryStats } from '@/components/cases/CaseSummaryStats';
import { CaseFilterBar } from '@/components/cases/CaseFilterBar';
import { CaseCardGrid } from '@/components/cases/CaseCardGrid';
import { useCaseListStore } from '@/stores/caseListStore';
import { useUiStore } from '@/store/ui-store';
import { GetNextButton } from '@/components/getnext/GetNextButton';
import { GetNextPreviewPanel } from '@/components/getnext/GetNextPreviewPanel';
import { QueueDepthIndicator } from '@/components/getnext/QueueDepthIndicator';
import { CapacityModal } from '@/components/getnext/CapacityModal';
import type { GetNextResult } from '@/types/getnext';
import type { UserCapacityInfo } from '@/types/getnext';

type CaseScope = 'my' | 'team' | 'all';

const scopeTabs: { value: CaseScope; label: string }[] = [
  { value: 'my', label: 'My Cases' },
  { value: 'team', label: "My Team's Cases" },
  { value: 'all', label: 'All Cases' },
];

export default function CaseListPage() {
  const navigate = useNavigate();
  const store = useCaseListStore();
  const caseScope = useUiStore((s) => s.caseScope);
  const setCaseScope = useUiStore((s) => s.setCaseScope);
  const [capacityInfo, setCapacityInfo] = useState<UserCapacityInfo | null>(null);

  useSyncFiltersWithURL();

  function handleScopeChange(scope: CaseScope) {
    setCaseScope(scope);
    // Update the filter store to match scope
    if (scope === 'my') {
      store.setAdvancedFilter('assignedToMe', true);
      store.setAdvancedFilter('teamId', null);
    } else if (scope === 'team') {
      store.setAdvancedFilter('assignedToMe', false);
      store.setAdvancedFilter('teamId', 'current');
    } else {
      store.setAdvancedFilter('assignedToMe', false);
      store.setAdvancedFilter('teamId', null);
    }
  }

  function handleCaseClaimed(result: GetNextResult) {
    navigate(`/cases/${result.case.id}`);
  }

  function handleNoCase(message: string) {
    console.warn('GetNext:', message);
  }

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8 w-full">
      <div className="mb-8 flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight" style={{ color: 'rgb(var(--fg))' }}>Cases</h1>
          <p className="mt-2 text-sm" style={{ color: 'rgb(var(--fg-muted))' }}>
            Manage and track all loan origination cases across the system.
          </p>
        </div>

        {/* GetNext controls */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', flexWrap: 'wrap' }}>
          <QueueDepthIndicator variant="compact" />
          <GetNextButton
            onCaseClaimed={handleCaseClaimed}
            onNoCase={handleNoCase}
            variant="full"
          />
        </div>
      </div>

      {/* Scope tabs */}
      <div className="mb-6 flex gap-1 rounded-xl p-1" style={{ backgroundColor: 'rgb(var(--bg-muted))' }}>
        {scopeTabs.map((tab) => (
          <button
            key={tab.value}
            type="button"
            onClick={() => handleScopeChange(tab.value)}
            className="flex-1 rounded-lg px-4 py-2 text-sm font-medium transition-all"
            style={
              caseScope === tab.value
                ? {
                    backgroundColor: 'rgb(var(--bg-panel))',
                    color: 'rgb(var(--fg))',
                    boxShadow: '0 1px 3px rgb(0 0 0 / 0.1)',
                  }
                : {
                    color: 'rgb(var(--fg-muted))',
                  }
            }
          >
            {tab.label}
          </button>
        ))}
      </div>

      <CaseSummaryStats />

      {/* GetNext Preview Panel */}
      <div className="mb-4">
        <GetNextPreviewPanel />
      </div>

      <div className="mb-6">
        <CaseFilterBar />
      </div>

      <CaseCardGrid />

      {/* Capacity Modal */}
      {capacityInfo && (
        <CapacityModal
          capacity={capacityInfo}
          onClose={() => setCapacityInfo(null)}
        />
      )}
    </div>
  );
}
