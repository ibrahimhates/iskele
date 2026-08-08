import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Area,
  AreaChart,
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { eventSourceURL, fetchStreamTicket } from '../../api/client';
import type { Stats } from '../../api/types';
import { formatBytes } from '../../lib/format';
import { Spinner } from '../../components/Spinner';

/** Matches the server-side ring buffer, so both agree on "recent". */
const HISTORY = 60;

export function StatsPanel({ containerID, running }: { containerID: string; running: boolean }) {
  const { t } = useTranslation();
  const [samples, setSamples] = useState<Stats[]>([]);
  const [error, setError] = useState<string | null>(null);
  const sourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    if (!running) return;
    let cancelled = false;

    async function connect() {
      try {
        const ticket = await fetchStreamTicket();
        if (cancelled) return;

        const source = new EventSource(
          eventSourceURL(`/containers/${encodeURIComponent(containerID)}/stats`, { ticket }),
        );
        sourceRef.current = source;

        source.addEventListener('stats', (event) => {
          const sample = JSON.parse((event as MessageEvent).data as string) as Stats;
          setSamples((current) => [...current, sample].slice(-HISTORY));
        });

        source.addEventListener('error', (event) => {
          const data = (event as MessageEvent).data;
          if (typeof data === 'string') {
            try {
              setError((JSON.parse(data) as { message?: string }).message ?? 'stream failed');
            } catch {
              setError('stream failed');
            }
          }
        });
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      }
    }

    void connect();

    return () => {
      cancelled = true;
      sourceRef.current?.close();
      sourceRef.current = null;
    };
  }, [containerID, running]);

  if (!running) {
    return <p className="text-sm text-muted">{t('stats.onlyRunning')}</p>;
  }

  if (error) {
    return <p className="text-sm text-danger">{error}</p>;
  }

  if (samples.length === 0) {
    return (
      <div className="flex items-center gap-2 py-8 text-sm text-muted">
        <Spinner className="h-4 w-4" />
        {t('stats.waiting')}
      </div>
    );
  }

  const latest = samples[samples.length - 1]!;
  const chartData = samples.map((sample, index) => ({
    index,
    time: new Date(sample.timestamp).toLocaleTimeString(),
    cpu: Number(sample.cpu_percent.toFixed(2)),
    memory: sample.memory_usage,
    memoryPercent: Number(sample.memory_percent.toFixed(2)),
    rx: sample.network_rx,
    tx: sample.network_tx,
    read: sample.block_read,
    write: sample.block_write,
  }));

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Metric label={t('stats.cpu')} value={`${latest.cpu_percent.toFixed(1)}%`} />
        <Metric
          label={t('stats.memory')}
          value={formatBytes(latest.memory_usage)}
          hint={
            latest.memory_limit > 0
              ? `${latest.memory_percent.toFixed(1)}% of ${formatBytes(latest.memory_limit)}`
              : undefined
          }
        />
        <Metric
          label={t('stats.network')}
          value={`↓ ${formatBytes(latest.network_rx)}`}
          hint={`↑ ${formatBytes(latest.network_tx)}`}
        />
        <Metric label={t('stats.pids')} value={String(latest.pids)} />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <ChartCard title={t('stats.cpu')}>
          <ResponsiveContainer width="100%" height={180}>
            <AreaChart data={chartData} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgb(var(--border))" />
              <XAxis dataKey="time" hide />
              <YAxis width={44} tick={{ fontSize: 11 }} stroke="rgb(var(--muted))" unit="%" />
              <Tooltip
                contentStyle={{
                  background: 'rgb(var(--surface))',
                  border: '1px solid rgb(var(--border))',
                  borderRadius: 6,
                  fontSize: 12,
                }}
              />
              <Area
                type="monotone"
                dataKey="cpu"
                stroke="rgb(var(--accent))"
                fill="rgb(var(--accent) / 0.2)"
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        </ChartCard>

        <ChartCard title={t('stats.memory')}>
          <ResponsiveContainer width="100%" height={180}>
            <AreaChart data={chartData} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgb(var(--border))" />
              <XAxis dataKey="time" hide />
              <YAxis
                width={56}
                tick={{ fontSize: 11 }}
                stroke="rgb(var(--muted))"
                tickFormatter={(value: number) => formatBytes(value)}
              />
              <Tooltip
                formatter={(value: number) => formatBytes(value)}
                contentStyle={{
                  background: 'rgb(var(--surface))',
                  border: '1px solid rgb(var(--border))',
                  borderRadius: 6,
                  fontSize: 12,
                }}
              />
              <Area
                type="monotone"
                dataKey="memory"
                stroke="rgb(var(--ok))"
                fill="rgb(var(--ok) / 0.2)"
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        </ChartCard>

        <ChartCard title={t('stats.network')}>
          <ResponsiveContainer width="100%" height={180}>
            <LineChart data={chartData} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgb(var(--border))" />
              <XAxis dataKey="time" hide />
              <YAxis
                width={56}
                tick={{ fontSize: 11 }}
                stroke="rgb(var(--muted))"
                tickFormatter={(value: number) => formatBytes(value)}
              />
              <Tooltip
                formatter={(value: number) => formatBytes(value)}
                contentStyle={{
                  background: 'rgb(var(--surface))',
                  border: '1px solid rgb(var(--border))',
                  borderRadius: 6,
                  fontSize: 12,
                }}
              />
              <Legend wrapperStyle={{ fontSize: 11 }} />
              <Line
                type="monotone"
                dataKey="rx"
                name={t('stats.rx')}
                stroke="rgb(var(--accent))"
                dot={false}
                isAnimationActive={false}
              />
              <Line
                type="monotone"
                dataKey="tx"
                name={t('stats.tx')}
                stroke="rgb(var(--warn))"
                dot={false}
                isAnimationActive={false}
              />
            </LineChart>
          </ResponsiveContainer>
        </ChartCard>

        <ChartCard title={t('stats.block')}>
          <ResponsiveContainer width="100%" height={180}>
            <LineChart data={chartData} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgb(var(--border))" />
              <XAxis dataKey="time" hide />
              <YAxis
                width={56}
                tick={{ fontSize: 11 }}
                stroke="rgb(var(--muted))"
                tickFormatter={(value: number) => formatBytes(value)}
              />
              <Tooltip
                formatter={(value: number) => formatBytes(value)}
                contentStyle={{
                  background: 'rgb(var(--surface))',
                  border: '1px solid rgb(var(--border))',
                  borderRadius: 6,
                  fontSize: 12,
                }}
              />
              <Legend wrapperStyle={{ fontSize: 11 }} />
              <Line
                type="monotone"
                dataKey="read"
                name={t('stats.read')}
                stroke="rgb(var(--accent))"
                dot={false}
                isAnimationActive={false}
              />
              <Line
                type="monotone"
                dataKey="write"
                name={t('stats.write')}
                stroke="rgb(var(--danger))"
                dot={false}
                isAnimationActive={false}
              />
            </LineChart>
          </ResponsiveContainer>
        </ChartCard>
      </div>
    </div>
  );
}

function Metric({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="card p-3">
      <div className="text-xs text-muted">{label}</div>
      <div className="mt-1 text-lg font-semibold tabular-nums">{value}</div>
      {hint && <div className="text-xs text-muted">{hint}</div>}
    </div>
  );
}

function ChartCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="card p-3">
      <h3 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted">{title}</h3>
      {children}
    </div>
  );
}
