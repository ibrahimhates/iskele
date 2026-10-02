import type { Container } from '../../api/types';

/**
 * States the engine refuses to remove without force: it answers 409 for a
 * running, paused or restarting container rather than killing it.
 */
const FORCE_STATES = new Set(['running', 'paused', 'restarting']);

export function needsForce(state: string): boolean {
  return FORCE_STATES.has(state);
}

/**
 * The compose project a container belongs to, if any.
 *
 * Removing one service of a stack leaves the stack drifted: the next deploy
 * recreates it. Iskele's own label wins over compose's, since a stack Iskele
 * manages carries both.
 */
export function stackOf(container: Pick<Container, 'labels'>): string | undefined {
  const labels = container.labels ?? {};
  return labels['com.iskele.stack'] || labels['com.docker.compose.project'] || undefined;
}
