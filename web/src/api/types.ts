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

// --- M5: container creation, resource management, registries, tasks ---

/** One environment entry. Key and value stay apart so a value with "=" survives. */
export interface EnvVar {
  key: string;
  value: string;
}

export interface PortMapping {
  host_ip?: string;
  host_port?: string;
  container_port: number;
  protocol?: 'tcp' | 'udp' | 'sctp';
}

export type MountType = 'bind' | 'volume' | 'tmpfs';

export interface MountSpec {
  type: MountType;
  source?: string;
  destination: string;
  read_only?: boolean;
  tmpfs_size?: number;
  bind_propagation?: string;
  create_host_path?: boolean;
}

export type RestartPolicyName = 'no' | 'always' | 'unless-stopped' | 'on-failure';

export interface RestartPolicySpec {
  name?: RestartPolicyName;
  max_retries?: number;
}

export interface ResourceSpec {
  cpus?: number;
  cpu_shares?: number;
  cpuset_cpus?: string;
  memory?: number;
  memory_reservation?: number;
  memory_swap?: number;
  pids_limit?: number;
  shm_size?: number;
}

export interface ContainerNetworkSpec {
  name?: string;
  aliases?: string[];
  ipv4_address?: string;
  ipv6_address?: string;
  extra_hosts?: string[];
  dns?: string[];
  dns_search?: string[];
  dns_options?: string[];
  mac_address?: string;
}

export interface HealthSpec {
  test?: string[];
  interval?: string;
  timeout?: string;
  start_period?: string;
  retries?: number;
  disable?: boolean;
}

export interface LoggingSpec {
  driver?: string;
  options?: Record<string, string>;
}

/** Every field here needs the `privileged` permission. */
export interface SecuritySpec {
  privileged?: boolean;
  cap_add?: string[];
  cap_drop?: string[];
  security_opt?: string[];
  devices?: string[];
  read_only_root_fs?: boolean;
  sysctls?: Record<string, string>;
}

export type PullPolicy = 'missing' | 'always' | 'never';

/** What the create wizard submits. */
export interface ContainerSpec {
  name?: string;
  image: string;
  pull_policy?: PullPolicy;
  command?: string[];
  entrypoint?: string[];
  working_dir?: string;
  user?: string;
  hostname?: string;
  domain_name?: string;
  tty?: boolean;
  open_stdin?: boolean;
  env?: EnvVar[];
  labels?: Record<string, string>;
  ports?: PortMapping[];
  mounts?: MountSpec[];
  restart_policy?: RestartPolicySpec;
  resources?: ResourceSpec;
  network?: ContainerNetworkSpec;
  health_check?: HealthSpec;
  logging?: LoggingSpec;
  security?: SecuritySpec;
  auto_remove?: boolean;
  init?: boolean;
  start?: boolean;
}

export interface CreateResult {
  id: string;
  name: string;
  image: string;
  started: boolean;
}

export interface PruneReport {
  deleted: string[];
  space_reclaimed: number;
}

export interface ImageDeleted {
  deleted?: string;
  untagged?: string;
}

export interface ImageHistoryEntry {
  id: string;
  created: string;
  created_by: string;
  size: number;
  comment?: string;
  tags: string[];
}

export interface VolumeSpec {
  name?: string;
  driver?: string;
  driver_opts?: Record<string, string>;
  labels?: Record<string, string>;
}

export interface NetworkSpec {
  name: string;
  driver?: string;
  internal?: boolean;
  attachable?: boolean;
  enable_ipv6?: boolean;
  ipam?: { subnet?: string; gateway?: string; ip_range?: string }[];
  options?: Record<string, string>;
  labels?: Record<string, string>;
}

export interface Registry {
  id: string;
  name: string;
  server: string;
  username: string;
  email?: string;
  has_password: boolean;
  created_at: string;
  updated_at: string;
  last_used_at?: string;
}

export interface RegistryInput {
  name: string;
  server: string;
  username?: string;
  password?: string;
  email?: string;
}

export type TaskState = 'running' | 'succeeded' | 'failed' | 'canceled';

export interface Task {
  id: string;
  kind: string;
  target: string;
  state: TaskState;
  /** 0..100, or -1 while nothing can be measured. */
  progress: number;
  message?: string;
  error?: string;
  username?: string;
  started_at: string;
  finished_at?: string;
  cancelable: boolean;
}

/** One line of an image pull, with the overall percentage the server computes. */
export interface PullProgress {
  id?: string;
  status: string;
  current?: number;
  total?: number;
  error?: string;
  percent: number;
}

/** One entry in a browsed host directory. */
export interface DirEntry {
  name: string;
  /** The absolute path, so no caller has to join paths itself. */
  path: string;
  is_dir: boolean;
  size: number;
  mod_time: string;
  /**
   * The entry is a symlink. It is resolved to decide `is_dir`, but browsing
   * into one that leaves the whitelist is still refused.
   */
  symlink?: boolean;
}

