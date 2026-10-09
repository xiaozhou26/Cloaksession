import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { copyFile, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import test from 'node:test';

async function fixture(t, overrides = {}) {
  const directory = await mkdtemp(join(tmpdir(), 'cloaksession reverse fixture '));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const bridge = join(directory, 'bridge.mjs');
  await copyFile(new URL('../bridge.mjs', import.meta.url), bridge);
  await writeFile(join(directory, 'windows.mjs'), `export async function installWindowSelection() {}`);
  const packageDir = join(directory, 'node_modules/js-reverse-mcp');
  const entry = join(packageDir, 'unusual cli/entry.mjs');
  await mkdir(dirname(entry), { recursive: true });
  await writeFile(join(packageDir, 'package.json'), JSON.stringify({
    name: 'js-reverse-mcp', version: '4.0.5', type: 'module',
    main: 'not-the-cli.js', exports: { './package.json': './package.json' },
    bin: { 'js-reverse-mcp': 'unusual cli/entry.mjs' }, ...overrides,
  }));
  await writeFile(entry, `
    let input = '';
    process.stdin.setEncoding('utf8');
    process.stdin.on('data', chunk => { input += chunk; });
    process.stdin.on('end', () => {
      console.log(JSON.stringify({ argv: process.argv.slice(1), input, pid: process.pid, eof: true }));
    });
    console.error('fixture CLI');
  `);
  return { bridge, entry, directory };
}

for (const bin of [{ 'js-reverse-mcp': 'unusual cli/entry.mjs' }, 'unusual cli/entry.mjs']) {
  test(`bridge resolves the declared ${typeof bin} bin and preserves argv, stdio and EOF`, async (t) => {
    const { bridge, entry, directory } = await fixture(t, { bin });
    const args = ['--browserUrl', 'http://127.0.0.1:9222', '--allowedRoots', directory];
    const input = '{"jsonrpc":"2.0","id":1,"method":"tools/list"}\n';
    const result = spawnSync(process.execPath, [bridge, ...args], {
      cwd: tmpdir(), input, encoding: 'utf8', timeout: 10000,
    });
    assert.ifError(result.error);
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stderr.trim(), 'fixture CLI');
    assert.deepEqual(JSON.parse(result.stdout), { argv: [entry, ...args], input, pid: result.pid, eof: true });
  });
}

for (const [name, overrides, error] of [
  ['wrong version', { version: '4.0.4' }, /Expected js-reverse-mcp 4\.0\.5/],
  ['missing bin', { bin: {} }, /does not declare its CLI bin/],
  ['escaping bin', { bin: '../outside.mjs' }, /must be inside its package/],
  ['missing entry', { bin: 'absent.mjs' }, /Cannot find module/],
]) {
  test(`bridge rejects ${name} without writing to protocol stdout`, async (t) => {
    const { bridge } = await fixture(t, overrides);
    const result = spawnSync(process.execPath, [bridge], { encoding: 'utf8', timeout: 10000 });
    assert.ifError(result.error);
    assert.equal(result.status, 1);
    assert.equal(result.stdout, '');
    assert.match(result.stderr, error);
  });
}
