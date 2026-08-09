import type { ApiErrorBody, Session } from './types';

export const API_PREFIX = '/api/v1';

/**
 * An error carrying the server's stable code, so callers can branch on the
 * cause rather than on message text.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: Record<string, unknown>;

  constructor(status: number, code: string, message: string, details?: Record<string, unknown>) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.details = details;
  }

  /** The token expired; a refresh may fix it. */
  get isExpired(): boolean {
    return this.code === 'TOKEN_EXPIRED';
  }

  /** The caller is not signed in at all. */
  get isUnauthenticated(): boolean {
    return this.status === 401;
  }

  /** The installation still needs bootstrapping. */
  get isNotInitialized(): boolean {
    return this.code === 'NOT_INITIALIZED';
  }

  /** The Docker daemon could not be reached. */
  get isDockerUnavailable(): boolean {
    return this.code === 'DOCKER_UNAVAILABLE';
  }
}

/** Tokens and the hooks the client needs to keep them fresh. */
export interface AuthBridge {
  getAccessToken(): string | null;
  getRefreshToken(): string | null;
  onRefreshed(session: Session): void;
  onSignedOut(): void;
}

let bridge: AuthBridge | null = null;

/** Connects the client to the auth store. Called once at start-up. */
export function connectAuth(next: AuthBridge): void {
  bridge = next;
}

/**
 * A single in-flight refresh, shared by every request that hits a 401.
 *
 * Without this, a page that fires six queries at once on an expired token
 * would spend five refresh tokens and — because refresh rotates — invalidate
 * its own session.
 */
let refreshInFlight: Promise<boolean> | null = null;

async function refreshAccessToken(): Promise<boolean> {
  if (!bridge) return false;

  const refreshToken = bridge.getRefreshToken();
  if (!refreshToken) return false;

  refreshInFlight ??= (async () => {
    try {
      const response = await fetch(`${API_PREFIX}/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refreshToken }),
      });
      if (!response.ok) {
        bridge?.onSignedOut();
        return false;
      }
      const session = (await response.json()) as Session;
      bridge?.onRefreshed(session);
      return true;
    } catch {
      // A network failure is not a sign-out: the token may still be good once
      // the connection returns.
      return false;
    } finally {
      refreshInFlight = null;
    }
  })();

  return refreshInFlight;
}

export interface RequestOptions extends Omit<RequestInit, 'body'> {
  body?: unknown;
  /** Skips the Authorization header, for the pre-sign-in endpoints. */
  anonymous?: boolean;
  /** Internal: prevents a refreshed request from refreshing again. */
  retried?: boolean;
  /**
   * What the endpoint sends back. A build log is the operator's own output
   * verbatim, not JSON, and parsing it would only destroy it.
   */
  expect?: 'json' | 'text';
}

/**
 * Issues an API request, refreshing the access token once on expiry.
 */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { body, anonymous, retried, expect, headers, ...rest } = options;

  const finalHeaders = new Headers(headers);
  if (body !== undefined) {
    finalHeaders.set('Content-Type', 'application/json');
  }

  const token = anonymous ? null : bridge?.getAccessToken();
  if (token) {
    finalHeaders.set('Authorization', `Bearer ${token}`);
  }

  const response = await fetch(`${API_PREFIX}${path}`, {
    ...rest,
    headers: finalHeaders,
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  if (response.status === 204) {
    return undefined as T;
  }

  if (response.ok) {
    const text = await response.text();
    if (expect === 'text') {
      return text as T;
    }
    return (text ? JSON.parse(text) : undefined) as T;
  }

  const error = await toApiError(response);

  // One retry, and only for an expired token: retrying a 403 or a 500 would
  // just double the load on a server that already said no.
  if (!anonymous && !retried && (error.isExpired || error.isUnauthenticated)) {
    if (await refreshAccessToken()) {
      return request<T>(path, { ...options, retried: true });
    }
    bridge?.onSignedOut();
  }

  throw error;
}

async function toApiError(response: Response): Promise<ApiError> {
  let code = `HTTP_${response.status}`;
  let message = response.statusText || 'request failed';
  let details: Record<string, unknown> | undefined;

  try {
    const parsed = (await response.json()) as ApiErrorBody;
    if (parsed?.error) {
      code = parsed.error.code || code;
      message = parsed.error.message || message;
      details = parsed.error.details;
    }
  } catch {
    // A non-JSON error body (a proxy's HTML page, say) leaves the defaults.
  }

  return new ApiError(response.status, code, message, details);
}

export const api = {
  get: <T>(path: string, options?: RequestOptions) =>
    request<T>(path, { ...options, method: 'GET' }),
  getText: (path: string, options?: RequestOptions) =>
    request<string>(path, { ...options, method: 'GET', expect: 'text' }),
  post: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>(path, { ...options, method: 'POST', body }),
  put: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>(path, { ...options, method: 'PUT', body }),
  delete: <T>(path: string, options?: RequestOptions) =>
    request<T>(path, { ...options, method: 'DELETE' }),
};

/**
 * Fetches a single-use ticket for a WebSocket or SSE connection.
 *
 * Browsers cannot set an Authorization header on those, so the credential has
 * to travel in the URL; a ticket that dies on first use is what makes that
 * acceptable.
 */
export async function fetchStreamTicket(): Promise<string> {
  const { ticket } = await api.post<{ ticket: string; expires_in: number }>('/auth/ws-ticket', {});
  return ticket;
}

/** Builds an absolute WebSocket URL for a streaming endpoint. */
export function websocketURL(path: string, params: Record<string, string>): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = new URL(`${protocol}//${window.location.host}${API_PREFIX}${path}`);
  for (const [key, value] of Object.entries(params)) {
    url.searchParams.set(key, value);
  }
  return url.toString();
}

/** Builds an absolute URL for an SSE endpoint. */
export function eventSourceURL(path: string, params: Record<string, string>): string {
  const url = new URL(`${window.location.origin}${API_PREFIX}${path}`);
  for (const [key, value] of Object.entries(params)) {
    url.searchParams.set(key, value);
  }
  return url.toString();
}
