import { useCallback, useMemo, useState } from 'react';

import type {
  ContainerSpec,
  EnvVar,
  MountSpec,
  PortMapping,
  PullPolicy,
  RestartPolicyName,
} from '../../api/types';

/**
 * The wizard's working state.
 *
 * Rows keep their own client-side id so React can key them: two mount rows
 * with the same (empty) destination are still different rows, and keying by
 * index makes a deletion re-use the wrong input's focus and value.
 */
export interface Row {
  id: number;
}

export type PortRow = Row & PortMapping;
export type MountRow = Row & MountSpec;
export type EnvRow = Row & EnvVar;
export type PairRow = Row & { key: string; value: string };

export interface FormState {
  name: string;
  image: string;
  pullPolicy: PullPolicy;
  start: boolean;

  restartPolicy: RestartPolicyName;
  maxRetries: string;
  autoRemove: boolean;
  init: boolean;

  command: string;
  entrypoint: string;
  workingDir: string;
  user: string;
  hostname: string;
  tty: boolean;
  openStdin: boolean;

  ports: PortRow[];
  mounts: MountRow[];
  env: EnvRow[];
  labels: PairRow[];

  networkName: string;
  aliases: string;
  ipv4: string;
  extraHosts: string;
  dns: string;

  cpus: string;
  cpuShares: string;
  cpusetCpus: string;
  memoryMB: string;
  memoryReservationMB: string;
  pidsLimit: string;
  shmSizeMB: string;

  healthEnabled: boolean;
  healthDisable: boolean;
  healthTest: string;
  healthInterval: string;
  healthTimeout: string;
  healthStartPeriod: string;
  healthRetries: string;

  logDriver: string;
  logOptions: PairRow[];

  privileged: boolean;
  readOnlyRootFS: boolean;
  capAdd: string;
  capDrop: string;
  securityOpt: string;
  devices: string;
  sysctls: PairRow[];
}

/** A form with nothing filled in but the defaults an operator expects. */
export function emptyForm(): FormState {
  return {
    name: '',
    image: '',
    pullPolicy: 'missing',
    start: true,

    restartPolicy: 'unless-stopped',
    maxRetries: '',
    autoRemove: false,
    init: false,

    command: '',
    entrypoint: '',
    workingDir: '',
    user: '',
    hostname: '',
    tty: false,
    openStdin: false,

    ports: [],
    mounts: [],
    env: [],
    labels: [],

    networkName: '',
    aliases: '',
    ipv4: '',
    extraHosts: '',
    dns: '',

    cpus: '',
    cpuShares: '',
    cpusetCpus: '',
    memoryMB: '',
    memoryReservationMB: '',
    pidsLimit: '',
    shmSizeMB: '',

    healthEnabled: false,
    healthDisable: false,
    healthTest: '',
    healthInterval: '30s',
    healthTimeout: '5s',
    healthStartPeriod: '',
    healthRetries: '3',

    logDriver: '',
    logOptions: [],

    privileged: false,
    readOnlyRootFS: false,
    capAdd: '',
    capDrop: '',
    securityOpt: '',
    devices: '',
    sysctls: [],
  };
}

let nextRowID = 1;

/** newRowID hands out the client-side keys described on [Row]. */
export function newRowID(): number {
  nextRowID += 1;
  return nextRowID;
}

/** useCreateForm holds the wizard's state and derives the spec from it. */
export function useCreateForm() {
  const [form, setForm] = useState<FormState>(emptyForm);

  const set = useCallback(<K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }));
  }, []);

  const spec = useMemo(() => buildSpec(form), [form]);

  return { form, set, setForm, spec };
}

/**
 * Turns the form into the API's container definition.
 *
 * Every text field that holds a list is split here rather than in the inputs,
 * so the form stays plain strings — which is what makes it possible to type
 * "a, b," without the field fighting back mid-edit.
 */
