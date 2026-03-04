import { QueryClientProvider } from '@tanstack/react-query';
import { useEffect, type PropsWithChildren } from 'react';
import { Toaster } from 'sonner';
import { queryClient } from '@/app/query-client';
import { useUiStore, themeMap } from '@/store/ui-store';

const themeClasses = ['theme-ocean', 'theme-sunset', 'theme-forest', 'theme-arctic'];

export function AppProviders({ children }: PropsWithChildren) {
  const theme = useUiStore((state) => state.theme);

  useEffect(() => {
    const cfg = themeMap[theme];
    const html = document.documentElement;

    // Set dark/light base
    html.classList.toggle('dark', cfg.base === 'dark');

    // Remove all theme classes, then add current
    themeClasses.forEach((cls) => html.classList.remove(cls));
    if (cfg.cssClass) {
      html.classList.add(cfg.cssClass);
    }
  }, [theme]);

  return (
    <QueryClientProvider client={queryClient}>
      {children}
      <Toaster
        richColors
        closeButton
        toastOptions={{
          className: 'font-sans',
          duration: 3500
        }}
      />
    </QueryClientProvider>
  );
}
