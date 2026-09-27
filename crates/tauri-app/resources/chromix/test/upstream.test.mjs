import assert from 'node:assert/strict';
import childProcess from 'node:child_process';
import { createHash } from 'node:crypto';
import dns from 'node:dns';
import { cp, mkdir, mkdtemp, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import http from 'node:http';
import https from 'node:https';
import { syncBuiltinESMExports } from 'node:module';
import net from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, before, describe, mock, test } from 'node:test';
import tls from 'node:tls';
import { fileURLToPath, pathToFileURL } from 'node:url';

const commit = '39b9ea1bd262c2eb6306f3e23279398a6476c845';
const repo = 'https://github.com/xiaozhou26/Chromix';
const upstream = new URL('../vendor/chromix/', import.meta.url);
const manifest = JSON.parse(await readFile(new URL('../vendor/UPSTREAM.json', import.meta.url), 'utf8'));
const pkg = JSON.parse(await readFile(new URL('package.json', upstream), 'utf8'));

test('upstream manifest pins the complete unmodified package file set and SHA-256 hashes', async () => {
  assert.equal(manifest.schemaVersion, 1);
  assert.equal(manifest.commit, commit);
  assert.equal(manifest.repo, repo);
  assert.equal(manifest.sourceURL, `${repo}/tree/${commit}/sdk/node`);
  assert.match(manifest.fetchedDate, /^\d{4}-\d{2}-\d{2}$/);
  assert.deepEqual(manifest.package, { name: pkg.name, version: pkg.version });
  assert.equal(manifest.npmComparison.registryVersion, '0.1.0');
  assert.ok(manifest.npmComparison.summary.length > 0);
  assert.equal(manifest.hashAlgorithm, 'sha256');
  const expected = ['package.json', ...pkg.files].sort();
  assert.equal(new Set(expected).size, expected.length);
  assert.equal(expected.length, 15);
  assert.equal(manifest.fileCount, expected.length);
  assert.deepEqual(Object.keys(manifest.files).sort(), expected);
  // Local package installation can add dependencies, not upstream source files.
  const entries = (await readdir(upstream, { withFileTypes: true }))
    .filter((entry) => entry.name !== 'node_modules' || !entry.isDirectory());
  assert.ok(entries.every((entry) => entry.isFile()));
  assert.deepEqual(entries.map((entry) => entry.name).sort(), expected);
  for (const name of expected) {
    assert.match(manifest.files[name], /^[a-f0-9]{64}$/);
    const bytes = await readFile(new URL(name, upstream));
    assert.equal(createHash('sha256').update(bytes).digest('hex'), manifest.files[name], name);
  }
});