export function buildSpec(form: FormState): ContainerSpec {
  const spec: ContainerSpec = {
    image: form.image.trim(),
    start: form.start,
  };

  if (form.name.trim()) spec.name = form.name.trim();
  if (form.pullPolicy !== 'missing') spec.pull_policy = form.pullPolicy;

  const command = splitArgs(form.command);
  if (command.length) spec.command = command;
  const entrypoint = splitArgs(form.entrypoint);
  if (entrypoint.length) spec.entrypoint = entrypoint;

  if (form.workingDir.trim()) spec.working_dir = form.workingDir.trim();
  if (form.user.trim()) spec.user = form.user.trim();
  if (form.hostname.trim()) spec.hostname = form.hostname.trim();
  if (form.tty) spec.tty = true;
  if (form.openStdin) spec.open_stdin = true;
  if (form.autoRemove) spec.auto_remove = true;
  if (form.init) spec.init = true;

  if (form.restartPolicy !== 'no') {
    spec.restart_policy = { name: form.restartPolicy };
    if (form.restartPolicy === 'on-failure' && form.maxRetries.trim()) {
      spec.restart_policy.max_retries = Number(form.maxRetries);
    }
  }

  const ports = form.ports.filter((p) => p.container_port > 0).map(({ id: _id, ...port }) => port);
  if (ports.length) spec.ports = ports;

  const mounts = form.mounts
    .filter((m) => m.destination.trim() !== '')
    .map(({ id: _id, ...mount }) => mount);
  if (mounts.length) spec.mounts = mounts;

  const env = form.env
    .filter((e) => e.key.trim() !== '')
    .map(({ id: _id, ...entry }) => ({ key: entry.key.trim(), value: entry.value }));
  if (env.length) spec.env = env;

  const labels = pairsToRecord(form.labels);
  if (Object.keys(labels).length) spec.labels = labels;

  const network: ContainerSpec['network'] = {};
  if (form.networkName.trim()) network.name = form.networkName.trim();
  const aliases = splitList(form.aliases);
  if (aliases.length) network.aliases = aliases;
  if (form.ipv4.trim()) network.ipv4_address = form.ipv4.trim();
  const extraHosts = splitList(form.extraHosts);
  if (extraHosts.length) network.extra_hosts = extraHosts;
  const dns = splitList(form.dns);
  if (dns.length) network.dns = dns;
  if (Object.keys(network).length) spec.network = network;

  const resources: ContainerSpec['resources'] = {};
  if (form.cpus.trim()) resources.cpus = Number(form.cpus);
  if (form.cpuShares.trim()) resources.cpu_shares = Number(form.cpuShares);
  if (form.cpusetCpus.trim()) resources.cpuset_cpus = form.cpusetCpus.trim();
  if (form.memoryMB.trim()) resources.memory = megabytes(form.memoryMB);
  if (form.memoryReservationMB.trim()) {
    resources.memory_reservation = megabytes(form.memoryReservationMB);
  }
  if (form.pidsLimit.trim()) resources.pids_limit = Number(form.pidsLimit);
  if (form.shmSizeMB.trim()) resources.shm_size = megabytes(form.shmSizeMB);
  if (Object.keys(resources).length) spec.resources = resources;

  if (form.healthDisable) {
    spec.health_check = { disable: true };
  } else if (form.healthEnabled && form.healthTest.trim()) {
    spec.health_check = { test: [form.healthTest.trim()] };
    if (form.healthInterval.trim()) spec.health_check.interval = form.healthInterval.trim();
    if (form.healthTimeout.trim()) spec.health_check.timeout = form.healthTimeout.trim();
    if (form.healthStartPeriod.trim()) {
      spec.health_check.start_period = form.healthStartPeriod.trim();
    }
    if (form.healthRetries.trim()) spec.health_check.retries = Number(form.healthRetries);
  }

  if (form.logDriver.trim()) {
    spec.logging = { driver: form.logDriver.trim() };
    const options = pairsToRecord(form.logOptions);
    if (Object.keys(options).length) spec.logging.options = options;
  }

  const security: ContainerSpec['security'] = {};
  if (form.privileged) security.privileged = true;
  if (form.readOnlyRootFS) security.read_only_root_fs = true;
  const capAdd = splitList(form.capAdd);
  if (capAdd.length) security.cap_add = capAdd;
  const capDrop = splitList(form.capDrop);
  if (capDrop.length) security.cap_drop = capDrop;
  const securityOpt = splitList(form.securityOpt);
  if (securityOpt.length) security.security_opt = securityOpt;
  const devices = splitList(form.devices);
  if (devices.length) security.devices = devices;
  const sysctls = pairsToRecord(form.sysctls);
  if (Object.keys(sysctls).length) security.sysctls = sysctls;
  if (Object.keys(security).length) spec.security = security;

  return spec;
}

