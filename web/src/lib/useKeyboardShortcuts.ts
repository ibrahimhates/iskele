import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';

/** How long a `g` prefix stays armed before it is forgotten. */
const CHORD_WINDOW_MS = 1000;

// Only routes that exist: `g s` for stacks comes back with M7 (D-041).
const CHORDS: Record<string, string> = {
  c: '/containers',
  i: '/images',
  v: '/volumes',
  n: '/networks',
  d: '/dashboard',
};

/**
 * Global shortcuts: `/` focuses search, `g` then a letter navigates.
 *
 * Keystrokes inside a text field or a terminal are ignored — a shortcut that
 * fires while someone is typing a container name is worse than no shortcut.
 */
export function useKeyboardShortcuts(): void {
  const navigate = useNavigate();

  useEffect(() => {
    let chordArmedAt = 0;

    function isTyping(target: EventTarget | null): boolean {
      if (!(target instanceof HTMLElement)) return false;
      const tag = target.tagName;
      return (
        tag === 'INPUT' ||
        tag === 'TEXTAREA' ||
        tag === 'SELECT' ||
        target.isContentEditable ||
        target.closest('.xterm') !== null
      );
    }

    function onKeyDown(event: KeyboardEvent) {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      if (isTyping(event.target)) return;

      if (event.key === '/') {
        const search = document.querySelector<HTMLInputElement>('[data-search-input]');
        if (search) {
          event.preventDefault();
          search.focus();
        }
        return;
      }

      if (event.key === 'g') {
        chordArmedAt = Date.now();
        return;
      }

      if (chordArmedAt && Date.now() - chordArmedAt < CHORD_WINDOW_MS) {
        const destination = CHORDS[event.key];
        chordArmedAt = 0;
        if (destination) {
          event.preventDefault();
          navigate(destination);
        }
      }
    }

    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [navigate]);
}
