
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { CaseFilterBar } from './CaseFilterBar';

vi.mock('@/stores/caseListStore', () => ({
    useCaseListStore: () => ({
        filters: {
            search: '',
            statuses: [],
            priorities: [],
            complexities: [],
            skillCodes: [],
            assignedToMe: false,
            hasBlockingErrors: false,
            isVip: false,
            slaDueBefore: null,
            createdAfter: null,
            createdBefore: null,
            teamId: null
        },
        advancedFiltersOpen: false,
        setSearch: vi.fn(),
        setStatusFilter: vi.fn(),
        setPriorityFilter: vi.fn(),
        toggleAdvancedFilters: vi.fn(),
        setAdvancedFilter: vi.fn(),
        resetFilters: vi.fn()
    })
}));

describe('CaseFilterBar', () => {
    it('renders the search input and basic filters', () => {
        render(<CaseFilterBar />);
        expect(screen.getByPlaceholderText('Search cases...')).toBeInTheDocument();
        expect(screen.getByText('All Statuses')).toBeInTheDocument();
        expect(screen.getByText('All Priorities')).toBeInTheDocument();
    });
});