/** The result of browsing one whitelisted host directory. */
export interface DirListing {
  /** The directory listed. Empty when the roots were returned. */
  path: string;
  /** The directory above, absent at a root — which is what stops a walk out. */
  parent?: string;
  entries: DirEntry[];
  /** The directory held more entries than the server's cap. */
  truncated?: boolean;
  /** Dockerfile-looking files here, so the form can offer them. */
  dockerfiles?: string[];
  /** The whitelist, so the picker can always get back to a root. */
  allowed_roots: string[];
}

export type BuildStatus = 'running' | 'success' | 'failed' | 'canceled';

/** One image build started from the panel. */
export interface Build {
  id: string;
  user_id?: string;
  username?: string;
  context_dir: string;
  /** Relative to the context directory. */
  dockerfile: string;
  tags: string[];
  target?: string;
  platform?: string;
  no_cache: boolean;
  pull: boolean;
  status: BuildStatus;
  image_id?: string;
  error?: string;
  /** Files sent to the engine, after `.dockerignore`. */
  context_files: number;
  context_bytes: number;
  started_at: string;
  finished_at?: string;
  /** How long it ran, or how long it has been running. */
  duration_ms: number;
  /** The output is still archived and can be replayed. */
  log_archived: boolean;
}

/** What the build form asks for. */
export interface BuildRequest {
  context: string;
  dockerfile?: string;
  tags: string[];
  buildArgs: Record<string, string>;
  labels: Record<string, string>;
  target?: string;
  platform?: string;
  noCache: boolean;
  pull: boolean;
}

/** One JSON text frame on the build WebSocket. */
export interface BuildFrame {
  t: 'build' | 'log' | 'status' | 'done' | 'err';
  id?: string;
  line?: string;
  step?: number;
  total_steps?: number;
  status?: string;
  layer_id?: string;
  current?: number;
  total?: number;
  image_id?: string;
  m?: string;
  code?: string;
}

export type StackSource = 'editor' | 'file' | 'git';

export type StackStatus = 'created' | 'deploying' | 'deployed' | 'failed' | 'stopped';

/** One compose project Iskele manages. */
export interface Stack {
  id: string;
  name: string;
  source: StackSource;
  path?: string;
  git_url?: string;
  git_ref?: string;
  git_commit?: string;
  compose: string;
  /** Present only when a single stack is read; listings withhold it. */
  env?: string;
  working_dir?: string;
  /** What the last deploy did — not what the containers are doing now. */
  status: StackStatus;
  last_error?: string;
  last_deployed_at?: string;
  created_by?: string;
  created_by_id?: string;
  created_at: string;
  updated_at: string;
}

/** What the create and edit forms submit. */
export interface StackInput {
  name?: string;
  source: StackSource;
  compose?: string;
  env?: string;
  path?: string;
  git_url?: string;
  git_ref?: string;
}

/** A compose field Iskele read but will not act on. */
export interface ComposeWarning {
  service?: string;
  field: string;
  message: string;
}

/** One reason a stack cannot be deployed. */
export interface StackProblem {
  service: string;
  field: string;
  message: string;
}

/** One service and the containers it currently has. */
export interface StackServiceStatus {
  name: string;
  /** What the compose file asks for. */
  replicas: number;
  /** How many containers are actually up. */
  running: number;
  image?: string;
  ports?: Port[];
  containers: Container[];
  /** A container is running a configuration the compose file no longer asks for. */
  drifted?: boolean;
}

export interface StackDetail extends Stack {
  services: StackServiceStatus[];
  warnings: ComposeWarning[];
  /** Set when the stored compose file no longer parses. */
  parse_error?: string;
  /** Set when the daemon could not be reached; the definition still arrives. */
  engine_error?: string;
}

export interface StackValidation {
  valid: boolean;
  /** A file that would not parse at all. */
  error?: string;
  services?: string[];
  warnings: ComposeWarning[];
  problems: StackProblem[];
}

export type ChangeKind = 'added' | 'removed' | 'modified';

export interface StackServiceChange {
  service: string;
  kind: ChangeKind;
  /** What changed, in compose's own vocabulary. */
  fields?: string[];
  recreates: boolean;
}

export interface StackResourceChange {
  name: string;
  kind: ChangeKind;
}

export interface StackDiff {
  services: StackServiceChange[];
  networks: StackResourceChange[];
  volumes: StackResourceChange[];
  warnings: ComposeWarning[];
}

/** One line of a deploy's progress. */
export interface StackEvent {
  kind: 'step' | 'log' | 'warn' | 'done';
  service?: string;
  message: string;
  container?: string;
}

/** What a lifecycle action touched. */
export interface StackActionResult {
  containers: string[];
  networks?: string[];
  volumes?: string[];
  /** What could not be done, so a partial result stays legible. */
  failed?: string[];
}

/** One JSON text frame on the stack log WebSocket. */
export interface StackLogFrame {
  t: 'log' | 'err' | 'eof';
  service?: string;
  container?: string;
  s?: 'stdout' | 'stderr';
  ts?: string;
  m?: string;
  code?: string;
}

/** A compose project running here that Iskele has no record of. */
export interface DiscoveredStack {
  name: string;
  services: string[];
  containers: number;
  running: number;
  config_file?: string;
  working_dir?: string;
  importable: boolean;
  reason?: string;
  created_at?: string;
}
