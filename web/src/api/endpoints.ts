import { api } from './client';
import type {
  BatchResponse,
  Container,
  ContainerAction,
  ContainerDetail,
  DiskUsage,
  EngineStatus,
  Image,
  ListResponse,
  NetworkResource,
  RedeployResult,
  Session,
  SystemInfo,
  User,
  Volume,
} from './types';

export interface ContainerFilters {
  all?: boolean;
  status?: string[];
  label?: string[];
  name?: string;
}

function query(params: Record<string, string | boolean | string[] | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '' || value === false) continue;
    if (Array.isArray(value)) {
      for (const item of value) search.append(key, item);
      continue;
    }
    search.set(key, String(value));
  }
  const encoded = search.toString();
  return encoded ? `?${encoded}` : '';
}

export const auth = {
  status: () => api.get<{ initialized: boolean }>('/auth/status', { anonymous: true }),
  bootstrap: (username: string, password: string) =>
    api.post<Session>('/auth/bootstrap', { username, password }, { anonymous: true }),
  login: (username: string, password: string) =>
    api.post<Session>('/auth/login', { username, password }, { anonymous: true }),
  logout: (refreshToken: string) => api.post<void>('/auth/logout', { refresh_token: refreshToken }),
  me: () => api.get<User>('/auth/me'),
};

export const containers = {
  list: (filters: ContainerFilters = {}) =>
    api.get<ListResponse<Container>>(`/containers${query({ ...filters })}`),
  get: (id: string) => api.get<ContainerDetail>(`/containers/${encodeURIComponent(id)}`),
  inspect: (id: string) =>
    api.get<Record<string, unknown>>(`/containers/${encodeURIComponent(id)}/inspect`),
  action: (
    id: string,
    action: Exclude<ContainerAction, 'remove'>,
    params: Record<string, string> = {},
  ) =>
    api.post<{ id: string; action: string; status: string }>(
      `/containers/${encodeURIComponent(id)}/${action}${query(params)}`,
    ),
  rename: (id: string, name: string) =>
    api.post<{ id: string }>(`/containers/${encodeURIComponent(id)}/rename`, { name }),
  redeploy: (id: string) =>
    api.post<RedeployResult>(`/containers/${encodeURIComponent(id)}/redeploy`),
  remove: (id: string, opts: { force?: boolean; volumes?: boolean } = {}) =>
    api.delete<void>(`/containers/${encodeURIComponent(id)}${query({ ...opts })}`),
  batch: (ids: string[], action: ContainerAction) =>
    api.post<BatchResponse>('/containers/batch', { ids, action }),
};

export const images = {
  list: (opts: { all?: boolean; dangling?: boolean } = {}) =>
    api.get<ListResponse<Image>>(
      `/images${query({ all: opts.all, dangling: opts.dangling === undefined ? undefined : String(opts.dangling) })}`,
    ),
};

export const volumes = {
  list: () => api.get<ListResponse<Volume>>('/volumes'),
};

export const networks = {
  list: () => api.get<ListResponse<NetworkResource>>('/networks'),
};

export const system = {
  ping: () => api.get<EngineStatus>('/system/ping'),
  info: () => api.get<SystemInfo>('/system/info'),
  diskUsage: () => api.get<DiskUsage>('/system/df'),
  version: () =>
    api.get<{
      version: string;
      commit: string;
      build_date: string;
      go_version: string;
      platform: string;
    }>('/version', { anonymous: true }),
};
