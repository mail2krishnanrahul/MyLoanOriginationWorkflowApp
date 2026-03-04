import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useSyncFiltersWithURL } from '@/hooks/useSyncFiltersWithURL';
import { CaseSummaryStats } from '@/components/cases/CaseSummaryStats';
import { CaseFilterBar } from '@/components/cases/CaseFilterBar';
import { CaseCardGrid } from '@/components/cases/CaseCardGrid';
import { useCaseListStore } from '@/stores/caseListStore';
import { GetNextButton } from '@/components/getnext/GetNextButton';
import { GetNextPreviewPanel } from '@/components/getnext/GetNextPreviewPanel';
import { QueueDepthIndicator } from '@/components/getnext/QueueDepthIndicator';
import { CapacityModal } from '@/components/getnext/CapacityModal';
import type { GetNextResult } from '@/types/getnext';
import type { UserCapacityInfo } from '@/types/getnext';

export default function CaseListPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const store = useCaseListStore();
  const [capacityInfo, setCapacityInfo] = useState<UserCapacityInfo | null>(null);

  useSyncFiltersWithURL();

  // If route is /cases/my, ensure assignedToMe is true on mount
  useEffect(() => {
    if (location.pathname === '/cases/my') {
      store.setAdvancedFilter('assignedToMe', true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.pathname]);

  function handleCaseClaimed(result: GetNextResult) {
    // Navigate to the claimed case detail page
    navigate(`/cases/${result.case.id}`);
  }

  function handleNoCase(message: string) {
    // When blocked by capacity the handler provides capacity info via message
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
