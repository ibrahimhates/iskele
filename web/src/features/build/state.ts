import { useCallback, useMemo, useState } from 'react';

import type { BuildRequest } from '../../api/types';
import { newRowID, pairsToRecord, splitList, type PairRow } from '../create/state';

/** The build form's working state. */
export interface BuildFormState {
  context: string;
  /** Relative to the context directory; empty means `Dockerfile`. */
  dockerfile: string;
  /** Comma- or newline-separated image references. */
  tags: string;
  buildArgs: PairRow[];
  labels: PairRow[];
  target: string;
  platform: string;
  noCache: boolean;
  pull: boolean;
}

/** A form with nothing filled in but the defaults an operator expects. */
export function emptyBuildForm(): BuildFormState {
  return {
    context: '',
    dockerfile: '',
    tags: '',
    buildArgs: [],
    labels: [],
    target: '',
    platform: '',
    noCache: false,
    pull: false,
  };
}

/** Turns the form into what the build endpoint takes. */
export function buildRequest(form: BuildFormState): BuildRequest {
  return {
    context: form.context.trim(),
    dockerfile: form.dockerfile.trim() || undefined,
    tags: splitList(form.tags),
    buildArgs: pairsToRecord(form.buildArgs),
    labels: pairsToRecord(form.labels),
    target: form.target.trim() || undefined,
    platform: form.platform.trim() || undefined,
    noCache: form.noCache,
    pull: form.pull,
  };
}

/**
 * Why the form cannot be submitted yet, as translation keys.
 *
 * The server refuses the same things, and better; this only saves an operator
 * a round trip for the two mistakes a form can catch on its own.
 */
export function buildFormProblems(form: BuildFormState): string[] {
  const problems: string[] = [];

  if (!form.context.trim()) {
    problems.push('build.problem_context');
  }
  const dockerfile = form.dockerfile.trim();
  if (dockerfile.startsWith('/') || dockerfile.split('/').includes('..')) {
    problems.push('build.problem_dockerfile');
  }
  for (const tag of splitList(form.tags)) {
    if (/\s/.test(tag)) {
      problems.push('build.problem_tag');
      break;
    }
  }

  return problems;
}

/** An equivalent `docker build` command, for an operator who wants to check. */
export function dockerBuildCommand(request: BuildRequest): string {
  const parts = ['docker', 'build'];

  for (const tag of request.tags) parts.push('-t', shellQuote(tag));
  if (request.dockerfile && request.dockerfile !== 'Dockerfile') {
    parts.push('-f', shellQuote(request.dockerfile));
  }
  for (const [key, value] of Object.entries(request.buildArgs)) {
    parts.push('--build-arg', shellQuote(`${key}=${value}`));
  }
  for (const [key, value] of Object.entries(request.labels)) {
    parts.push('--label', shellQuote(`${key}=${value}`));
  }
  if (request.target) parts.push('--target', shellQuote(request.target));
  if (request.platform) parts.push('--platform', shellQuote(request.platform));
  if (request.noCache) parts.push('--no-cache');
  if (request.pull) parts.push('--pull');

  parts.push(shellQuote(request.context || '.'));
  return parts.join(' ');
}

/** Quotes an argument only when a shell would otherwise misread it. */
function shellQuote(value: string): string {
  if (value !== '' && /^[A-Za-z0-9_@%+=:,./-]+$/.test(value)) {
    return value;
  }
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

/** useBuildForm holds the form's state and derives the request from it. */
export function useBuildForm() {
  const [form, setForm] = useState<BuildFormState>(emptyBuildForm);

  const set = useCallback(<K extends keyof BuildFormState>(key: K, value: BuildFormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }));
  }, []);

  const addPair = useCallback((key: 'buildArgs' | 'labels') => {
    setForm((current) => ({
      ...current,
      [key]: [...current[key], { id: newRowID(), key: '', value: '' }],
    }));
  }, []);

  const removePair = useCallback((key: 'buildArgs' | 'labels', id: number) => {
    setForm((current) => ({ ...current, [key]: current[key].filter((row) => row.id !== id) }));
  }, []);

  const updatePair = useCallback(
    (key: 'buildArgs' | 'labels', id: number, patch: Partial<PairRow>) => {
      setForm((current) => ({
        ...current,
        [key]: current[key].map((row) => (row.id === id ? { ...row, ...patch } : row)),
      }));
    },
    [],
  );

  const request = useMemo(() => buildRequest(form), [form]);
  const problems = useMemo(() => buildFormProblems(form), [form]);

  return { form, set, setForm, addPair, removePair, updatePair, request, problems };
}
