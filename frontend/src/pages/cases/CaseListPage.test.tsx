import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import CaseListPage from '@/pages/cases/CaseListPage';
import { renderWithProviders } from '@/test/test-utils';

const defaultResponse = {
  items: [
    {
      id: 'case-1',
      referenceNumber: 'CASE-2026-0001',
      borrowerName: 'Morgan Price',
      caseType: 'HOME_LOAN',
      stage: 'UNDERWRITING',
      status: 'IN_PROGRESS',
      priority: 'HIGH',
      assignedTo: { id: 'user-1', displayName: 'Alex Lane' },
      slaStatus: 'WARNING',
      slaRemainingMinutes: 240,
      createdAt: '2026-02-20T11:00:00Z',
      updatedAt: '2026-02-21T09:00:00Z'
    }
  ],
  page: 1,
  limit: 25,
  total: 1
};

describe('CaseListPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders case rows from API response', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(defaultResponse), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    );

    renderWithProviders(<CaseListPage />, { route: '/cases' });

    expect(screen.getByText('Case Workbench')).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByText('Morgan Price')).toBeInTheDocument();
      expect(screen.getByText('CASE-2026-0001')).toBeInTheDocument();
    });
  });

  it('shows empty state when no rows exist', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          items: [],
          page: 1,
          limit: 25,
          total: 0
        }),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' }
        }
      )
    );

    const user = userEvent.setup();
    renderWithProviders(<CaseListPage />, { route: '/cases' });

    await waitFor(() => {
      expect(screen.getByText('No cases found')).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(screen.getByText('No cases found')).toBeInTheDocument();
  });
});
