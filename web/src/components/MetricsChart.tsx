import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  Legend,
} from 'recharts';
import { getAppMetrics } from '../api/client';
import type { MetricsResponse } from '../types';
import { Skeleton } from './ui/Skeleton';

// Historical metrics chart for a single app metric (CPU, memory, etc.).
// Backend returns one series per pod; we reshape into recharts' wide-format
// rows ({ t, podA, podB, ... }) so each pod renders as one Line.

export type MetricKind = 'cpu' | 'memory' | 'network_in' | 'network_out' | 'restarts';

interface Props {
  appId: string;
  metric: MetricKind;
  title: string;
  fromUnix: number;
  toUnix: number;
  // Y-axis formatter; defaults sensibly per metric type below.
  formatY?: (n: number) => string;
}

const defaultFormatters: Record<MetricKind, (n: number) => string> = {
  cpu: (n) => `${n.toFixed(2)} cores`,
  memory: (n) => formatBytes(n),
  network_in: (n) => `${formatBytes(n)}/s`,
  network_out: (n) => `${formatBytes(n)}/s`,
  restarts: (n) => `${n.toFixed(2)}/s`,
};

export function MetricsChart({ appId, metric, title, fromUnix, toUnix, formatY }: Props) {
  const fmt = formatY ?? defaultFormatters[metric];

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['metrics', appId, metric, fromUnix, toUnix],
    queryFn: () => getAppMetrics(appId, metric, fromUnix, toUnix),
    // Auto-refresh every 30s when looking at recent data — windows ending
    // in the last 5 min get live updates. Older ranges are static.
    refetchInterval: toUnix * 1000 > Date.now() - 5 * 60_000 ? 30_000 : false,
  });

  const { rows, podKeys } = useMemo(() => reshapeForRecharts(data), [data]);

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <h4 className="text-sm font-medium text-text-primary">{title}</h4>
        {data && (
          <span className="text-xs text-text-muted">
            step {data.step_seconds}s · {data.series.length} pod{data.series.length === 1 ? '' : 's'}
          </span>
        )}
      </div>
      <div className="h-64 bg-surface-hover rounded-lg p-2">
        {isLoading ? (
          <div className="flex items-center justify-center h-full">
            <Skeleton width={32} height={32} rounded="full" />
          </div>
        ) : isError ? (
          <div className="flex items-center justify-center h-full text-sm text-error px-4 text-center">
            {(error as Error)?.message || 'Failed to load metrics'}
          </div>
        ) : rows.length === 0 ? (
          <div className="flex items-center justify-center h-full text-sm text-text-secondary">
            No data for this range
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={rows} margin={{ top: 8, right: 16, bottom: 8, left: 8 }}>
              <CartesianGrid strokeDasharray="3 3" strokeOpacity={0.2} />
              <XAxis
                dataKey="t"
                tickFormatter={(t) => formatTime(t, toUnix - fromUnix)}
                stroke="currentColor"
                style={{ fontSize: 11 }}
                minTickGap={50}
              />
              <YAxis
                tickFormatter={fmt}
                stroke="currentColor"
                style={{ fontSize: 11 }}
                width={70}
              />
              <Tooltip
                labelFormatter={(t) => new Date((t as number) * 1000).toLocaleString()}
                formatter={(v) => (typeof v === 'number' ? fmt(v) : String(v))}
                contentStyle={{
                  background: 'var(--color-surface, #fff)',
                  border: '1px solid var(--color-border, #ddd)',
                  borderRadius: 6,
                  fontSize: 12,
                }}
              />
              {podKeys.length <= 6 && <Legend wrapperStyle={{ fontSize: 11 }} />}
              {podKeys.map((pod, i) => (
                <Line
                  key={pod}
                  type="monotone"
                  dataKey={pod}
                  stroke={lineColor(i)}
                  strokeWidth={1.5}
                  dot={false}
                  isAnimationActive={false}
                  connectNulls
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
}

// reshapeForRecharts pivots backend's per-series-per-pod layout into one row
// per timestamp with each pod as a column. Recharts' LineChart wants this
// "wide" shape so it can co-plot lines that share the same X axis.
function reshapeForRecharts(data: MetricsResponse | undefined): {
  rows: Array<Record<string, number>>;
  podKeys: string[];
} {
  if (!data || data.series.length === 0) return { rows: [], podKeys: [] };

  const podKeys = data.series.map((s, i) => s.labels.pod || `series-${i}`);
  // Use a Map keyed by timestamp so we can interleave irregular per-pod
  // sampling (a pod that started halfway through the range will skip points
  // before its birth, which Prometheus emits as missing).
  const byT = new Map<number, Record<string, number>>();
  data.series.forEach((s, i) => {
    const key = podKeys[i];
    s.timestamps.forEach((t, j) => {
      let row = byT.get(t);
      if (!row) {
        row = { t };
        byT.set(t, row);
      }
      row[key] = s.values[j];
    });
  });
  const rows = Array.from(byT.values()).sort((a, b) => a.t - b.t);
  return { rows, podKeys };
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n.toFixed(0)}B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)}KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)}MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)}GB`;
}

function formatTime(t: number, rangeSeconds: number): string {
  const d = new Date(t * 1000);
  // Short format for short ranges (HH:MM), longer for multi-day windows.
  if (rangeSeconds <= 6 * 3600) {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }
  if (rangeSeconds <= 7 * 24 * 3600) {
    return d.toLocaleString([], { weekday: 'short', hour: '2-digit', minute: '2-digit' });
  }
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
}

// Distinct colors for up to ~10 pods; cycles after that. Picked for decent
// contrast on both light and dark surfaces.
const palette = [
  '#7B56FB', '#4F9F6F', '#E8744F', '#3B8FCC', '#C76B9E',
  '#7E8B95', '#D4A574', '#5F7E8F', '#B07AA1', '#8C9DA8',
];
function lineColor(i: number): string {
  return palette[i % palette.length];
}