/**
 * Reports which of the wizard's options need the privileged permission, so the
 * form can say so before the server refuses.
 */
export function privilegedOptionsUsed(form: FormState): string[] {
  const used: string[] = [];
  if (form.privileged) used.push('privileged');
  if (splitList(form.capAdd).length) used.push('cap_add');
  if (splitList(form.devices).length) used.push('devices');
  if (splitList(form.securityOpt).length) used.push('security_opt');
  if (pairsToRecord(form.sysctls) && Object.keys(pairsToRecord(form.sysctls)).length) {
    used.push('sysctls');
  }
  if (form.networkName.trim().toLowerCase() === 'host') used.push('network=host');
  return used;
}

/** Host paths the form would bind-mount, for the whitelist warning. */
export function bindSources(form: FormState): string[] {
  return form.mounts
    .filter((m) => m.type === 'bind' && (m.source ?? '').trim() !== '')
    .map((m) => (m.source ?? '').trim());
}

/**
 * Splits a command line into arguments, honoring quotes.
 *
 * `sh -c "echo hello world"` has to reach the engine as three arguments, not
 * five; splitting on whitespace alone would break every shell-form command an
 * operator pastes in.
 */
export function splitArgs(input: string): string[] {
  const args: string[] = [];
  let current = '';
  let quote: '"' | "'" | null = null;
  let started = false;

  for (const char of input) {
    if (quote) {
      if (char === quote) {
        quote = null;
      } else {
        current += char;
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      started = true;
      continue;
    }
    if (/\s/.test(char)) {
      if (started) {
        args.push(current);
        current = '';
        started = false;
      }
      continue;
    }
    current += char;
    started = true;
  }

  if (started) args.push(current);
  return args;
}

/** Splits a comma- or newline-separated list, dropping blanks. */
export function splitList(input: string): string[] {
  return input
    .split(/[,\n]/)
    .map((item) => item.trim())
    .filter((item) => item !== '');
}

/** Parses pasted `.env` content into rows. */
export function parseDotEnv(input: string): EnvVar[] {
  const out: EnvVar[] = [];

  for (const rawLine of input.split('\n')) {
    const line = rawLine.trim();
    if (line === '' || line.startsWith('#')) continue;

    // `export FOO=bar` is what a shell script looks like, and pasting one in
    // is the common case.
    const withoutExport = line.startsWith('export ') ? line.slice('export '.length) : line;

    const separator = withoutExport.indexOf('=');
    if (separator <= 0) continue;

    const key = withoutExport.slice(0, separator).trim();
    let value = withoutExport.slice(separator + 1).trim();

    // A quoted value keeps its spaces; the quotes themselves are not part of it.
    const quoteChar = value.slice(0, 1);
    if (
      value.length >= 2 &&
      (quoteChar === '"' || quoteChar === "'") &&
      value.endsWith(quoteChar)
    ) {
      value = value.slice(1, -1);
    }

    if (key) out.push({ key, value });
  }

  return out;
}

/** Converts key/value rows into the record the API takes. */
export function pairsToRecord(rows: { key: string; value: string }[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (key) out[key] = row.value;
  }
  return out;
}

/** Reads a megabyte figure into bytes. */
function megabytes(input: string): number {
  return Math.round(Number(input) * 1024 * 1024);
}
