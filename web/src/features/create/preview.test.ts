import { describe, expect, it } from 'vitest';

import { cleanSpec, dockerRunCommand } from './preview';
import type { ContainerSpec } from '../../api/types';

const base: ContainerSpec = { image: 'nginx:1.27' };

describe('dockerRunCommand', () => {
  it('renders a bare spec as docker create', () => {
    // Nothing has been asked to start, so `run` would misrepresent it.
    expect(dockerRunCommand(base)).toBe('docker create nginx:1.27');
  });

  it('renders a starting spec as a detached run', () => {
    expect(dockerRunCommand({ ...base, start: true })).toBe('docker run -d nginx:1.27');
  });

  it('places the image before the command', () => {
    const command = dockerRunCommand({
      ...base,
      start: true,
      command: ['nginx', '-g', 'daemon off;'],
    });
    expect(command).toBe("docker run -d nginx:1.27 nginx -g 'daemon off;'");
  });

  it('renders port mappings the way --publish takes them', () => {
    const command = dockerRunCommand({
      ...base,
      ports: [
        { container_port: 80, host_port: '8080', host_ip: '127.0.0.1' },
        { container_port: 53, host_port: '5353', protocol: 'udp' },
        { container_port: 9000 },
      ],
    });

    expect(command).toContain('--publish 127.0.0.1:8080:80');
    expect(command).toContain('--publish 5353:53/udp');
    expect(command).toContain('--publish 9000');
  });

  it('renders mounts the way --mount takes them', () => {
    const command = dockerRunCommand({
      ...base,
      mounts: [
        { type: 'bind', source: '/srv/data', destination: '/data', read_only: true },
        { type: 'volume', source: 'pgdata', destination: '/var/lib/postgresql/data' },
        { type: 'tmpfs', destination: '/tmp', tmpfs_size: 67108864 },
      ],
    });

    expect(command).toContain('type=bind,source=/srv/data,target=/data,readonly');
    expect(command).toContain('type=volume,source=pgdata,target=/var/lib/postgresql/data');
    expect(command).toContain('type=tmpfs,target=/tmp,tmpfs-size=67108864');
  });

  // A value with spaces or quotes in it has to survive being pasted into a
  // shell, or the preview is worse than nothing.
  it('quotes environment values that a shell would mangle', () => {
    const command = dockerRunCommand({
      ...base,
      env: [
        { key: 'PLAIN', value: 'simple' },
        { key: 'DSN', value: 'postgres://u:p@host/db?sslmode=disable' },
        { key: 'MESSAGE', value: "it's here" },
        { key: '  ', value: 'dropped' },
      ],
    });

    // Nothing a shell would touch, so nothing to quote.
    expect(command).toContain('--env PLAIN=simple');
    // The "?" makes it a glob to a shell.
    expect(command).toContain(`--env 'DSN=postgres://u:p@host/db?sslmode=disable'`);
    // An apostrophe inside single quotes needs the '\'' dance.
    expect(command).toContain(`--env 'MESSAGE=it'\\''s here'`);
    // A blank key is not an environment variable.
    expect(command).not.toContain('dropped');
  });

  it('renders a restart policy with its retry count', () => {
    expect(dockerRunCommand({ ...base, restart_policy: { name: 'always' } })).toContain(
      '--restart always',
    );
    expect(
      dockerRunCommand({ ...base, restart_policy: { name: 'on-failure', max_retries: 5 } }),
    ).toContain('--restart on-failure:5');
    // "no" is the default and adding it would be noise.
    expect(dockerRunCommand({ ...base, restart_policy: { name: 'no' } })).not.toContain(
      '--restart',
    );
  });

  it('renders byte limits the way an operator types them', () => {
    const command = dockerRunCommand({
      ...base,
      resources: { memory: 512 * 1024 * 1024, memory_swap: 1024 * 1024 * 1024, cpus: 1.5 },
    });

    expect(command).toContain('--memory 512m');
    expect(command).toContain('--memory-swap 1g');
    expect(command).toContain('--cpus 1.5');
  });

  it('renders a health check, and its absence', () => {
    const withCheck = dockerRunCommand({
      ...base,
      health_check: { test: ['curl -f localhost/health'], interval: '30s', retries: 3 },
    });
    expect(withCheck).toContain(`--health-cmd 'curl -f localhost/health'`);
    expect(withCheck).toContain('--health-interval 30s');
    expect(withCheck).toContain('--health-retries 3');

    expect(dockerRunCommand({ ...base, health_check: { disable: true } })).toContain(
      '--no-healthcheck',
    );
  });

  it('renders the privileged options, which is the point of showing them', () => {
    const command = dockerRunCommand({
      ...base,
      security: {
        privileged: true,
        cap_add: ['NET_ADMIN'],
        cap_drop: ['ALL'],
        devices: ['/dev/ttyUSB0'],
        security_opt: ['apparmor=unconfined'],
        read_only_root_fs: true,
        sysctls: { 'net.ipv4.ip_forward': '1' },
      },
    });

    expect(command).toContain('--privileged');
    expect(command).toContain('--read-only');
    expect(command).toContain('--cap-add NET_ADMIN');
    expect(command).toContain('--cap-drop ALL');
    expect(command).toContain('--device /dev/ttyUSB0');
    expect(command).toContain('--security-opt apparmor=unconfined');
    expect(command).toContain('--sysctl net.ipv4.ip_forward=1');
  });

  it('renders network attachment', () => {
    const command = dockerRunCommand({
      ...base,
      network: {
        name: 'backend',
        aliases: ['api'],
        ipv4_address: '172.30.0.5',
        extra_hosts: ['db:10.0.0.5'],
      },
    });

    expect(command).toContain('--network backend');
    expect(command).toContain('--network-alias api');
    expect(command).toContain('--ip 172.30.0.5');
    expect(command).toContain('--add-host db:10.0.0.5');
  });

  it('names the image placeholder while the form is still empty', () => {
    expect(dockerRunCommand({ image: '' })).toContain('<image>');
  });
});

describe('cleanSpec', () => {
  it('drops the fields the API treats as absent', () => {
    const cleaned = cleanSpec({
      image: 'nginx',
      name: '',
      command: [],
      labels: {},
      env: [{ key: 'A', value: 'b' }],
    });

    expect(cleaned).toEqual({ image: 'nginx', env: [{ key: 'A', value: 'b' }] });
  });

  // An empty string is meaningful in an environment value: FOO= is not FOO
  // being unset.
  it('keeps empty strings inside kept objects', () => {
    const cleaned = cleanSpec({ image: 'nginx', env: [{ key: 'EMPTY', value: '' }] });
    expect(cleaned.env).toEqual([{ key: 'EMPTY' }]);
  });
});
