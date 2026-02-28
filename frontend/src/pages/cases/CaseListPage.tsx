import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { useSyncFiltersWithURL } from '@/hooks/useSyncFiltersWithURL';
import { CaseSummaryStats } from '@/components/cases/CaseSummaryStats';
import { CaseFilterBar } from '@/components/cases/CaseFilterBar';
import { CaseCardGrid } from '@/components/cases/CaseCardGrid';
import { useCaseListStore } from '@/stores/caseListStore';

export default function CaseListPage() {
  const location = useLocation();
  const store = useCaseListStore();

  useSyncFiltersWithURL();

  // If route is /cases/my, ensure assignedToMe is true on mount
  useEffect(() => {
    if (location.pathname === '/cases/my') {
      store.setAdvancedFilter('assignedToMe', true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.pathname]);

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8 w-full">
      <div className="mb-8 flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 tracking-tight">Cases</h1>
          <p className="mt-2 text-sm text-gray-500">
            Manage and track all loan origination cases across the system.
          </p>
        </div>
        {/* View Switcher placeholder - prompt implies we keep grid view as primary, but if I need ViewSwitcher I can do it easily. For now, we removed the legacy table view so Grid is default. */}
      </div>

      <CaseSummaryStats />

      <div className="mb-6">
        <CaseFilterBar />
      </div>

      <CaseCardGrid />
    </div>
  );
}
