import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, isAbsolute, relative, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { installWindowSelection } from './windows.mjs';

try {
  const require = createRequire(import.meta.url);
  const manifestPath = require.resolve('js-reverse-mcp/package.json');
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  if (manifest.version !== '4.0.5') {
    throw new Error(`Expected js-reverse-mcp 4.0.5, found ${manifest.version}`);
  }
  const bin = typeof manifest.bin === 'string' ? manifest.bin : manifest.bin?.['js-reverse-mcp'];
  if (typeof bin !== 'string' || !bin) {
    throw new Error('js-reverse-mcp does not declare its CLI bin');
  }
  const packageDir = dirname(manifestPath);
  const entry = resolve(packageDir, bin);
  const localPath = relative(packageDir, entry);
  if (isAbsolute(localPath) || localPath === '..' || localPath.startsWith('../') || localPath.startsWith('..\\')) {
    throw new Error('js-reverse-mcp CLI bin must be inside its package');
  }
  // Keep the CLI in this process so upstream owns stdio, signals, and EOF cleanup.
  process.argv[1] = entry;
  await installWindowSelection(pathToFileURL(`${dirname(entry)}/`));
  await import(pathToFileURL(entry).href);
} catch (error) {
  console.error(`Reverse bridge failed: ${error.message}`);
  process.exitCode = 1;
}
