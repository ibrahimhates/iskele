import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Download, Pause, Play, Search } from 'lucide-react';

import { useLogStream } from './useLogStream';
import { cn } from '../../lib/cn';

const TAIL_OPTIONS = [100, 500, 1000, 5000];

export function LogViewer({ containerID, name }: { containerID: string; name: string }) {
  const { t } = useTranslation();

  const [tail, setTail] = useState(500);
  const [timestamps, setTimestamps] = useState(false);
  const [follow, setFollow] = useState(true);
  const [filter, setFilter] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);

  const { entries, state, error } = useLogStream({
    containerID,
    tail,
    timestamps,
    follow,
    enabled: true,
  });

  const scrollRef = useRef<HTMLDivElement>(null);

  const visible = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return entries;
    return entries.filter((entry) => entry.message.toLowerCase().includes(needle));
  }, [entries, filter]);

  // Following the tail is only useful while the operator is at the bottom;
  // scrolling up to read something must not be undone by the next line.
  useEffect(() => {
    if (!autoScroll) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [visible, autoScroll]);

  function onScroll() {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    setAutoScroll(atBottom);
  }

  function download() {
    const text = entries
      .map((entry) => (entry.timestamp ? `${entry.timestamp} ${entry.message}` : entry.message))
      .join('\n');
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = `${name || containerID}.log`;
    link.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="flex h-[60vh] min-h-80 flex-col">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <div className="relative min-w-48 flex-1">
          <Search
            size={14}
            className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted"
            aria-hidden
          />
          <input
            className="input py-1.5 pl-8 text-sm"
            placeholder={t('logs.filter')}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            aria-label={t('logs.filter')}
          />
        </div>

        <label className="flex items-center gap-1.5 text-sm">
          <span className="text-muted">{t('logs.tail')}</span>
          <select
            className="input w-24 py-1.5 text-sm"
            value={tail}
            onChange={(e) => setTail(Number(e.target.value))}
          >
            {TAIL_OPTIONS.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </label>

        <label className="flex cursor-pointer items-center gap-1.5 text-sm">
          <input
            type="checkbox"
            className="accent-accent"
            checked={timestamps}
            onChange={(e) => setTimestamps(e.target.checked)}
          />
          {t('logs.timestamps')}
        </label>

        <button
          type="button"
          className="btn-default py-1.5"
          onClick={() => setFollow((v) => !v)}
          aria-pressed={follow}
        >
          {follow ? <Pause size={14} aria-hidden /> : <Play size={14} aria-hidden />}
          {t('logs.follow')}
        </button>

        <button type="button" className="btn-default py-1.5" onClick={download}>
          <Download size={14} aria-hidden />
          {t('common.download')}
        </button>

        <StreamIndicator state={state} />
      </div>

      {error && (
        <p className="mb-2 rounded border border-danger/40 bg-danger/10 px-2 py-1 text-xs text-danger">
          {error}
        </p>
      )}

      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="flex-1 overflow-auto rounded-md border border-border bg-black/90 p-3 font-mono text-xs leading-relaxed"
        role="log"
        aria-live="polite"
      >
        {visible.length === 0 ? (
          <p className="text-zinc-500">{t('logs.empty')}</p>
        ) : (
          visible.map((entry) => (
            <div
              key={entry.id}
              className={cn(
                'whitespace-pre-wrap break-all',
                entry.stream === 'stderr' ? 'text-red-400' : 'text-zinc-200',
              )}
            >
              {entry.timestamp && <span className="mr-2 text-zinc-500">{entry.timestamp}</span>}
              {entry.message}
            </div>
          ))
        )}
      </div>

      {!autoScroll && (
        <button
          type="button"
          className="btn-default mt-2 self-center py-1 text-xs"
          onClick={() => {
            setAutoScroll(true);
            const el = scrollRef.current;
            if (el) el.scrollTop = el.scrollHeight;
          }}
        >
          {t('logs.resume')}
        </button>
      )}
    </div>
  );
}

function StreamIndicator({ state }: { state: string }) {
  const colour =
    state === 'open' ? 'bg-ok' : state === 'connecting' ? 'bg-warn animate-pulse' : 'bg-muted';
  return (
    <span className="flex items-center gap-1.5 text-xs text-muted">
      <span className={cn('h-1.5 w-1.5 rounded-full', colour)} aria-hidden />
      {state}
    </span>
  );
}
