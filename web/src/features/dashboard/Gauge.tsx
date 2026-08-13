import { cn } from '../../lib/cn';

interface Props {
  label: string;
  /** 0..100, or negative when the reading is not available yet. */
  percent: number;
  /** The absolute figures behind the percentage, e.g. "3.1 GB / 8.0 GB". */
  detail?: string;
  /** Shown under the bar, e.g. the CPU model or the filesystem. */
  hint?: string;
  unknownLabel: string;
}

/**
 * One host reading as a labelled bar.
 *
 * The colour is the reading itself, not decoration: a disk at 92% is the one
 * thing on this page an operator needs to see without reading any numbers.
 */
export function Gauge({ label, percent, detail, hint, unknownLabel }: Props) {
  const known = percent >= 0;
  const width = known ? Math.min(100, Math.max(0, percent)) : 0;

  return (
    <div className="card p-4">
      <div className="flex items-baseline justify-between gap-2">
        <span className="truncate text-sm text-muted">{label}</span>
        <span className="text-lg font-semibold tabular-nums">
          {known ? `${percent.toFixed(0)}%` : unknownLabel}
        </span>
      </div>

      <div
        className="mt-2 h-2 overflow-hidden rounded-full bg-elevated"
        role="progressbar"
        aria-label={label}
        aria-valuenow={known ? Math.round(width) : undefined}
        aria-valuemin={0}
        aria-valuemax={100}
      >
        <div
          className={cn('h-full rounded-full transition-[width]', barColor(percent))}
          style={{ width: `${width}%` }}
        />
      </div>

      {detail && <div className="mt-2 text-xs tabular-nums text-muted">{detail}</div>}
      {hint && (
        <div className="mt-0.5 truncate text-xs text-muted" title={hint}>
          {hint}
        </div>
      )}
    </div>
  );
}

/**
 * The thresholds are deliberately conservative for a disk-shaped reading: by
 * the time a Docker host's filesystem is 90% full, pulls have started failing.
 */
function barColor(percent: number): string {
  if (percent >= 90) return 'bg-danger';
  if (percent >= 75) return 'bg-warn';
  return 'bg-accent';
}
