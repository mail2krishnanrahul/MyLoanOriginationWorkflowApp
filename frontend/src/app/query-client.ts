import { QueryClient } from '@tanstack/react-query';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 20_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: true,
      refetchInterval: 30_000,
      retry: 1
    },
    mutations: {
      retry: 1
    }
  }
});
