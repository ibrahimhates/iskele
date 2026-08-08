import { useMutation, useQueryClient } from '@tanstack/react-query';

import { containers as containersApi } from '../../api/endpoints';
import type { ContainerAction } from '../../api/types';

/**
 * Runs a lifecycle action and refreshes what it changed.
 *
 * Invalidating rather than optimistically patching is deliberate: the engine
 * decides the resulting state (a stop can leave a container "exited" or
 * "dead"), and guessing would show the operator something untrue.
 */
export function useContainerAction() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({
      id,
      action,
    }: {
      id: string;
      action: Exclude<ContainerAction, 'remove'>;
    }) => containersApi.action(id, action),
    onSettled: (_data, _error, variables) => {
      void queryClient.invalidateQueries({ queryKey: ['containers'] });
      void queryClient.invalidateQueries({ queryKey: ['container', variables.id] });
    },
  });
}

export function useContainerRemove() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({
      id,
      force,
      volumes,
    }: {
      id: string;
      force?: boolean;
      volumes?: boolean;
    }) => containersApi.remove(id, { force, volumes }),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['containers'] });
    },
  });
}

export function useContainerBatch() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ ids, action }: { ids: string[]; action: ContainerAction }) =>
      containersApi.batch(ids, action),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['containers'] });
    },
  });
}

export function useContainerRedeploy() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (id: string) => containersApi.redeploy(id),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['containers'] });
    },
  });
}

export function useContainerRename() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string }) =>
      containersApi.rename(id, name),
    onSettled: (_data, _error, variables) => {
      void queryClient.invalidateQueries({ queryKey: ['containers'] });
      void queryClient.invalidateQueries({ queryKey: ['container', variables.id] });
    },
  });
}
