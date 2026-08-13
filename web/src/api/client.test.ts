import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError, api, connectAuth } from './client';

const originalFetch = global.fetch;

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('api client', () => {
  let accessToken: string | null;
  let refreshToken: string | null;
  let signedOut: boolean;

  beforeEach(() => {
    accessToken = 'access-1';
    refreshToken = 'refresh-1';
    signedOut = false;

    connectAuth({
      getAccessToken: () => accessToken,
      getRefreshToken: () => refreshToken,
      onRefreshed: (session) => {
        accessToken = session.access_token;
        refreshToken = session.refresh_token;
      },
      onSignedOut: () => {
        signedOut = true;
        accessToken = null;
      },
    });
  });

  afterEach(() => {
    global.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it('sends the bearer token', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
      jsonResponse(200, { ok: true }),
    );
    global.fetch = fetchMock as unknown as typeof fetch;

    await api.get('/containers');

    const [, init] = fetchMock.mock.calls[0]!;
    const headers = new Headers(init?.headers);
    expect(headers.get('Authorization')).toBe('Bearer access-1');
  });

  it('omits the token for anonymous requests', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
      jsonResponse(200, { initialized: false }),
    );
    global.fetch = fetchMock as unknown as typeof fetch;

    await api.get('/auth/status', { anonymous: true });

    const [, init] = fetchMock.mock.calls[0]!;
    const headers = new Headers(init?.headers);
    expect(headers.get('Authorization')).toBeNull();
  });

  it('refreshes once on an expired token and replays the request', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse(401, { error: { code: 'TOKEN_EXPIRED', message: 'expired' } }),
      )
      .mockResolvedValueOnce(
        jsonResponse(200, {
          access_token: 'access-2',
          refresh_token: 'refresh-2',
          token_type: 'Bearer',
          expires_at: '',
          refresh_expires_at: '',
          user: {},
        }),
      )
      .mockResolvedValueOnce(jsonResponse(200, { items: [], total: 0 }));
    global.fetch = fetchMock as unknown as typeof fetch;

    const result = await api.get<{ total: number }>('/containers');

    expect(result.total).toBe(0);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    // The replay must carry the new token, not the expired one.
    const [, replayInit] = fetchMock.mock.calls[2]!;
    expect(new Headers((replayInit as RequestInit).headers).get('Authorization')).toBe(
      'Bearer access-2',
    );
  });

  it('signs out when the refresh is rejected', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: 'UNAUTHORIZED', message: 'no' } }))
      .mockResolvedValueOnce(jsonResponse(401, { error: { code: 'UNAUTHORIZED', message: 'no' } }));
    global.fetch = fetchMock as unknown as typeof fetch;

    await expect(api.get('/containers')).rejects.toBeInstanceOf(ApiError);
    expect(signedOut).toBe(true);
  });

  it('does not retry a 403', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse(403, { error: { code: 'FORBIDDEN', message: 'role viewer may not operate' } }),
    );
    global.fetch = fetchMock as unknown as typeof fetch;

    await expect(api.post('/containers/x/start')).rejects.toMatchObject({ code: 'FORBIDDEN' });
    // Retrying an authorization failure only doubles the load.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('surfaces the server code and message', async () => {
    global.fetch = vi.fn(async () =>
      jsonResponse(503, {
        error: {
          code: 'DOCKER_UNAVAILABLE',
          message: 'not connected to unix:///var/run/docker.sock',
        },
      }),
    ) as unknown as typeof fetch;

    const error = await api.get('/containers').catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).isDockerUnavailable).toBe(true);
    // The engine's own wording is what tells the operator what to fix.
    expect((error as ApiError).message).toContain('docker.sock');
  });

  it('handles a 204 with no body', async () => {
    global.fetch = vi.fn(
      async () => new Response(null, { status: 204 }),
    ) as unknown as typeof fetch;

    await expect(api.delete('/containers/x')).resolves.toBeUndefined();
  });

  it('falls back cleanly when the error body is not JSON', async () => {
    global.fetch = vi.fn(
      async () => new Response('<html>502 Bad Gateway</html>', { status: 502 }),
    ) as unknown as typeof fetch;

    const error = (await api.get('/containers').catch((e: unknown) => e)) as ApiError;

    expect(error.status).toBe(502);
    expect(error.code).toBe('HTTP_502');
  });
});
