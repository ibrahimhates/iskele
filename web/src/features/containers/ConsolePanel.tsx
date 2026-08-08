import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';

import { fetchStreamTicket, websocketURL } from '../../api/client';
import { cn } from '../../lib/cn';

const SHELLS = ['/bin/sh', '/bin/bash', '/bin/ash', '/bin/zsh'];

type State = 'idle' | 'connecting' | 'connected' | 'closed';

/**
 * An interactive shell inside the container, the equivalent of `docker exec -it`.
 */
export function ConsolePanel({ containerID, running }: { containerID: string; running: boolean }) {
  const { t } = useTranslation();
  const [shell, setShell] = useState(SHELLS[0]!);
  const [state, setState] = useState<State>('idle');
  const [exitCode, setExitCode] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  const hostRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);

  // The terminal is created once and reused across connections, so scrollback
  // survives a disconnect.
  useEffect(() => {
    if (!hostRef.current || termRef.current) return;

    const term = new Terminal({
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
      fontSize: 13,
      cursorBlink: true,
      convertEol: true,
      theme: { background: '#09090b', foreground: '#fafafa' },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(hostRef.current);
    fit.fit();

    termRef.current = term;
    fitRef.current = fit;

    const onResize = () => {
      fit.fit();
      sendResize();
    };
    window.addEventListener('resize', onResize);

    return () => {
      window.removeEventListener('resize', onResize);
      socketRef.current?.close();
      term.dispose();
      termRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function sendResize() {
    const term = termRef.current;
    const socket = socketRef.current;
    if (!term || !socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(JSON.stringify({ t: 'resize', rows: term.rows, cols: term.cols }));
  }

  async function connect() {
    const term = termRef.current;
    if (!term) return;

    setState('connecting');
    setError(null);
    setExitCode(null);

    let socket: WebSocket;
    try {
      const ticket = await fetchStreamTicket();
      socket = new WebSocket(
        websocketURL(`/containers/${encodeURIComponent(containerID)}/exec`, {
          ticket,
          cmd: shell,
          tty: 'true',
          rows: String(term.rows),
          cols: String(term.cols),
        }),
      );
    } catch (err) {
      setState('closed');
      setError(err instanceof Error ? err.message : String(err));
      return;
    }

    socket.binaryType = 'arraybuffer';
    socketRef.current = socket;

    socket.onopen = () => {
      setState('connected');
      fitRef.current?.fit();
      sendResize();
      term.focus();
    };

    socket.onmessage = (event) => {
      if (typeof event.data === 'string') {
        // Text frames are control messages, not output.
        try {
          const message = JSON.parse(event.data) as { t: string; m?: string; exit_code?: number };
          if (message.t === 'err') setError(message.m ?? 'exec failed');
          if (message.t === 'exit' && typeof message.exit_code === 'number') {
            setExitCode(message.exit_code);
          }
        } catch {
          /* ignored */
        }
        return;
      }
      term.write(new Uint8Array(event.data as ArrayBuffer));
    };

    socket.onclose = () => {
      socketRef.current = null;
      setState('closed');
    };

    socket.onerror = () => setError('connection failed');

    term.onData((data) => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(new TextEncoder().encode(data));
      }
    });
  }

  function disconnect() {
    socketRef.current?.close();
    socketRef.current = null;
    setState('closed');
  }

  return (
    <div className="space-y-3">
      {!running && <p className="text-sm text-muted">{t('console.onlyRunning')}</p>}

      <div className="flex flex-wrap items-center gap-2">
        <label className="flex items-center gap-1.5 text-sm">
          <span className="text-muted">{t('console.shell')}</span>
          <select
            className="input w-36 py-1.5 text-sm"
            value={shell}
            onChange={(e) => setShell(e.target.value)}
            disabled={state === 'connected'}
          >
            {SHELLS.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>

        {state === 'connected' ? (
          <button type="button" className="btn-default" onClick={disconnect}>
            {t('console.disconnect')}
          </button>
        ) : (
          <button
            type="button"
            className="btn-primary"
            onClick={() => void connect()}
            disabled={!running || state === 'connecting'}
          >
            {t('console.connect')}
          </button>
        )}

        <span className="flex items-center gap-1.5 text-xs text-muted">
          <span
            className={cn(
              'h-1.5 w-1.5 rounded-full',
              state === 'connected'
                ? 'bg-ok'
                : state === 'connecting'
                  ? 'bg-warn animate-pulse'
                  : 'bg-muted',
            )}
            aria-hidden
          />
          {state === 'connected' ? t('console.connected') : t('console.disconnected')}
        </span>
      </div>

      {error && (
        <p className="rounded border border-danger/40 bg-danger/10 px-2 py-1 text-xs text-danger">
          {error}
        </p>
      )}
      {exitCode !== null && (
        <p className="text-xs text-muted">{t('console.exited', { code: exitCode })}</p>
      )}

      <div
        ref={hostRef}
        className="h-[55vh] min-h-72 overflow-hidden rounded-md border border-border bg-[#09090b] p-2"
      />
    </div>
  );
}
