import { create } from 'zustand';

type CaseScope = 'my' | 'team' | 'all';

export type ThemeId = 'light' | 'dark' | 'ocean' | 'sunset' | 'forest' | 'arctic';

export interface ThemeConfig {
  id: ThemeId;
  label: string;
  base: 'light' | 'dark';
  accent: string;        // hex color for swatch preview
  cssClass: string;      // class applied to <html>
}

export const themes: ThemeConfig[] = [
  { id: 'light',  label: 'Light',          base: 'light', accent: '#0f82ff', cssClass: '' },
  { id: 'dark',   label: 'Dark',           base: 'dark',  accent: '#38bdf8', cssClass: '' },
  { id: 'ocean',  label: 'Ocean Breeze',   base: 'dark',  accent: '#06b6d4', cssClass: 'theme-ocean' },
  { id: 'sunset', label: 'Sunset Ember',   base: 'dark',  accent: '#f59e0b', cssClass: 'theme-sunset' },
  { id: 'forest', label: 'Forest Canopy',  base: 'dark',  accent: '#22c55e', cssClass: 'theme-forest' },
  { id: 'arctic', label: 'Arctic Frost',   base: 'light', accent: '#7dd3fc', cssClass: 'theme-arctic' },
];

export const themeMap = Object.fromEntries(themes.map((t) => [t.id, t])) as Record<ThemeId, ThemeConfig>;

interface UiState {
  sidebarCollapsed: boolean;
  theme: ThemeId;
  caseScope: CaseScope;
  setSidebarCollapsed: (collapsed: boolean) => void;
  toggleSidebar: () => void;
  setTheme: (theme: ThemeId) => void;
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
