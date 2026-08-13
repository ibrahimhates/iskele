import { loader } from '@monaco-editor/react';
// The editor API alone, not the `monaco-editor` barrel: that one pulls in
// every language Monaco ships — Solidity, PowerShell, a TypeScript compiler —
// and turns a YAML box into four megabytes of download.
import * as monaco from 'monaco-editor/editor/editor.api';
import editorWorker from 'monaco-editor/editor/editor.worker?worker';

// The two languages Iskele actually edits, registered one by one. The
// `basic-languages` barrel registers all ninety of them, and every unused
// tokenizer is dead weight inside a binary that ships its own frontend.
import 'monaco-editor/languages/definitions/yaml/register';
import 'monaco-editor/languages/definitions/ini/register';

// The editing behaviour a person expects from a code box. Imported one by one
// because the bundle that has them all also has everything else.
import 'monaco-editor/editor/contrib/find/browser/findController';
import 'monaco-editor/editor/contrib/folding/browser/folding';
import 'monaco-editor/editor/contrib/comment/browser/comment';
import 'monaco-editor/editor/contrib/bracketMatching/browser/bracketMatching';
import 'monaco-editor/editor/contrib/wordOperations/browser/wordOperations';
import 'monaco-editor/editor/contrib/multicursor/browser/multicursor';
import 'monaco-editor/editor/contrib/indentation/browser/indentation';
import 'monaco-editor/editor/contrib/contextmenu/browser/contextmenu';

/**
 * Points the editor at the copy of Monaco bundled into this binary.
 *
 * `@monaco-editor/react` fetches Monaco from a CDN by default. Iskele is a
 * single binary that serves its own frontend, often on a host with no route to
 * the internet at all — an editor that only works when jsdelivr is reachable
 * would be an editor that does not work.
 */
let configured = false;

export function configureMonaco(): void {
  if (configured) return;
  configured = true;

  // Monaco runs its tokenizer off the main thread. Without a worker it falls
  // back to synchronous parsing and the tab freezes on a large file.
  window.MonacoEnvironment = {
    getWorker: () => new editorWorker(),
  };

  loader.config({ monaco });
}

/** The editor options every Iskele editor shares. */
export const editorOptions = {
  minimap: { enabled: false },
  fontSize: 13,
  lineNumbers: 'on',
  scrollBeyondLastLine: false,
  automaticLayout: true,
  tabSize: 2,
  insertSpaces: true,
  renderWhitespace: 'selection',
  wordWrap: 'off',
} as const;
