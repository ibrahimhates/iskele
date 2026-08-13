import type { ContainerSpec } from '../../api/types';

/**
 * Renders a spec as the `docker run` command that would produce it.
 *
 * The preview is what makes the wizard trustworthy: an operator who knows
 * Docker can read one line and see exactly what they are about to create,
 * including the flags a form makes easy to set without noticing.
 *
 * It is a rendering of the same object the API receives, not a second
 * description of it — so it cannot drift from what actually gets sent.
 */
export function dockerRunCommand(spec: ContainerSpec): string {
  const args: string[] = ['docker', 'run'];

  if (spec.start === false || spec.start === undefined) {
    // `docker create` is the honest equivalent of a spec that does not start.
    args[1] = 'create';
  } else {
    args.push('-d');
  }

  if (spec.name) args.push('--name', quote(spec.name));
  if (spec.hostname) args.push('--hostname', quote(spec.hostname));
  if (spec.domain_name) args.push('--domainname', quote(spec.domain_name));
  if (spec.user) args.push('--user', quote(spec.user));
  if (spec.working_dir) args.push('--workdir', quote(spec.working_dir));
  if (spec.tty) args.push('--tty');
  if (spec.open_stdin) args.push('--interactive');
  if (spec.init) args.push('--init');
  if (spec.auto_remove) args.push('--rm');

  if (spec.pull_policy && spec.pull_policy !== 'missing') {
    args.push('--pull', spec.pull_policy);
  }

  const restart = spec.restart_policy?.name;
  if (restart && restart !== 'no') {
    const retries = spec.restart_policy?.max_retries;
    args.push('--restart', restart === 'on-failure' && retries ? `on-failure:${retries}` : restart);
  }

  for (const port of spec.ports ?? []) {
    args.push('--publish', quote(formatPortArg(port)));
  }

  for (const mount of spec.mounts ?? []) {
    args.push('--mount', quote(formatMountArg(mount)));
  }

  for (const entry of spec.env ?? []) {
    if (!entry.key.trim()) continue;
    args.push('--env', quote(`${entry.key}=${entry.value}`));
  }

  for (const [key, value] of Object.entries(spec.labels ?? {})) {
    args.push('--label', quote(`${key}=${value}`));
  }

  const network = spec.network;
  if (network?.name) args.push('--network', quote(network.name));
  for (const alias of network?.aliases ?? []) args.push('--network-alias', quote(alias));
  if (network?.ipv4_address) args.push('--ip', network.ipv4_address);
  if (network?.ipv6_address) args.push('--ip6', network.ipv6_address);
  if (network?.mac_address) args.push('--mac-address', network.mac_address);
  for (const host of network?.extra_hosts ?? []) args.push('--add-host', quote(host));
  for (const server of network?.dns ?? []) args.push('--dns', server);
  for (const domain of network?.dns_search ?? []) args.push('--dns-search', domain);
  for (const option of network?.dns_options ?? []) args.push('--dns-option', quote(option));

  const resources = spec.resources;
  if (resources?.cpus) args.push('--cpus', String(resources.cpus));
  if (resources?.cpu_shares) args.push('--cpu-shares', String(resources.cpu_shares));
  if (resources?.cpuset_cpus) args.push('--cpuset-cpus', quote(resources.cpuset_cpus));
  if (resources?.memory) args.push('--memory', formatBytesArg(resources.memory));
  if (resources?.memory_reservation) {
    args.push('--memory-reservation', formatBytesArg(resources.memory_reservation));
  }
  if (resources?.memory_swap) args.push('--memory-swap', formatBytesArg(resources.memory_swap));
  if (resources?.pids_limit) args.push('--pids-limit', String(resources.pids_limit));
  if (resources?.shm_size) args.push('--shm-size', formatBytesArg(resources.shm_size));

  const health = spec.health_check;
  if (health?.disable) {
    args.push('--no-healthcheck');
  } else if (health?.test?.length) {
    args.push('--health-cmd', quote(health.test.join(' ')));
    if (health.interval) args.push('--health-interval', health.interval);
    if (health.timeout) args.push('--health-timeout', health.timeout);
    if (health.start_period) args.push('--health-start-period', health.start_period);
    if (health.retries) args.push('--health-retries', String(health.retries));
  }

  if (spec.logging?.driver) {
    args.push('--log-driver', quote(spec.logging.driver));
    for (const [key, value] of Object.entries(spec.logging.options ?? {})) {
      args.push('--log-opt', quote(`${key}=${value}`));
    }
  }

  const security = spec.security;
  if (security?.privileged) args.push('--privileged');
  if (security?.read_only_root_fs) args.push('--read-only');
  for (const cap of security?.cap_add ?? []) args.push('--cap-add', cap);
  for (const cap of security?.cap_drop ?? []) args.push('--cap-drop', cap);
  for (const opt of security?.security_opt ?? []) args.push('--security-opt', quote(opt));
  for (const device of security?.devices ?? []) args.push('--device', quote(device));
  for (const [key, value] of Object.entries(security?.sysctls ?? {})) {
    args.push('--sysctl', quote(`${key}=${value}`));
  }

  for (const part of spec.entrypoint ?? []) {
    args.push('--entrypoint', quote(part));
  }

  args.push(spec.image || '<image>');

  for (const part of spec.command ?? []) {
    args.push(quote(part));
  }

  return args.join(' ');
}

