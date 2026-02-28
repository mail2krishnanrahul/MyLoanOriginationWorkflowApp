
import { screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import CaseListPage from '@/pages/cases/CaseListPage';
import { renderWithProviders } from '@/test/test-utils';

vi.mock('@/hooks/useCaseList', () => ({
  useCaseList: () => ({
    data: {
      items: [
        {
          id: 'case-1',
          reference: 'CASE-2026-0001',
          title: 'Morgan Price',
          status: 'IN_PROGRESS',
          priority: 'HIGH',
          assignedUser: { userId: '1', displayName: 'John', initials: 'J' },
          tags: [],
          createdAt: '2026-02-20T11:00:00Z'
        }
      ],
      totalCount: 1,
      page: 1,
      pageSize: 20,
      hasNextPage: false
    },
    isLoading: false,
    isError: false,
    isFetching: false
  })
}));

vi.mock('@/hooks/useCaseSummaryStats', () => ({
  useCaseSummaryStats: () => ({
    data: {
      totalCases: 1,
      activeCases: 1,
      resolvedCases: 0,
      atRiskCases: 0,
      myActiveCases: 1
    },
    isLoading: false,
    isError: false
  })
}));

describe('CaseListPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders case view header', async () => {
    renderWithProviders(<CaseListPage />, { route: '/cases' });
    expect(screen.getByText('Cases')).toBeInTheDocument();
    expect(screen.getByText('Manage and track all loan origination cases across the system.')).toBeInTheDocument();
  });

  it('renders case cards directly from mocked hooks', async () => {
    renderWithProviders(<CaseListPage />, { route: '/cases' });
    expect(screen.getByText('Morgan Price')).toBeInTheDocument();
    expect(screen.getByText('CASE-2026-0001')).toBeInTheDocument();
  });
});
