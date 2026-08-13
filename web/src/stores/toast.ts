import { create } from 'zustand';

export type ToastKind = 'success' | 'error' | 'info';

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
  /** A second line: the server's explanation, a count, a reclaimed size. */
  detail?: string;
}

/** How long a toast stays up before dismissing itself, by kind. */
const LIFETIME_MS: Record<ToastKind, number> = {
  success: 4_000,
  info: 5_000,
  // A failure is the one an operator has to read and possibly copy, so it
  // stays until dismissed.
  error: 0,
};

interface ToastState {
  toasts: Toast[];
  /** Shows a toast and returns its id, so a caller can dismiss it early. */
  push: (kind: ToastKind, message: string, detail?: string) => number;
  dismiss: (id: number) => void;
  clear: () => void;
}

let nextID = 0;

/**
 * The notification queue.
 *
 * It lives in a store rather than in React context so that a mutation's
 * `onSuccess` — which is not inside a component — can raise one without every
 * caller threading a hook through.
 */
export const useToasts = create<ToastState>()((set, get) => ({
  toasts: [],

  push: (kind, message, detail) => {
    nextID += 1;
    const id = nextID;
    set((state) => ({ toasts: [...state.toasts, { id, kind, message, detail }] }));

    const lifetime = LIFETIME_MS[kind];
    if (lifetime > 0) {
      setTimeout(() => get().dismiss(id), lifetime);
    }
    return id;
  },

  dismiss: (id) => set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) })),
  clear: () => set({ toasts: [] }),
}));

/**
 * Raises a toast from outside React.
 *
 * The three helpers exist so call sites read as what happened rather than as
 * an enum lookup.
 */
export const toast = {
  success: (message: string, detail?: string) =>
    useToasts.getState().push('success', message, detail),
  error: (message: string, detail?: string) => useToasts.getState().push('error', message, detail),
  info: (message: string, detail?: string) => useToasts.getState().push('info', message, detail),
};
