import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Boxes, Database, HardDrive, Network, Trash2 } from 'lucide-react';

import {
  containers as containersApi,
  images as imagesApi,
  networks as networksApi,
  volumes as volumesApi,
} from '../../api/endpoints';
import type { PruneReport } from '../../api/types';
import { ConfirmDialog } from '../../components/ConfirmDialog';
import { Spinner } from '../../components/Spinner';
import { formatBytes } from '../../lib/format';
import { toast } from '../../stores/toast';

/** One reclaimable resource kind. */
interface Target {
  key: 'containers' | 'images' | 'volumes' | 'networks';
  Icon: typeof Boxes;
  run: () => Promise<PruneReport>;
  /** The queries whose answers the prune invalidates. */
  invalidates: string[];
  /** Whether losing this can cost data rather than just disk. */
  destructive: boolean;
}

const TARGETS: Target[] = [
  {
    key: 'containers',
    Icon: Boxes,
    run: () => containersApi.prune(),
    invalidates: ['containers'],
    destructive: false,
  },
  {
    key: 'images',
    // Dangling only: `all` would remove every image no container currently
    // uses, which on a host with stopped services means the next start has to
    // pull again. That is a different decision, and not one to hide behind the
    // same button.
    Icon: HardDrive,
    run: () => imagesApi.prune(false),
    invalidates: ['images'],
    destructive: false,
  },
  {
    key: 'volumes',
    Icon: Database,
    run: () => volumesApi.prune(),
    invalidates: ['volumes'],
    // A volume is somebody's database. Everything else here is rebuildable.
    destructive: true,
  },
  {
    key: 'networks',
    Icon: Network,
    run: () => networksApi.prune(),
    invalidates: ['networks'],
    destructive: false,
  },
];

/**
 * Reclaiming disk space.
 *
 * Each button deletes things nobody named individually, so each one asks
 * first, and the confirmation says exactly what the engine's own prune rule
 * is — "unused" means something different for every one of these.
 */
export function PrunePanel() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [pending, setPending] = useState<Target | null>(null);

  const prune = useMutation({
    mutationFn: (target: Target) => target.run(),
    onSuccess: (report, target) => {
      setPending(null);
      for (const key of [...target.invalidates, 'system']) {
        void queryClient.invalidateQueries({ queryKey: [key] });
      }

      const count = report.deleted?.length ?? 0;
      if (count === 0) {
        toast.info(t('prune.nothing', { target: t(`prune.${target.key}`) }));
        return;
      }
      toast.success(
        t('prune.done', { count, target: t(`prune.${target.key}`) }),
        report.space_reclaimed > 0
          ? t('prune.reclaimed', { size: formatBytes(report.space_reclaimed) })
          : undefined,
      );
    },
    onError: (error: unknown, target) => {
      setPending(null);
      toast.error(
        t('prune.failed', { target: t(`prune.${target.key}`) }),
        error instanceof Error ? error.message : undefined,
      );
    },
  });

  return (
    <section className="card p-4">
      <h2 className="mb-1 flex items-center gap-2 text-sm font-medium">
        <Trash2 size={16} className="text-muted" aria-hidden />
        {t('prune.title')}
      </h2>
      <p className="mb-3 text-xs text-muted">{t('prune.description')}</p>

      <div className="grid gap-2 sm:grid-cols-2">
        {TARGETS.map((target) => (
          <button
            key={target.key}
            type="button"
            className="btn-default justify-start"
            disabled={prune.isPending}
            onClick={() => setPending(target)}
          >
            {prune.isPending && prune.variables?.key === target.key ? (
              <Spinner className="h-3.5 w-3.5" />
            ) : (
              <target.Icon size={14} aria-hidden />
            )}
            {t(`prune.${target.key}`)}
          </button>
        ))}
      </div>

      {pending && (
        <ConfirmDialog
          open
          title={t('prune.confirm_title', { target: t(`prune.${pending.key}`) })}
          description={t(`prune.${pending.key}_rule`)}
          // A volume holds data that is not coming back from anywhere; the
          // others cost a rebuild or a pull at worst.
          confirmText={pending.destructive ? t(`prune.${pending.key}`) : undefined}
          confirmLabel={t('common.prune')}
          destructive={pending.destructive}
          busy={prune.isPending}
          onCancel={() => setPending(null)}
          onConfirm={() => prune.mutate(pending)}
        />
      )}
    </section>
  );
}
