import { lazy, Suspense } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { AppShell } from '@/components/layout/AppShell';
import { SkeletonCard } from '@/components/ui/LoadingSkeleton';

const CaseListPage = lazy(() => import('@/pages/cases/CaseListPage'));
const CaseDetailPage = lazy(() => import('@/pages/cases/CaseDetailPage'));
const WorkbasketPage = lazy(() => import('@/pages/workbaskets/WorkbasketPage'));
const PersonalDashboardPage = lazy(() => import('@/pages/dashboards/PersonalDashboardPage'));
const TeamDashboardPage = lazy(() => import('@/pages/dashboards/TeamDashboardPage'));
const ComplianceDashboardPage = lazy(() => import('@/pages/dashboards/ComplianceDashboardPage'));

function RouteFallback() {
  return (
    <div className="grid gap-4">
      <SkeletonCard className="h-16" />
      <SkeletonCard className="h-[28rem]" />
    </div>
  );
}

export function AppRouter() {
  return (
    <AppShell>
      <Suspense fallback={<RouteFallback />}>
        <Routes>
          <Route path="/" element={<Navigate to="/dashboards/personal" replace />} />
          <Route path="/dashboards/personal" element={<PersonalDashboardPage />} />
          <Route path="/dashboards/team" element={<TeamDashboardPage />} />
          <Route path="/dashboards/compliance" element={<ComplianceDashboardPage />} />
          <Route path="/cases" element={<CaseListPage />} />
          <Route path="/cases/:caseId" element={<CaseDetailPage />} />
          <Route path="/workbaskets" element={<WorkbasketPage />} />
          <Route path="*" element={<Navigate to="/dashboards/personal" replace />} />
        </Routes>
      </Suspense>
    </AppShell>
  );
}
