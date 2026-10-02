import { useTranslation } from 'react-i18next';

interface Props {
  force: boolean;
  volumes: boolean;
  onForce: (value: boolean) => void;
  onVolumes: (value: boolean) => void;
  /** How many of the targets the engine will refuse without force. */
  running: number;
  /** Compose projects the targets belong to. */
  stacks: string[];
}

/** The options and warnings shared by the single and the bulk remove dialogs. */
export function RemoveOptions({ force, volumes, onForce, onVolumes, running, stacks }: Props) {
  const { t } = useTranslation();

  return (
    <div className="space-y-2 text-sm">
      <label className="flex cursor-pointer items-center gap-2">
        <input
          type="checkbox"
          className="accent-accent"
          checked={force}
          onChange={(e) => onForce(e.target.checked)}
        />
        {t('containers.forceRemove')}
      </label>
      <label className="flex cursor-pointer items-center gap-2">
        <input
          type="checkbox"
          className="accent-accent"
          checked={volumes}
          onChange={(e) => onVolumes(e.target.checked)}
        />
        {t('containers.removeVolumes')}
      </label>

      {running > 0 && !force && (
        <p className="rounded-md border border-warn/40 bg-warn/10 px-3 py-2 text-xs text-warn">
          {t('containers.removeRunningHint', { count: running })}
        </p>
      )}
      {stacks.length > 0 && (
        <p className="rounded-md border border-border bg-elevated px-3 py-2 text-xs text-muted">
          {t('containers.removeStackHint', { stacks: stacks.join(', ') })}
        </p>
      )}
    </div>
  );
}
