import { QueryClientProvider } from '@tanstack/react-query';
import { useEffect, type PropsWithChildren } from 'react';
import { Toaster } from 'sonner';
import { queryClient } from '@/app/query-client';
import { useUiStore } from '@/store/ui-store';

export function AppProviders({ children }: PropsWithChildren) {
  const theme = useUiStore((state) => state.theme);

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark');
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
