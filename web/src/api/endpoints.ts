import { api } from './client';
import type {
  BatchResponse,
  Build,
  BuildStatus,
  Container,
  ContainerAction,
  ContainerDetail,
  ContainerSpec,
  CreateResult,
  DirListing,
  DiscoveredStack,
  DiskUsage,
  EngineStatus,
  HostReport,
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
  CatalogResponse,
  Session,
  Stack,
  StackActionResult,
  StackDetail,
  StackDiff,
  StackInput,
  StackValidation,
  SystemInfo,
  Task,
  Template,
  TemplateDeploy,
  TemplateDeployResult,
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

export const fs = {
  /** Lists a whitelisted host directory. An empty path returns the roots. */
  browse: (path: string) => api.get<DirListing>(`/fs/browse${query({ path })}`),
};

export const builds = {
  list: (opts: { status?: BuildStatus; limit?: number } = {}) =>
    api.get<ListResponse<Build>>(
      `/builds${query({ status: opts.status, limit: opts.limit ? String(opts.limit) : undefined })}`,
    ),
  get: (id: string) => api.get<Build>(`/builds/${encodeURIComponent(id)}`),
  /** The build's output verbatim; plain text, not JSON. */
  log: (id: string) => api.getText(`/builds/${encodeURIComponent(id)}/log`),
  cancel: (id: string) => api.post<Build>(`/builds/${encodeURIComponent(id)}/cancel`),
};

export const stacks = {
  list: () => api.get<ListResponse<Stack>>('/stacks'),
  get: (id: string) => api.get<StackDetail>(`/stacks/${encodeURIComponent(id)}`),
  create: (input: StackInput) => api.post<Stack>('/stacks', input),
  update: (id: string, input: StackInput) =>
    api.put<Stack>(`/stacks/${encodeURIComponent(id)}`, input),
  remove: (id: string) => api.delete<void>(`/stacks/${encodeURIComponent(id)}`),

  /** Checks a compose file without saving it; an invalid file is still a 200. */
  validate: (input: StackInput) => api.post<StackValidation>('/stacks/validate', input),
  diff: (id: string, input: StackInput) =>
    api.post<StackDiff>(`/stacks/${encodeURIComponent(id)}/diff`, input),

  down: (id: string, opts: { volumes?: boolean; networks?: boolean } = {}) =>
    api.post<StackActionResult>(
      // Serialized as strings on purpose: `query` drops a `false`, and
      // `networks=false` is exactly how an operator asks to keep them.
      `/stacks/${encodeURIComponent(id)}/down${query({
        volumes: opts.volumes === undefined ? undefined : String(opts.volumes),
        networks: opts.networks === undefined ? undefined : String(opts.networks),
      })}`,
    ),
  act: (id: string, action: 'stop' | 'start' | 'restart') =>
    api.post<StackActionResult>(`/stacks/${encodeURIComponent(id)}/${action}`),

  discovered: () => api.get<ListResponse<DiscoveredStack>>('/stacks/discovered'),
  import: (name: string) => api.post<Stack>('/stacks/import', { name }),
};

export const catalog = {
  list: () => api.get<CatalogResponse>('/templates'),
  get: (id: string) => api.get<Template>(`/templates/${encodeURIComponent(id)}`),
  deploy: (id: string, input: TemplateDeploy) =>
    api.post<TemplateDeployResult>(`/templates/${encodeURIComponent(id)}/deploy`, input),
  /** Generated on the server: the browser is the wrong place to make a secret. */
  secret: (length?: number) =>
    api.post<{ secret: string }>(
      `/templates/secret${query({ length: length ? String(length) : undefined })}`,
    ),
};

export const system = {
  ping: () => api.get<EngineStatus>('/system/ping'),
  info: () => api.get<SystemInfo>('/system/info'),
  diskUsage: () => api.get<DiskUsage>('/system/df'),
  /** Host CPU/RAM/disk plus the daemon's own uptime. Never fails on a down engine. */
  host: () => api.get<HostReport>('/system/host'),
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
