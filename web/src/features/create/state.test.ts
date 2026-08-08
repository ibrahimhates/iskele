import { describe, expect, it } from 'vitest';

import {
  bindSources,
  buildSpec,
  emptyForm,
  newRowID,
  parseDotEnv,
  privilegedOptionsUsed,
  splitArgs,
  splitList,
} from './state';

describe('splitArgs', () => {
  // `sh -c "echo hello world"` is three arguments; splitting on whitespace
  // alone would make it five and break every shell-form command.
  it('keeps a quoted argument whole', () => {
    expect(splitArgs('sh -c "echo hello world"')).toEqual(['sh', '-c', 'echo hello world']);
    expect(splitArgs("sh -c 'echo hello world'")).toEqual(['sh', '-c', 'echo hello world']);
  });

  it('collapses runs of whitespace', () => {
    expect(splitArgs('  nginx   -g   ')).toEqual(['nginx', '-g']);
  });

  it('returns nothing for an empty field', () => {
    expect(splitArgs('')).toEqual([]);
    expect(splitArgs('   ')).toEqual([]);
  });

  // An empty quoted argument is a real argument.
  it('keeps an explicitly empty argument', () => {
    expect(splitArgs('cmd ""')).toEqual(['cmd', '']);
  });
});

describe('splitList', () => {
  it('accepts commas and newlines alike', () => {
    expect(splitList('a, b\nc ,, d')).toEqual(['a', 'b', 'c', 'd']);
  });

  it('returns nothing for a blank field', () => {
    expect(splitList('  ,\n , ')).toEqual([]);
  });
});

describe('parseDotEnv', () => {
  it('parses a pasted .env file', () => {
    const parsed = parseDotEnv(
      [
        '# a comment',
        '',
        'POSTGRES_USER=app',
        'POSTGRES_PASSWORD="a secret with spaces"',
        "TOKEN='single quoted'",
        'export EXPORTED=yes',
        'EMPTY=',
      ].join('\n'),
    );

    expect(parsed).toEqual([
      { key: 'POSTGRES_USER', value: 'app' },
      { key: 'POSTGRES_PASSWORD', value: 'a secret with spaces' },
      { key: 'TOKEN', value: 'single quoted' },
      { key: 'EXPORTED', value: 'yes' },
      { key: 'EMPTY', value: '' },
    ]);
  });

  // A connection string is the common case and it is full of "=".
  it('splits on the first equals only', () => {
    expect(parseDotEnv('DSN=postgres://u:p@h/db?sslmode=disable')).toEqual([
      { key: 'DSN', value: 'postgres://u:p@h/db?sslmode=disable' },
    ]);
  });

  it('ignores lines that are not assignments', () => {
    expect(parseDotEnv('just a line\n=novalue\n')).toEqual([]);
  });
});