/** Renders a port row the way `--publish` takes it. */
function formatPortArg(port: {
  host_ip?: string;
  host_port?: string;
  container_port: number;
  protocol?: string;
}): string {
  const proto = port.protocol && port.protocol !== 'tcp' ? `/${port.protocol}` : '';
  const target = `${port.container_port}${proto}`;

  if (port.host_ip && port.host_port) return `${port.host_ip}:${port.host_port}:${target}`;
  if (port.host_port) return `${port.host_port}:${target}`;
  if (port.host_ip) return `${port.host_ip}::${target}`;
  return target;
}

/** Renders a mount row the way `--mount` takes it. */
function formatMountArg(mount: {
  type: string;
  source?: string;
  destination: string;
  read_only?: boolean;
  tmpfs_size?: number;
}): string {
  const parts = [`type=${mount.type || 'volume'}`];
  if (mount.source) parts.push(`source=${mount.source}`);
  parts.push(`target=${mount.destination}`);
  if (mount.read_only) parts.push('readonly');
  if (mount.type === 'tmpfs' && mount.tmpfs_size) {
    parts.push(`tmpfs-size=${mount.tmpfs_size}`);
  }
  return parts.join(',');
}

/** Renders a byte count the way an operator would type it. */
function formatBytesArg(bytes: number): string {
  const units: [number, string][] = [
    [1024 ** 3, 'g'],
    [1024 ** 2, 'm'],
    [1024, 'k'],
  ];
  for (const [size, suffix] of units) {
    if (bytes >= size && bytes % size === 0) return `${bytes / size}${suffix}`;
  }
  return String(bytes);
}

/**
 * Quotes an argument only when a shell would need it.
 *
 * Single quotes, with the standard `'\''` escape, because that is the one
 * form that is literal in every POSIX shell — an operator pasting the
 * preview into a terminal gets the container they were shown.
 */
function quote(value: string): string {
  if (value !== '' && !/[\s'"$`\\|&;<>()[\]{}*?!#~]/.test(value)) {
    return value;
  }
  return `'${value.replaceAll("'", `'\\''`)}'`;
}

/** Strips the fields the API treats as absent, for the payload preview. */
export function cleanSpec(spec: ContainerSpec): ContainerSpec {
  return JSON.parse(
    JSON.stringify(spec, (_key, value: unknown) => {
      if (value === '' || value === null) return undefined;
      if (Array.isArray(value) && value.length === 0) return undefined;
      if (typeof value === 'object' && value !== null && Object.keys(value).length === 0) {
        return undefined;
      }
      return value;
    }),
  ) as ContainerSpec;
}
