
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { CaseCardGrid } from './CaseCardGrid';
import { useCaseList } from '@/hooks/useCaseList';

vi.mock('react-router-dom', () => ({
    useNavigate: () => vi.fn()
}));

vi.mock('@/stores/caseListStore', () => ({
    useCaseListStore: () => ({
        filters: { search: '', skillCodes: [] },
        page: 1,
        pageSize: 20,
        resetFilters: vi.fn(),
        setPage: vi.fn(),
        setAdvancedFilter: vi.fn()
    })
}));

vi.mock('@/hooks/useCaseList', () => ({
    useCaseList: vi.fn()
}));

vi.mock('@/hooks/useDebouncedValue', () => ({
    useDebouncedValue: (val: string) => val
}));

describe('CaseCardGrid Component', () => {
    it('renders loading state', () => {
        vi.mocked(useCaseList).mockReturnValue({ isLoading: true } as any);
        render(<CaseCardGrid />);
        expect(screen.getByText('Loading cases...')).toBeInTheDocument();
    });

    it('renders error state', () => {
        vi.mocked(useCaseList).mockReturnValue({ isError: true, error: { message: 'Failed' } } as any);
        render(<CaseCardGrid />);
        expect(screen.getByText('Failed')).toBeInTheDocument();
    });

    it('renders empty state when no items', () => {
        vi.mocked(useCaseList).mockReturnValue({ data: { items: [], totalCount: 0 } } as any);
        render(<CaseCardGrid />);
        expect(screen.getByText('No cases found')).toBeInTheDocument();
    });

    it('renders items', () => {
        const mockItems = [{
            id: '1', reference: 'REF-1', title: 'Test 1', priority: 'LOW', status: 'NEW', tags: [], createdAt: '2026-02-27T10:00:00Z', isVip: false, hasBlockingErrors: false
        }];
        vi.mocked(useCaseList).mockReturnValue({ data: { items: mockItems, totalCount: 1, hasNextPage: false } } as any);
        render(<CaseCardGrid />);
        expect(screen.getByText('Showing 1 of 1 cases')).toBeInTheDocument();
        expect(screen.getByText('Test 1')).toBeInTheDocument();
    });
});
