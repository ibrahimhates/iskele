/** Formats a byte count for humans. -1 means the engine did not compute it. */
export function formatBytes(bytes: number, unknownLabel = '—'): string {
  if (bytes < 0) return unknownLabel;
  if (bytes === 0) return '0 B';

  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, exponent);
  return `${value.toFixed(exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}

/** Formats an elapsed duration as the coarsest useful unit. */
export function formatRelative(iso: string | undefined, now = Date.now()): string {
  if (!iso) return '—';
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return '—';

  const seconds = Math.max(0, Math.floor((now - then) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d`;
  const months = Math.floor(days / 30);
  if (months < 12) return `${months}mo`;
  return `${Math.floor(months / 12)}y`;
}

/** Renders a port mapping the way `docker ps` does. */
export function formatPort(port: {
  ip?: string;
  public_port?: number;
  private_port: number;
  type: string;
}): string {
  if (!port.public_port) return `${port.private_port}/${port.type}`;
  const host = port.ip && port.ip !== '0.0.0.0' ? `${port.ip}:` : '';
  return `${host}${port.public_port}→${port.private_port}/${port.type}`;
}

/** Shortens a container or image ID to the 12 characters Docker displays. */
export function shortID(id: string): string {
  const withoutPrefix = id.startsWith('sha256:') ? id.slice(7) : id;
  return withoutPrefix.slice(0, 12);
}

/** Formats a timestamp for display, in the viewer's locale. */
export function formatTime(iso: string | undefined): string {
  if (!iso) return '—';
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString();
}
