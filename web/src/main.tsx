import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import App from './App';
import './lib/i18n';
import './index.css';
import { applyTheme, useUI } from './stores/ui';
import { ApiError } from './api/client';
import './stores/auth';

// The theme must be on the document before the first paint, or a dark-mode
// user sees a white flash on every load.
applyTheme(useUI.getState().theme);

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      refetchOnWindowFocus: true,
      retry: (failureCount, error) => {
        // Retrying an authorization or validation failure only delays the
        // message the operator needs to see.
        if (error instanceof ApiError && error.status < 500 && !error.isDockerUnavailable) {
          return false;
        }
        return failureCount < 2;
      },
    },
  },
});

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
