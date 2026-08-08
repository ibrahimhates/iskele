import { api } from './client';
import type {
  BatchResponse,
  Container,
  ContainerAction,
  ContainerDetail,
  ContainerSpec,
  CreateResult,
  DiskUsage,
  EngineStatus,
  Image,
  ImageDeleted,
  ImageHistoryEntry,
  ListResponse,
  NetworkResource,
  NetworkSpec,
  PruneReport,
  RedeployResult,
  Registry,
  RegistryInput,
  Session,
  SystemInfo,
  Task,
  User,
  Volume,
  VolumeSpec,
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
  create: (spec: ContainerSpec) => api.post<CreateResult>('/containers', spec),
};

export const images = {
  list: (opts: { all?: boolean; dangling?: boolean } = {}) =>
    api.get<ListResponse<Image>>(
      `/images${query({ all: opts.all, dangling: opts.dangling === undefined ? undefined : String(opts.dangling) })}`,
    ),
  remove: (id: string, opts: { force?: boolean; noprune?: boolean } = {}) =>
    api.delete<{ deleted: ImageDeleted[] }>(
      `/images/${encodeURIComponent(id)}${query({ ...opts })}`,
    ),
  prune: (all = false) => api.post<PruneReport>(`/images/prune${query({ all })}`),
  tag: (id: string, tag: string) =>
    api.post<void>(`/images/${encodeURIComponent(id)}/tag`, { tag }),
  history: (id: string) =>
    api.get<ListResponse<ImageHistoryEntry>>(`/images/${encodeURIComponent(id)}/history`),
  inspect: (id: string) =>
    api.get<Record<string, unknown>>(`/images/${encodeURIComponent(id)}/inspect`),
};

export const volumes = {
  list: () => api.get<ListResponse<Volume>>('/volumes'),
  get: (name: string) => api.get<Volume>(`/volumes/${encodeURIComponent(name)}`),
  inspect: (name: string) =>
    api.get<Record<string, unknown>>(`/volumes/${encodeURIComponent(name)}/inspect`),
  create: (spec: VolumeSpec) => api.post<Volume>('/volumes', spec),
  remove: (name: string, force = false) =>
    api.delete<void>(`/volumes/${encodeURIComponent(name)}${query({ force })}`),
  prune: () => api.post<PruneReport>('/volumes/prune'),
};

export const networks = {
  list: () => api.get<ListResponse<NetworkResource>>('/networks'),
  get: (id: string) => api.get<NetworkResource>(`/networks/${encodeURIComponent(id)}`),
  inspect: (id: string) =>
    api.get<Record<string, unknown>>(`/networks/${encodeURIComponent(id)}/inspect`),
  create: (spec: NetworkSpec) => api.post<NetworkResource>('/networks', spec),
  remove: (id: string) => api.delete<void>(`/networks/${encodeURIComponent(id)}`),
  prune: () => api.post<PruneReport>('/networks/prune'),
  connect: (id: string, container: string, aliases: string[] = []) =>
    api.post<void>(`/networks/${encodeURIComponent(id)}/connect`, { container, aliases }),
  disconnect: (id: string, container: string, force = false) =>
    api.post<void>(`/networks/${encodeURIComponent(id)}/disconnect`, { container, force }),
};

export const registries = {
  list: () => api.get<ListResponse<Registry>>('/registries'),
  create: (input: RegistryInput) => api.post<Registry>('/registries', input),
  update: (id: string, input: RegistryInput) =>
    api.put<Registry>(`/registries/${encodeURIComponent(id)}`, input),
  remove: (id: string) => api.delete<void>(`/registries/${encodeURIComponent(id)}`),
};

export const tasks = {
  list: () => api.get<ListResponse<Task>>('/tasks'),
  get: (id: string) => api.get<Task>(`/tasks/${encodeURIComponent(id)}`),
  cancel: (id: string) => api.post<Task>(`/tasks/${encodeURIComponent(id)}/cancel`),
};

export const system = {
  ping: () => api.get<EngineStatus>('/system/ping'),
  info: () => api.get<SystemInfo>('/system/info'),
  diskUsage: () => api.get<DiskUsage>('/system/df'),
  allowedPaths: () => api.get<{ paths: string[] }>('/system/allowed-paths'),
  version: () =>
    api.get<{
      version: string;
      commit: string;
      build_date: string;
      go_version: string;
      platform: string;
    }>('/version', { anonymous: true }),
};