describe('pinned upstream option builders, without network or browser launch', { concurrency: false }, () => {
  let sdk, puppeteer, cookies, devicePool, profileSeed;
  let directory, executable, sdkRoot;
  const environment = new Map();
  const attempts = [];
  const forbid = (name) => () => {
    attempts.push(name);
    throw new Error(`Offline upstream test forbids ${name}`);
  };
  const setEnv = (key, value) => {
    environment.set(key, process.env[key]);
    process.env[key] = value;
  };
  const build = (options = {}) => sdk.buildLaunchOptions({
    ...options,
    launchOptions: { executablePath: executable, ...options.launchOptions },
  });
  const flags = (options) => new Map(options.args.map((arg) => {
    const separator = arg.indexOf('=');
    return separator < 0 ? [arg, ''] : [arg.slice(0, separator), arg.slice(separator + 1)];
  }));

  before(async () => {
    mock.method(globalThis, 'fetch', forbid('fetch'));
    for (const [name, target, methods] of [
      ['http', http, ['request', 'get']],
      ['https', https, ['request', 'get']],
      ['net', net, ['connect', 'createConnection']],
      ['Socket', net.Socket.prototype, ['connect']],
      ['tls', tls, ['connect']],
      ['dns', dns, ['lookup', 'resolve']],
      ['dns.promises', dns.promises, ['lookup', 'resolve']],
      ['child_process', childProcess, ['spawn', 'spawnSync', 'exec', 'execSync', 'execFile', 'execFileSync', 'fork']],
    ]) {
      for (const method of methods) mock.method(target, method, forbid(`${name}.${method}`));
    }
    syncBuiltinESMExports();
    directory = await mkdtemp(join(tmpdir(), 'chromix-upstream-'));
    executable = join(directory, process.platform === 'win32' ? 'fake-chromix.exe' : 'fake-chromix');
    await writeFile(executable, 'Not a browser; option builders must never execute this file.\n', { mode: 0o755 });
    sdkRoot = join(directory, 'sdk');
    await cp(fileURLToPath(upstream), sdkRoot, { recursive: true });
    await mkdir(join(sdkRoot, 'node_modules', 'yauzl'), { recursive: true });
    await writeFile(join(sdkRoot, 'node_modules', 'yauzl', 'package.json'), JSON.stringify({ name: 'yauzl', version: '3.4.0', type: 'module' }));
    await writeFile(join(sdkRoot, 'node_modules', 'yauzl', 'index.js'), 'export default {};\n');
    setEnv('CLOAKBROWSER_WIDEVINE', '0');
    setEnv('CLOAKBROWSER_BINARY_PATH', join(directory, 'missing-environment-binary'));
    setEnv('CHROMIX_TEST_OVERRIDE', 'inherited');
    const staged = (name) => pathToFileURL(join(sdkRoot, name));
    [sdk, puppeteer, cookies, devicePool, { profileSeed }] = await Promise.all([
      import(staged('index.js')),
      import(staged('puppeteer.js')),
      import(staged('cookies.js')),
      import(staged('_device_pool.js')),
      import(staged('_profile.js')),
    ]);
  });

  after(async () => {
    mock.restoreAll();
    syncBuiltinESMExports();
    for (const [key, value] of environment) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
    if (directory) await rm(directory, { recursive: true, force: true });
    assert.deepEqual(attempts, [], 'No download, network lookup, Python process or browser launch is allowed');
  });

  test('main exports cookies and Puppeteer while measured admission remains internal', () => {
    assert.deepEqual(pkg.exports, { '.': './index.js', './puppeteer': './puppeteer.js', './cookies': './cookies.js' });
    assert.equal(pkg.dependencies.yauzl, '^3.4.0');
    for (const name of ['launch', 'launchContext', 'launchPersistentContext', 'buildLaunchOptions']) {
      assert.equal(typeof sdk[name], 'function', name);
      assert.equal(typeof puppeteer[name], 'function', name);
    }
    for (const name of ['exportCookies', 'importCookies', 'encryptCookies', 'decryptCookies']) {
      assert.equal(typeof cookies[name], 'function', name);
      assert.equal(sdk[name], cookies[name]);
      assert.equal(puppeteer[name], cookies[name]);
    }
    assert.equal(typeof puppeteer.connect, 'function');
    assert.equal(typeof devicePool.measuredOptions, 'function');
    assert.equal(typeof devicePool.launchMeasured, 'function');
    assert.equal(Object.hasOwn(pkg.exports, './devicePool'), false);
  });

  test('nested executablePath bypasses binary resolution; top-level executablePath is ignored', async () => {
    const options = await build({ executablePath: join(directory, 'ignored-top-level') });
    assert.equal(options.executablePath, executable);
    assert.deepEqual(options.ignoreDefaultArgs, ['--enable-automation']);
    await assert.rejects(sdk.buildLaunchOptions({ executablePath: executable }), {
      message: `CLOAKBROWSER_BINARY_PATH does not exist: ${process.env.CLOAKBROWSER_BINARY_PATH}`,
    });
    await assert.rejects(build({ launchOptions: { executablePath: join(directory, 'missing-nested') } }), {
      message: 'executablePath must name an existing browser executable',
    });
  });

  test('custom fingerprint flags retain uint64 seeds and normalize supported public values', async () => {
    const args = [
      '--fingerprint=18446744073709551615', '--fingerprint-platform=windows',
      '--fingerprint-brand=edge', '--fingerprint-brand-version=153.0.8010.36',
      '--fingerprint-platform-version=15.0.0', '--fingerprint-hardware-concurrency=16',
      '--fingerprint-device-memory=16', '--fingerprint-screen-width=2560',
      '--fingerprint-screen-height=1440', '--fingerprint-taskbar-height=48',
      '--fingerprint-storage-quota=102400', '--fingerprint-gpu-backend=compatibility',
      '--fingerprint-gpu-vendor=Custom vendor', '--fingerprint-gpu-renderer=Custom renderer',
      '--fingerprint-font-policy=restricted', '--fingerprint-font-whitelist=Arial,Times New Roman',
      '--fingerprint-audio-render=isolated', '--fingerprint-audio-seed=18446744073709551615',
      '--fingerprint-timer-resolution=7', '--fingerprint-codec-h264=supported,smooth',
      '--fingerprint-pointer=coarse', '--fingerprint-hover=none', '--fingerprint-max-touch-points=1',
      '--fingerprint-color-scheme=dark', '--fingerprint-noise=false',
      '--fingerprint-reduced-motion=on', '--fingerprint-webrtc-ip=203.0.113.7',
      '--fingerprint-allow-3p-cookies', '--enable-blink-features=FakeShadowRoot',
    ];
    const snapshot = [...args];
    const result = await build({ args, startMaximized: false });
    const actual = flags(result);
    for (const arg of args) {
      const [key] = arg.split('=', 1);
      if (['--fingerprint-brand', '--fingerprint-reduced-motion', '--fingerprint-allow-3p-cookies'].includes(key)) continue;
      assert.ok(result.args.includes(arg), arg);
    }
    assert.equal(actual.get('--fingerprint-brand'), 'Edge');
    assert.equal(actual.get('--fingerprint-reduced-motion'), 'true');
    assert.equal(actual.get('--fingerprint-allow-3p-cookies'), 'true');
    assert.equal(actual.size, result.args.length);
    assert.equal(actual.has('--ignore-gpu-blocklist'), false);
    assert.deepEqual(args, snapshot);
  });

  test('invalid custom fields preserve exact upstream validation errors', async () => {
    for (const [arg, message] of [
      ['--fingerprint=18446744073709551616', '--fingerprint requires a uint64 seed or off/false/0/disable/disabled'],
      ['--fingerprint-brand=Brave', '--fingerprint-brand must be Chrome, Edge, Opera or Vivaldi'],
      ['--fingerprint-hardware-concurrency=129', '--fingerprint-hardware-concurrency requires an integer in [1, 128]'],
      ['--fingerprint-device-memory=33', '--fingerprint-device-memory requires a number greater than 0 and at most 32'],
      ['--fingerprint-storage-quota=-1', '--fingerprint-storage-quota requires an integer in [0, 8796093022207]'],
      ['--fingerprint-timer-resolution=1001', '--fingerprint-timer-resolution requires an integer in [0, 1000]'],
      ['--fingerprint-gpu-backend=software', '--fingerprint-gpu-backend requires one of native, compatibility'],
      ['--fingerprint-webrtc-fake-srflx=203.0.113.7', '--fingerprint-webrtc-fake-srflx is retired; use --fingerprint-webrtc-ip for candidate presentation.'],
    ]) {
      await assert.rejects(build({ args: [arg] }), { message }, arg);
    }
  });

  test('launchOptions replace input args but keep SDK seed and dedicated regional overrides', async () => {
    const options = {
      headless: true, timezone: 'Asia/Shanghai', timezoneId: 'UTC', locale: 'zh-CN',
      args: ['--fingerprint=11', '--top-only'],
      contextOptions: { args: ['--context-only'] },
      launchOptions: {
        headless: false, args: ['--fingerprint=42', '--lang=fr-FR', '--fingerprint-timezone=UTC', '--nested-only'],
        timeout: 4321, ignoreDefaultArgs: ['--custom-default'], env: { CHROMIX_TEST_OVERRIDE: 'explicit' },
      },
    };
    const snapshot = structuredClone(options);
    const result = await build(options);
    const actual = flags(result);
    assert.equal(actual.get('--fingerprint'), '42');
    assert.equal(actual.get('--lang'), 'zh-CN');
    assert.equal(actual.get('--fingerprint-locale'), 'zh-CN');
    assert.equal(actual.get('--fingerprint-timezone'), 'Asia/Shanghai');
    assert.equal(actual.has('--nested-only'), true);
    assert.equal(actual.has('--top-only'), false);
    assert.equal(actual.has('--context-only'), false);
    assert.equal(result.headless, false);
    assert.equal(result.timeout, 4321);
    assert.deepEqual(result.ignoreDefaultArgs, ['--custom-default']);
    assert.equal(result.env.CHROMIX_TEST_OVERRIDE, 'explicit');
    assert.deepEqual(options, snapshot);
  });

  test('native viewport and top-level context overrides replace nested defaults', () => {
    assert.equal(sdk.buildContextOptions().viewport, null);
    const nested = { viewport: { width: 800, height: 600 }, userAgent: 'Nested UA', colorScheme: 'light', permissions: ['clipboard-read'] };
    assert.deepEqual(sdk.buildContextOptions({ contextOptions: nested }), nested);
    const result = sdk.buildContextOptions({ viewport: null, userAgent: 'Top UA', colorScheme: 'dark', contextOptions: nested });
    assert.deepEqual(result, { ...nested, viewport: null, userAgent: 'Top UA', colorScheme: 'dark' });
  });

  test('context locale/timezone are stripped, while conflicting launch keys are forwarded', () => {
    const warnings = [];
    const warning = mock.method(console, 'warn', (message) => warnings.push(message));
    try {
      const result = sdk.buildContextOptions({ contextOptions: {
        locale: 'fr-FR', timezoneId: 'UTC', headless: false,
        executablePath: executable, env: { CHROMIX_TEST_OVERRIDE: 'context' },
      } });
      assert.equal(Object.hasOwn(result, 'locale'), false);
      assert.equal(Object.hasOwn(result, 'timezoneId'), false);
      assert.equal(result.headless, false);
      assert.equal(result.executablePath, executable);
      assert.deepEqual(result.env, { CHROMIX_TEST_OVERRIDE: 'context' });
      assert.deepEqual(warnings, ['[chromix] contextOptions.locale/timezoneId ignored — use top-level locale/timezone (binary flag)']);
    } finally {
      warning.mock.restore();
    }
  });

  test('proxy and CDP flags survive official argument construction without networking', async () => {
    const result = await build({
      proxy: 'http://unused.invalid:8080',
      launchOptions: {
        proxy: 'http://user:pass@selected.invalid:8081',
        args: ['--remote-debugging-address=127.0.0.1', '--remote-debugging-port=19222'],
      },
    });
    assert.deepEqual(result.proxy, { server: 'http://selected.invalid:8081', username: 'user', password: 'pass' });
    const actual = flags(result);
    assert.equal(actual.get('--remote-debugging-address'), '127.0.0.1');
    assert.equal(actual.get('--remote-debugging-port'), '19222');
    assert.equal(actual.get('--force-webrtc-ip-handling-policy'), 'disable_non_proxied_udp');
    assert.match(actual.get('--fingerprint'), /^[1-9][0-9]*$/);
    const direct = await build({ proxy: 'http://unused.invalid:8080', launchOptions: { proxy: null } });
    assert.equal(direct.proxy, null);
    assert.equal(flags(direct).has('--force-webrtc-ip-handling-policy'), false);
  });

  test('stealth false with nested args skips extension and maximization injection', async () => {
    const result = await build({
      stealthArgs: false, startMaximized: true, extensionPaths: [fileURLToPath(upstream)],
      timezone: 'UTC', locale: 'en-US', launchOptions: { args: ['--custom'] },
    });
    assert.deepEqual(result.args, ['--custom', '--fingerprint-timezone=UTC', '--lang=en-US', '--fingerprint-locale=en-US']);
  });

  test('fingerprint off aliases remove the injected platform but preserve explicit regional flags', async () => {
    for (const value of ['off', 'false', '0', 'disable', 'disabled']) {
      const result = await build({ args: [`--fingerprint=${value}`], timezone: 'UTC', locale: 'en-US' });
      const actual = flags(result);
      assert.equal(actual.get('--fingerprint'), 'off');
      assert.equal(actual.has('--fingerprint-platform'), false);
      assert.equal(actual.get('--fingerprint-timezone'), 'UTC');
      assert.equal(actual.get('--fingerprint-locale'), 'en-US');
    }
  });

  test('official profileSeed publishes and reuses one uint32 seed without launching', async () => {
    const profile = join(directory, 'persistent-profile');
    const seeds = await Promise.all(Array.from({ length: 4 }, () => profileSeed(profile)));
    assert.ok(Number.isInteger(seeds[0]) && seeds[0] > 0 && seeds[0] <= 0xffffffff);
    assert.ok(seeds.every((seed) => seed === seeds[0]));
    const path = join(profile, '.chromix-fingerprint-seed');
    const initial = await stat(path);
    assert.equal(await readFile(path, 'utf8'), `${seeds[0]}\n`);
    assert.equal(await profileSeed(profile), seeds[0]);
    assert.equal((await stat(path)).mtimeMs, initial.mtimeMs);
    assert.deepEqual(await readdir(profile), ['.chromix-fingerprint-seed']);
    if (process.platform !== 'win32') assert.equal(initial.mode & 0o777, 0o600);
    const built = await build({ args: [`--fingerprint=${seeds[0]}`] });
    assert.equal(flags(built).get('--fingerprint'), String(seeds[0]));
    await writeFile(path, '0\n');
    await assert.rejects(profileSeed(profile), { message: `Invalid Chromix profile seed file: ${path}` });
    assert.equal(await readFile(path, 'utf8'), '0\n');
  });

  test('humanConfig overrides the preset without conflating behavioral and fingerprint seeds', () => {
    const custom = { seed: 7, typingDelay: 12, mistype: 0 };
    const resolved = sdk.resolveHumanConfig('careful', custom);
    assert.equal(resolved.aimDelay, 200);
    for (const [key, value] of Object.entries(custom)) assert.equal(resolved[key], value);
    assert.equal(sdk.resolveHumanConfig('unknown').typingDelay, 70);
  });

  test('devicePool is measured admission, not a custom-field or host-CDP configuration', async () => {
    const pool = { python: 'unused-python', host: 'host.json', records: ['record.json'], seed: 18446744073709551615n };
    const measured = devicePool.measuredOptions({ devicePool: pool, headless: false, userDataDir: 'profile' });
    assert.equal(measured.python, 'unused-python');
    assert.equal(measured.pool.seed, '18446744073709551615');
    assert.equal(pool.seed, 18446744073709551615n);
    for (const [key, value] of Object.entries({
      args: ['--remote-debugging-port=19222'], proxy: null, viewport: null,
      humanize: false, locale: 'en-US', launchOptions: {}, contextOptions: {},
    })) {
      assert.throws(() => devicePool.measuredOptions({ devicePool: pool, [key]: value }), {
        message: `devicePool mode rejects launch/context override: ${key}`,
      });
    }
    const message = 'devicePool requires launchContext or launchPersistentContext for runtime verification';
    await assert.rejects(sdk.buildLaunchOptions({ devicePool: pool }), { message });
    assert.throws(() => sdk.buildContextOptions({ devicePool: pool }), { message });
    await assert.rejects(puppeteer.buildLaunchOptions({ devicePool: pool }), {
      message: 'Puppeteer devicePool admission is not implemented; use the measured Playwright entry point',
    });
  });
});