describe('buildSpec', () => {
  it('sends only what was filled in', () => {
    const spec = buildSpec({ ...emptyForm(), image: 'nginx:1.27' });

    expect(spec).toEqual({
      image: 'nginx:1.27',
      start: true,
      restart_policy: { name: 'unless-stopped' },
    });
  });

  it('converts megabyte fields to bytes', () => {
    const spec = buildSpec({ ...emptyForm(), image: 'app', memoryMB: '512', shmSizeMB: '64' });

    expect(spec.resources?.memory).toBe(512 * 1024 * 1024);
    expect(spec.resources?.shm_size).toBe(64 * 1024 * 1024);
  });

  it('carries a retry count only for on-failure', () => {
    const withRetries = buildSpec({
      ...emptyForm(),
      image: 'app',
      restartPolicy: 'on-failure',
      maxRetries: '5',
    });
    expect(withRetries.restart_policy).toEqual({ name: 'on-failure', max_retries: 5 });

    const withoutRetries = buildSpec({
      ...emptyForm(),
      image: 'app',
      restartPolicy: 'always',
      maxRetries: '5',
    });
    expect(withoutRetries.restart_policy).toEqual({ name: 'always' });
  });

  it('drops rows the operator started and abandoned', () => {
    const spec = buildSpec({
      ...emptyForm(),
      image: 'app',
      ports: [{ id: newRowID(), container_port: 0 }],
      mounts: [{ id: newRowID(), type: 'volume', destination: '  ' }],
      env: [{ id: newRowID(), key: '', value: 'orphan' }],
      labels: [{ id: newRowID(), key: '', value: 'orphan' }],
    });

    expect(spec.ports).toBeUndefined();
    expect(spec.mounts).toBeUndefined();
    expect(spec.env).toBeUndefined();
    expect(spec.labels).toBeUndefined();
  });

  it('strips the client-side row ids', () => {
    const spec = buildSpec({
      ...emptyForm(),
      image: 'app',
      ports: [{ id: newRowID(), container_port: 80, host_port: '8080' }],
    });

    expect(spec.ports?.[0]).toEqual({ container_port: 80, host_port: '8080' });
    expect(spec.ports?.[0]).not.toHaveProperty('id');
  });

  it('builds a health check only when one was asked for', () => {
    expect(
      buildSpec({ ...emptyForm(), image: 'app', healthTest: 'true' }).health_check,
    ).toBeUndefined();

    const enabled = buildSpec({
      ...emptyForm(),
      image: 'app',
      healthEnabled: true,
      healthTest: 'curl -f localhost/health',
    });
    expect(enabled.health_check?.test).toEqual(['curl -f localhost/health']);
    expect(enabled.health_check?.retries).toBe(3);

    const disabled = buildSpec({ ...emptyForm(), image: 'app', healthDisable: true });
    expect(disabled.health_check).toEqual({ disable: true });
  });

  it('trims the free-text fields', () => {
    const spec = buildSpec({
      ...emptyForm(),
      image: '  nginx:1.27  ',
      name: '  web  ',
      user: '  1000:1000  ',
    });

    expect(spec.image).toBe('nginx:1.27');
    expect(spec.name).toBe('web');
    expect(spec.user).toBe('1000:1000');
  });

  it('keeps an environment value exactly as typed', () => {
    const spec = buildSpec({
      ...emptyForm(),
      image: 'app',
      env: [{ id: newRowID(), key: ' KEY ', value: '  spaces matter  ' }],
    });

    expect(spec.env).toEqual([{ key: 'KEY', value: '  spaces matter  ' }]);
  });
});

describe('privilegedOptionsUsed', () => {
  it('finds nothing in an ordinary form', () => {
    expect(privilegedOptionsUsed({ ...emptyForm(), image: 'nginx' })).toEqual([]);
  });

  it('names each option that needs the permission', () => {
    const used = privilegedOptionsUsed({
      ...emptyForm(),
      privileged: true,
      capAdd: 'NET_ADMIN',
      devices: '/dev/sda',
      securityOpt: 'apparmor=unconfined',
      networkName: 'host',
    });

    expect(used).toEqual(['privileged', 'cap_add', 'devices', 'security_opt', 'network=host']);
  });

  // Dropping capabilities narrows the container; it must not be gated.
  it('does not count cap_drop', () => {
    expect(privilegedOptionsUsed({ ...emptyForm(), capDrop: 'ALL' })).toEqual([]);
  });
});

describe('bindSources', () => {
  it('lists only the bind mounts', () => {
    const sources = bindSources({
      ...emptyForm(),
      mounts: [
        { id: 1, type: 'bind', source: '/srv/data', destination: '/data' },
        { id: 2, type: 'volume', source: 'pgdata', destination: '/var/lib' },
        { id: 3, type: 'tmpfs', destination: '/tmp' },
        { id: 4, type: 'bind', source: '  ', destination: '/blank' },
      ],
    });

    expect(sources).toEqual(['/srv/data']);
  });
});
