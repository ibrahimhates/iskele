import { describe, expect, it } from 'vitest';

import {
  formatBytes,
  formatDuration,
  formatPort,
  formatRelative,
  formatUptime,
  shortID,
} from './format';

describe('formatBytes', () => {
  it('renders human units', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1024)).toBe('1.0 KB');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(142_000_000)).toBe('135.4 MB');
  });

  it('shows the unknown sentinel rather than a misleading zero', () => {
    // The API returns -1 when the engine did not compute the size; rendering
    // that as "0 B" would tell the operator the container is empty.
    expect(formatBytes(-1)).toBe('—');
    expect(formatBytes(-1, 'unknown')).toBe('unknown');
  });
});

describe('formatRelative', () => {
  const now = new Date('2026-01-01T12:00:00Z').getTime();

  it('picks the coarsest useful unit', () => {
    expect(formatRelative('2026-01-01T11:59:30Z', now)).toBe('30s');
    expect(formatRelative('2026-01-01T11:30:00Z', now)).toBe('30m');
    expect(formatRelative('2026-01-01T06:00:00Z', now)).toBe('6h');
    expect(formatRelative('2025-12-25T12:00:00Z', now)).toBe('7d');
  });

  it('tolerates missing and malformed input', () => {
    expect(formatRelative(undefined, now)).toBe('—');
    expect(formatRelative('not a date', now)).toBe('—');
  });
});

describe('formatPort', () => {
  it('shows a published mapping with its host side', () => {
    expect(formatPort({ ip: '0.0.0.0', public_port: 8080, private_port: 80, type: 'tcp' })).toBe(
      '8080→80/tcp',
    );
    expect(formatPort({ ip: '127.0.0.1', public_port: 8080, private_port: 80, type: 'tcp' })).toBe(
      '127.0.0.1:8080→80/tcp',
    );
  });

  it('shows an exposed-but-unpublished port without an arrow', () => {
    expect(formatPort({ private_port: 5432, type: 'tcp' })).toBe('5432/tcp');
  });
});

describe('shortID', () => {
  it('trims to the 12 characters Docker shows', () => {
    expect(shortID('c1000000000000000000000000000000')).toBe('c10000000000');
    expect(shortID('sha256:abcdef1234567890')).toBe('abcdef123456');
  });
});

describe('formatDuration', () => {
  it('reports a short build in seconds', () => {
    expect(formatDuration(4200)).toBe('4s');
  });

  it('reports a longer one in minutes and seconds', () => {
    expect(formatDuration(3 * 60_000 + 7_000)).toBe('3m 7s');
  });

  it('drops to hours and minutes once a build runs that long', () => {
    expect(formatDuration(2 * 3_600_000 + 5 * 60_000)).toBe('2h 5m');
  });

  it('has nothing to say about a negative duration', () => {
    expect(formatDuration(-1)).toBe('—');
  });
});

describe('formatUptime', () => {
  it('coarsens as the machine stays up', () => {
    expect(formatUptime(0)).toBe('0s');
    expect(formatUptime(45)).toBe('45s');
    expect(formatUptime(60)).toBe('1m');
    expect(formatUptime(59 * 60)).toBe('59m');
    expect(formatUptime(3600)).toBe('1h 0m');
    expect(formatUptime(3600 * 5 + 60 * 13)).toBe('5h 13m');
    expect(formatUptime(86_400)).toBe('1d 0h');
    expect(formatUptime(86_400 * 26 + 3600 * 7)).toBe('26d 7h');
  });

  it('labels a reading it cannot make', () => {
    expect(formatUptime(-1)).toBe('—');
    expect(formatUptime(Number.NaN, 'unknown')).toBe('unknown');
  });
});
