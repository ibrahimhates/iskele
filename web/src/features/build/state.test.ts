import { describe, expect, it } from 'vitest';

import { newRowID } from '../create/state';
import { buildFormProblems, buildRequest, dockerBuildCommand, emptyBuildForm } from './state';

function pair(key: string, value: string) {
  return { id: newRowID(), key, value };
}

describe('buildRequest', () => {
  it('trims and drops what an operator leaves blank', () => {
    const request = buildRequest({
      ...emptyBuildForm(),
      context: '  /srv/app  ',
      dockerfile: '  ',
      target: '   ',
      platform: '',
    });

    expect(request.context).toBe('/srv/app');
    // Undefined, not the empty string: an empty `dockerfile` parameter is a
    // different request from an absent one.
    expect(request.dockerfile).toBeUndefined();
    expect(request.target).toBeUndefined();
    expect(request.platform).toBeUndefined();
  });

  it('splits tags on commas and newlines', () => {
    const request = buildRequest({
      ...emptyBuildForm(),
      context: '/srv/app',
      tags: 'app:latest,\nregistry.example.com/app:1.2.3,  ',
    });

    expect(request.tags).toEqual(['app:latest', 'registry.example.com/app:1.2.3']);
  });

  it('drops an argument row with no key but keeps an empty value', () => {
    const request = buildRequest({
      ...emptyBuildForm(),
      context: '/srv/app',
      buildArgs: [pair('VERSION', '1.2.3'), pair('  ', 'orphan'), pair('EMPTY', '')],
    });

    expect(request.buildArgs).toEqual({ VERSION: '1.2.3', EMPTY: '' });
  });
});

describe('buildFormProblems', () => {
  it('accepts a filled-in form', () => {
    expect(
      buildFormProblems({ ...emptyBuildForm(), context: '/srv/app', tags: 'app:latest' }),
    ).toEqual([]);
  });

  it('requires a context directory', () => {
    expect(buildFormProblems(emptyBuildForm())).toContain('build.problem_context');
  });

  it('refuses a Dockerfile that leaves the context', () => {
    expect(
      buildFormProblems({ ...emptyBuildForm(), context: '/srv/app', dockerfile: '/etc/passwd' }),
    ).toContain('build.problem_dockerfile');

    expect(
      buildFormProblems({ ...emptyBuildForm(), context: '/srv/app', dockerfile: '../Dockerfile' }),
    ).toContain('build.problem_dockerfile');
  });

  it('allows a Dockerfile in a subdirectory', () => {
    expect(
      buildFormProblems({
        ...emptyBuildForm(),
        context: '/srv/app',
        dockerfile: 'docker/Dockerfile.prod',
      }),
    ).toEqual([]);
  });

  it('refuses a tag with a space in it', () => {
    expect(
      buildFormProblems({ ...emptyBuildForm(), context: '/srv/app', tags: 'my app:latest' }),
    ).toContain('build.problem_tag');
  });
});

describe('dockerBuildCommand', () => {
  it('renders the command an operator would have typed', () => {
    const request = buildRequest({
      ...emptyBuildForm(),
      context: '/srv/app',
      dockerfile: 'docker/Dockerfile',
      tags: 'app:latest',
      buildArgs: [pair('VERSION', '1.2.3')],
      labels: [pair('org.opencontainers.image.source', 'https://example.com/app')],
      target: 'runtime',
      platform: 'linux/arm64',
      noCache: true,
      pull: true,
    });

    expect(dockerBuildCommand(request)).toBe(
      'docker build -t app:latest -f docker/Dockerfile --build-arg VERSION=1.2.3 ' +
        '--label org.opencontainers.image.source=https://example.com/app ' +
        '--target runtime --platform linux/arm64 --no-cache --pull /srv/app',
    );
  });

  it('quotes an argument a shell would misread', () => {
    const request = buildRequest({
      ...emptyBuildForm(),
      context: '/srv/my app',
      buildArgs: [pair('MOTD', "it's fine")],
    });

    expect(dockerBuildCommand(request)).toBe(
      `docker build --build-arg 'MOTD=it'\\''s fine' '/srv/my app'`,
    );
  });

  it('falls back to the current directory when nothing is picked', () => {
    expect(dockerBuildCommand(buildRequest(emptyBuildForm()))).toBe('docker build .');
  });

  it('leaves out a default Dockerfile', () => {
    const request = buildRequest({
      ...emptyBuildForm(),
      context: '/srv/app',
      dockerfile: 'Dockerfile',
    });

    expect(dockerBuildCommand(request)).toBe('docker build /srv/app');
  });
});
