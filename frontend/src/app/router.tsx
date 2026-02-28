import { lazy, Suspense } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { AppShell } from '@/components/layout/AppShell';
import { SkeletonCard } from '@/components/ui/LoadingSkeleton';

const CaseListPage = lazy(() => import('@/pages/cases/CaseListPage'));
const CaseDetailPage = lazy(() => import('@/pages/cases/CaseDetailPage'));
const WorkbasketPage = lazy(() => import('@/pages/workbaskets/WorkbasketPage'));
const DashboardPage = lazy(() => import('@/pages/dashboards/DashboardPage'));
const AdminHubPage = lazy(() => import('@/pages/admin/AdminHubPage'));

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
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<DashboardPage />} />
          <Route path="/admin" element={<AdminHubPage />} />
          <Route path="/cases" element={<CaseListPage />} />
          <Route path="/cases/my" element={<CaseListPage />} />
          <Route path="/cases/:caseId" element={<CaseDetailPage />} />
          <Route path="/workbaskets" element={<WorkbasketPage />} />
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Routes>
      </Suspense>
    </AppShell>
  );
}
