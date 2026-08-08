import { create } from 'zustand';

export type ConnectionState = 'connected' | 'reconnecting' | 'offline';

interface ConnectionStore {
  /** Whether the browser can reach the daemon. */
  api: ConnectionState;
  /** Whether the daemon can reach Docker. */
  dockerReachable: boolean;
  dockerError: string | null;
  setApi: (state: ConnectionState) => void;
  setDocker: (reachable: boolean, error?: string | null) => void;
}

/**
 * Connection health, shared by the reconnecting banner and every screen that
 * needs to explain why it has no data.
 */
export const useConnection = create<ConnectionStore>((set) => ({
  api: 'connected',
  dockerReachable: true,
  dockerError: null,
  setApi: (api) => set({ api }),
  setDocker: (dockerReachable, dockerError = null) => set({ dockerReachable, dockerError }),
}));
