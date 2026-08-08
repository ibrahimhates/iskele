// Recreates web/dist/.gitkeep after a build.
//
// The file is committed so that `go build` works on a checkout that has never
// run the frontend build — go:embed needs the directory to exist. Vite's
// emptyOutDir wipes it on every build, which would leave the working tree
// showing a deleted tracked file after `make build`.

import { closeSync, mkdirSync, openSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = join(dirname(fileURLToPath(import.meta.url)), '..', 'dist');
mkdirSync(dist, { recursive: true });
closeSync(openSync(join(dist, '.gitkeep'), 'a'));
