import { useMutation, useQueryClient } from '@tanstack/react-query';
import { t } from 'i18next';

import { containers as containersApi } from '../../api/endpoints';
import type { ContainerAction } from '../../api/types';
import { toast } from '../../stores/toast';

/**
 * Reports a failed action.
 *
 * Success is silent for the single-container actions: the row's own state
 * changes in front of the operator, and a toast saying what they can already
 * see is noise. A failure is the case where the screen does not change and
 * nothing would otherwise say why.
 */
function reportFailure(what: string) {
  return (error: unknown) => {
    toast.error(what, error instanceof Error ? error.message : undefined);
  };
}

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
    onError: (error, variables) => {
      reportFailure(t('containers.action_failed', { action: variables.action }))(error);
    },
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
    onSuccess: () => {
      // The row disappears, which on a long list is easy to miss.
      toast.success(t('containers.removed'));
    },
    onError: reportFailure(t('containers.remove_failed')),
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
    onSuccess: (result) => {
      // A bulk action is the case a toast exists for: the operator selected
      // twelve rows and cannot check each one.
      if (result.failed === 0) {
        toast.success(
          t('containers.batch_done', { count: result.succeeded, action: result.action }),
        );
        return;
      }
      toast.error(
        t('containers.batch_partial', { failed: result.failed, total: result.total }),
        result.results.find((r) => !r.ok)?.error,
      );
    },
    onError: reportFailure(t('containers.batch_failed')),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['containers'] });
    },
  });
}

export function useContainerRedeploy() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (id: string) => containersApi.redeploy(id),
    onSuccess: (result) => {
      if (result.rolled_back) {
        // The container is running again, but on the old image: a success
        // that an operator must not read as one.
        toast.error(t('containers.redeploy_rolled_back'));
        return;
      }
      toast.success(t('containers.redeployed', { image: result.image }));
    },
    onError: reportFailure(t('containers.redeploy_failed')),
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
    onError: reportFailure(t('containers.rename_failed')),
    onSettled: (_data, _error, variables) => {
      void queryClient.invalidateQueries({ queryKey: ['containers'] });
      void queryClient.invalidateQueries({ queryKey: ['container', variables.id] });
    },
  });
}
