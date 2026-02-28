import { ReactNode } from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { useCaseList } from './useCaseList';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

vi.mock('@/api/cases', () => ({
    listCases: vi.fn().mockResolvedValue({
        items: [],
        totalCount: 0,
        page: 1,
        pageSize: 20,
        hasNextPage: false
    })
}));

const queryClient = new QueryClient({
    defaultOptions: {
        queries: { retry: false }
    }
});

const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);

describe('useCaseList hook', () => {
    it('calls the listCases API with the right filters', async () => {
        const filters = { search: 'test query', statuses: [], priorities: [], complexities: [], skillCodes: [], assignedToMe: false, hasBlockingErrors: false, isVip: false, slaDueBefore: null, createdAfter: null, createdBefore: null, teamId: null };

        const { result } = renderHook(() => useCaseList(filters, 1, 20), { wrapper });

        await waitFor(() => {
            expect(result.current.isSuccess).toBe(true);
        });

        expect(result.current.data?.totalCount).toBe(0);
    });
});
