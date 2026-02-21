import { create } from 'zustand';

type CaseScope = 'my' | 'team' | 'all';

type ThemeMode = 'light' | 'dark';

interface UiState {
  sidebarCollapsed: boolean;
  theme: ThemeMode;
  caseScope: CaseScope;
  setSidebarCollapsed: (collapsed: boolean) => void;
  toggleSidebar: () => void;
  setTheme: (theme: ThemeMode) => void;
  setCaseScope: (scope: CaseScope) => void;
}

export const useUiStore = create<UiState>((set) => ({
  sidebarCollapsed: false,
  theme: 'light',
  caseScope: 'my',
  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
  toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
  setTheme: (theme) => set({ theme }),
  setCaseScope: (caseScope) => set({ caseScope })
}));
