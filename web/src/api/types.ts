// Wire types, mirroring docs/openapi.yaml.
//
// They are hand-written for now. `make gen-api` regenerates a machine-checked
// copy from the spec; keeping this file readable is worth the small overlap.

export type Role = 'admin' | 'operator' | 'viewer';

export type Permission =
  'read' | 'operate' | 'create' | 'delete' | 'build' | 'prune' | 'privileged' | 'admin';

export interface User {
  id: string;
  username: string;
  role: Role;
  totp_enabled: boolean;
  disabled: boolean;
  created_at: string;
  last_login_at?: string;
  permissions: Permission[];
  token_id?: string;
  scopes?: string[];
}

export interface Session {
  access_token: string;
  token_type: 'Bearer';
  expires_at: string;
  refresh_token: string;
  refresh_expires_at: string;
  user: User;
}

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
    details?: Record<string, unknown>;
  };
}

/**
 * The engine's health verdict, parsed out of the status line. Empty when the
 * image declares no healthcheck.
 */
export type HealthStatus = '' | 'healthy' | 'unhealthy' | 'starting';

export interface Port {
  ip?: string;
  private_port: number;
  public_port?: number;
  type: string;
}

export interface Container {
  id: string;
  name: string;
  names: string[];
  image: string;
  image_id: string;
  command: string;
  created: string;
  state: string;
  status: string;
  health?: HealthStatus;
  ports: Port[];
  labels: Record<string, string>;
  networks: string[];
  mounts: string[];
  size_rw: number;
  size_root_fs: number;
}

export interface MountPoint {
  type: string;
  name?: string;
  source: string;
  destination: string;
  driver?: string;
  mode?: string;
  rw: boolean;
  propagation?: string;
}

export interface NetworkAttachment {
  name: string;
  network_id: string;
  endpoint_id?: string;
  ip_address?: string;
  ip_prefix_len?: number;
  gateway?: string;
  mac_address?: string;
  aliases?: string[];
}

export interface ContainerDetail extends Container {
  restart_count: number;
  platform?: string;
  driver?: string;
  log_path?: string;
  path?: string;
  args?: string[];
  entrypoint?: string[];
  cmd?: string[];
  env?: string[];
  working_dir?: string;
  user?: string;
  hostname?: string;
  restart_policy?: string;
  privileged: boolean;
  started_at?: string;
  finished_at?: string;
  exit_code: number;
  oom_killed: boolean;
  pid: number;
  error?: string;
  mount_points: MountPoint[];
  network_list: NetworkAttachment[];
  health_check?: {
    status: string;
    failing_streak: number;
    last_output?: string;
  };
  port_bindings?: Record<string, Port[]>;
}

export interface Image {
  id: string;
  parent_id?: string;
  repo_tags: string[];
  repo_digests: string[];
  created: string;
  size: number;
  shared_size: number;
  labels: Record<string, string>;
  containers: number;
  dangling: boolean;
}

export interface Volume {
  name: string;
  driver: string;
  mountpoint: string;
  scope: string;
  created_at?: string;
  labels: Record<string, string>;
  options: Record<string, string>;
  size: number;
  ref_count: number;
}

export interface NetworkResource {
  id: string;
  name: string;
  driver: string;
  scope: string;
  created: string;
  internal: boolean;
  attachable: boolean;
  ingress: boolean;
  enable_ipv6: boolean;
  ipam: { subnet?: string; gateway?: string; ip_range?: string }[];
  labels: Record<string, string>;
  container_count: number;
}

export interface SystemInfo {
  server_version: string;
  api_version: string;
  name: string;
  os_type: string;
  operating_system: string;
  architecture: string;
  kernel_version: string;
  ncpu: number;
  mem_total: number;
  docker_root_dir: string;
  storage_driver: string;
  logging_driver: string;
  cgroup_driver: string;
  containers: number;
  containers_running: number;
  containers_paused: number;
  containers_stopped: number;
  images: number;
  warnings?: string[];
}

export interface DiskUsageEntry {
  count: number;
  size: number;
  reclaimable: number;
}

export interface DiskUsage {
  layers_size: number;
  images: DiskUsageEntry;
  containers: DiskUsageEntry;
  volumes: DiskUsageEntry;
  build_cache: DiskUsageEntry;
}

export interface EngineStatus {
  reachable: boolean;
  api_version?: string;
  os_type?: string;
  error?: string;
}

export interface ListResponse<T> {
  items: T[];
  total: number;
}

export interface BatchResult {
  id: string;
  ok: boolean;
  error?: string;
  code?: string;
}

export interface BatchResponse {
  action: string;
  total: number;
  succeeded: number;
  failed: number;
  results: BatchResult[];
}

export interface RedeployResult {
  old_id: string;
  new_id: string;
  image: string;
  rolled_back: boolean;
}

/** One sample from the SSE stats stream. */
export interface Stats {
  timestamp: string;
  cpu_percent: number;
  memory_usage: number;
  memory_limit: number;
  memory_percent: number;
  network_rx: number;
  network_tx: number;
  block_read: number;
  block_write: number;
  pids: number;
}

/** One frame from the WebSocket log stream. */
export interface LogFrame {
  t: 'log' | 'err' | 'eof' | 'exit';
  s?: 'stdout' | 'stderr';
  ts?: string;
  m?: string;
  code?: string;
  exit_code?: number;
}

/** One event from the SSE engine event stream. */
export interface DockerEvent {
  type: string;
  action: string;
  actor: string;
  name?: string;
  scope?: string;
  attributes?: Record<string, string>;
  time: string;
}

/** The lifecycle actions the API accepts, individually and in bulk. */
export type ContainerAction =
  'start' | 'stop' | 'restart' | 'pause' | 'unpause' | 'kill' | 'remove';
