import { create } from 'zustand';
import type { CaseListFilters, CaseStatus, CasePriority } from '@/types/cases';

interface CaseListState {
    filters: CaseListFilters;
    page: number;
    pageSize: number;
    advancedFiltersOpen: boolean;

    setSearch: (search: string) => void;
    setStatusFilter: (statuses: CaseStatus[]) => void;
    setPriorityFilter: (priorities: CasePriority[]) => void;
    toggleAdvancedFilters: () => void;
    setAdvancedFilter: (key: keyof CaseListFilters, value: unknown) => void;
    resetFilters: () => void;
    setPage: (page: number) => void;
    setAllFilters: (filters: CaseListFilters, page: number) => void;
}

const initialFilters: CaseListFilters = {
    search: '',
    statuses: [],
    priorities: [],
    complexities: [],
    skillCodes: [],
    assignedToMe: true,
    hasBlockingErrors: false,
    isVip: false,
    slaDueBefore: null,
    createdAfter: null,
    createdBefore: null,
    teamId: null,
};

export const useCaseListStore = create<CaseListState>((set) => ({
    filters: { ...initialFilters },
    page: 1,
    pageSize: 20,
    advancedFiltersOpen: false,

    setSearch: (search) => set((state) => ({ filters: { ...state.filters, search }, page: 1 })),

    setStatusFilter: (statuses) => set((state) => ({ filters: { ...state.filters, statuses }, page: 1 })),

    setPriorityFilter: (priorities) => set((state) => ({ filters: { ...state.filters, priorities }, page: 1 })),

    toggleAdvancedFilters: () => set((state) => ({ advancedFiltersOpen: !state.advancedFiltersOpen })),

    setAdvancedFilter: (key, value) => set((state) => ({
        filters: { ...state.filters, [key]: value },
        page: 1
    })),

    resetFilters: () => set({ filters: { ...initialFilters }, page: 1 }),

    setPage: (page) => set({ page }),

    setAllFilters: (filters, page) => set({ filters, page })
}));
