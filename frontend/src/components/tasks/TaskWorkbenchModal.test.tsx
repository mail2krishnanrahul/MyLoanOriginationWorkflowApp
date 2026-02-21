import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { TaskWorkbenchModal } from '@/components/tasks/TaskWorkbenchModal';
import { renderWithProviders } from '@/test/test-utils';

const taskResponse = {
  id: 'task-1',
  name: 'Capture borrower declaration',
  caseId: 'case-1',
  caseReference: 'CASE-2026-0001',
  status: 'IN_PROGRESS',
  priority: 'HIGH',
  slaStatus: 'WARNING',
  createdAt: '2026-02-20T11:00:00Z',
  dueAt: '2026-02-21T20:00:00Z',
  inputPayload: {
    borrower: 'Morgan Price'
  },
  outputPayload: {},
  outputSchema: [
    {
      key: 'declarationText',
      label: 'Declaration',
      type: 'textarea',
      required: true
    }
  ]
};

describe('TaskWorkbenchModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('enforces required dynamic fields before enabling completion', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue(
      new Response(JSON.stringify(taskResponse), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      })
    );

    const user = userEvent.setup();
    renderWithProviders(<TaskWorkbenchModal taskId="task-1" open onOpenChange={() => undefined} />);

    await waitFor(() => {
      expect(screen.getByText('Capture borrower declaration')).toBeInTheDocument();
    });

    const completeButton = screen.getByRole('button', { name: 'Complete Task' });
    expect(completeButton).toBeDisabled();

    await user.type(screen.getByLabelText(/Declaration/i), 'Borrower accepted all disclosures.');

    await waitFor(() => {
      expect(completeButton).toBeEnabled();
    });
  });
});
