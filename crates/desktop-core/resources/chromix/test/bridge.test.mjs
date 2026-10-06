import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { PassThrough } from 'node:stream';
import test from 'node:test';
import { configureBinary, prepareOptions, runBridge, waitForCdp } from '../bridge.mjs';

const request = (options = {}) => ({
  type: 'launch', options, cdpPort: 19222, userDataDir: '/tmp/chromix-test-profile',
  binaryPath: process.execPath, skipDownload: false,
  proxy: { server: 'http://profile:8080', username: 'secret', password: 'not-on-argv' },
  extensionPaths: ['/extension/default'], startUrl: 'https://example.com',
});

async function harness(t, overrides = {}) {
  const directory = await mkdtemp(join(tmpdir(), 'chromix-bridge-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const input = new PassThrough();
  const messages = [];
  const context = new EventEmitter();
  const navigations = [];
  let closeCount = 0;
  let actualOptions;
  context.pages = () => [{ url: () => 'about:blank', goto: async (...args) => navigations.push(args) }];
  context.close = async () => { closeCount++; context.emit('close'); };
  const done = runBridge({
    input, send: (message) => messages.push(message), env: {}, ready: async () => {},
    loadPlaywright: async () => ({ chromium: { launchPersistentContext: async (userDataDir, options) => { actualOptions = {userDataDir, ...options}; return context; } } }),
    ...overrides,
  });
  const send = (message) => input.write(`${JSON.stringify(message)}\n`);
  const until = async (type) => {
    for (let i = 0; i < 200; i++) {
      const message = messages.find((message) => message.type === type);
      if (message) return message;
      await new Promise((resolve) => setTimeout(resolve, 5));
    }
    assert.fail(`Missing ${type}: ${JSON.stringify(messages)}`);
  };
  t.after(() => input.end());
  return { input, messages, context, done, send, until, navigations,
    get options() { return actualOptions; }, get closeCount() { return closeCount; },
    launch: (options = {}, extra = {}) => send({ ...request(options), userDataDir: directory, ...extra }),
  };
}

test('ready follows context/CDP readiness and close calls Playwright context.close once', async (t) => {
  let allowReady;
  const h = await harness(t, { ready: () => new Promise((resolve) => { allowReady = resolve; }) });
  h.launch();
  while (!allowReady) await new Promise((resolve) => setImmediate(resolve));
  assert.equal(h.messages.length, 0);
  allowReady();
  const message = await h.until('ready');
  assert.equal(message.cdpEndpoint, 'http://127.0.0.1:19222');
  assert.equal(h.navigations[0][0], 'https://example.com');
  h.send({ type: 'close' });
  await h.done;
  assert.equal(h.closeCount, 1);
  assert.equal(h.messages.at(-1).type, 'closed');
});

test('EOF closes Playwright context normally', async (t) => {
  const h = await harness(t);
  h.launch();
  await h.until('ready');
  h.input.end();
  await h.done;
  assert.equal(h.closeCount, 1);
});

test('EOF during an in-flight launch closes the eventual context without ready', async (t) => {
  let finishLaunch;
  let closes = 0;
  const h = await harness(t, { loadPlaywright: async () => ({ chromium: {
    launchPersistentContext: () => new Promise((resolve) => { finishLaunch = resolve; }),
  } }) });
  h.launch();
  while (!finishLaunch) await new Promise((resolve) => setImmediate(resolve));
  h.input.end();
  finishLaunch({ close: async () => { closes++; } });
  await h.done;
  assert.equal(closes, 1);
  assert.equal(h.messages.some((message) => message.type === 'ready'), false);
});

test('launch errors and CDP errors report JSON and clean up contexts', async (t) => {
  const h = await harness(t, { ready: async () => { throw new Error('CDP unavailable'); } });
  h.launch();
  await h.done;
  assert.deepEqual(h.messages[0], { type: 'error', message: 'CDP unavailable' });
  assert.equal(h.closeCount, 1);
  const bad = await harness(t, { loadPlaywright: async () => { throw new Error('Playwright missing'); } });
  bad.launch();
  await bad.done;
  assert.equal(bad.messages[0].message, 'Playwright missing');
});

test('restored pages and explicit positional start URLs override the profile start URL', async (t) => {
  const h = await harness(t);
  h.context.pages = () => [{ url: () => 'https://restored.example', goto: () => assert.fail('must keep restored page') }];
  h.launch();
  await h.until('ready');
  h.send({ type: 'close' });
  await h.done;
  const explicit = await harness(t);
  explicit.launch({ args: ['https://explicit.example'] });
  await explicit.until('ready');
  assert.equal(explicit.navigations[0][0], 'https://explicit.example');
  assert.ok(!explicit.options.args.includes('https://explicit.example'));
  explicit.send({ type: 'close' });
  await explicit.done;
});

test('a closed browser emits closed, and a stuck Playwright close reaches bounded cleanup', async (t) => {
  const h = await harness(t);
  h.launch();
  await h.until('ready');
  h.context.emit('close');
  await h.done;
  assert.equal(h.messages.at(-1).type, 'closed');
  let forced;
  const stuck = await harness(t, { closeTimeoutMs: 20, forceExit: (code) => { forced = code; } });
  stuck.context.close = () => new Promise(() => {});
  stuck.launch();
  await stuck.until('ready');
  stuck.input.end();
  await stuck.done;
  assert.equal(forced, 1);
  assert.equal(stuck.messages.at(-1).message, 'Playwright shutdown timed out');
});

test('invalid JSON does not launch and CDP cancellation is bounded', async (t) => {
  const h = await harness(t);
  h.input.write('not JSON\n');
  await h.done;
  assert.equal(h.options, undefined);
  assert.equal(h.messages[0].message, 'Invalid bridge JSON request');
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(waitForCdp('http://127.0.0.1:1', controller.signal), /cancelled/);
});

test('maps legacy shorthand and nested options without mutating stored options', () => {
  const original = { headless: true, timezone: 'Asia/Shanghai', locale: 'zh-CN',
    extensionPaths: ['/custom/extension'], startMaximized: true,
    args: ['--custom=1'], launchOptions: { timeout: 80000, args: ['--launch=2'] },
    contextOptions: { permissions: ['clipboard-read'], args: ['--context=3'] } };
  const snapshot = structuredClone(original);
  const actual = prepareOptions(request(original));
  assert.equal(actual.headless, true);
  assert.equal(actual.timezoneId, 'Asia/Shanghai');
  assert.equal(actual.locale, 'zh-CN');
  assert.deepEqual(actual.permissions, ['clipboard-read']);
  assert.equal(actual.timeout, 80000);
  for (const arg of ['--custom=1', '--launch=2', '--context=3', '--start-maximized',
    '--load-extension=/custom/extension', '--fingerprint-timezone=Asia/Shanghai',
    '--remote-debugging-address=127.0.0.1', '--remote-debugging-port=19222']) assert.ok(actual.args.includes(arg), arg);
  assert.ok(actual.ignoreDefaultArgs.includes('--disable-extensions'));
  for (const key of ['launchOptions', 'contextOptions', 'extensionPaths', 'timezone']) assert.equal(actual[key], undefined);
  assert.deepEqual(original, snapshot);
});

test('uint64 fixed and custom seeds are exact and remove only conflicting seed flags', () => {
  for (const fingerprintMode of ['fixed', 'custom']) {
    const original = { fingerprintMode, fingerprintSeed: '18446744073709551615',
      args: ['--fingerprint=7', '--uxr-fingerprint-seed=8', '--fingerprint-noise=false', '--uxr-audio-seed=19'],
      launchOptions: { args: ['--fingerprint', '77', '--fingerprint-screen-width=1440'] },
      contextOptions: { args: ['--fingerprint-seed=123', '--uxr-gpu-vendor=custom'] } };
    const actual = prepareOptions(request(original));
    assert.equal(actual.args.filter((arg) => /^--(?:fingerprint(?:-seed)?|uxr-fingerprint-seed)=/.test(arg)).length, 1);
    assert.ok(actual.args.includes('--fingerprint=18446744073709551615'));
    for (const arg of ['--fingerprint-noise=false', '--uxr-audio-seed=19', '--fingerprint-screen-width=1440', '--uxr-gpu-vendor=custom']) assert.ok(actual.args.includes(arg));
    assert.equal(actual.args.includes('77'), false);
    assert.equal(original.args[0], '--fingerprint=7');
  }
  for (const fingerprintSeed of [0, 17, 9007199254740992, '', '-1', '0', '18446744073709551616', '1.5']) {
    assert.throws(() => prepareOptions(request({ fingerprintMode: 'fixed', fingerprintSeed })), /decimal string/);
  }
  assert.throws(() => prepareOptions(request({ fingerprintMode: 'fixed' })), /fingerprintSeed/);
});

test('random mode uses a new crypto seed on each launch without changing persisted data', () => {
  const original = request({ fingerprintMode: 'random', fingerprintSeed: '9', args: ['--fingerprint=5', '--fingerprint-noise=false'] });
  const snapshot = structuredClone(original);
  const values = new Set();
  for (let i = 0; i < 24; i++) {
    const actual = prepareOptions(original);
    const value = actual.args.find((arg) => arg.startsWith('--fingerprint=')).slice(14);
    assert.ok(BigInt(value) > 0n && BigInt(value) <= 0xffffffffffffffffn);
    values.add(value);
    assert.ok(actual.args.includes('--fingerprint-noise=false'));
  }
  assert.equal(values.size, 24);
  assert.deepEqual(original, snapshot);
});

test('legacy raw fingerprint and uxr aliases win over profile defaults', () => {
  const input = { ...request({ args: ['--fingerprint=123', '--uxr-screen-width=900', '--fingerprint-noise=false'] }),
    fingerprint: { seed: '18446744073709551615', locale: 'en-US', timezone: 'UTC',
      screen: { width: 1920, height: 1080 }, hardwareConcurrency: 12, storageQuota: 2147483648 } };
  const actual = prepareOptions(input);
  assert.ok(actual.args.includes('--fingerprint=123'));
  assert.ok(actual.args.includes('--uxr-screen-width=900'));
  assert.ok(!actual.args.includes('--fingerprint-screen-width=1920'));
  assert.ok(actual.args.includes('--fingerprint-hardware-concurrency=12'));
  assert.ok(actual.args.includes('--fingerprint-storage-quota=2048'));
  input.options = {};
  assert.ok(prepareOptions(input).args.includes('--fingerprint=18446744073709551615'));
  input.options = { stealthArgs: false };
  assert.ok(!prepareOptions(input).args.some((arg) => arg.startsWith('--fingerprint')));
});

test('reserved flags fail in every argument and ignoreDefaultArgs layer', () => {
  for (const flag of ['--remote-debugging-port=9876', '--remote-debugging-port', '--remote-debugging-address=0.0.0.0',
    '--remote-debugging-pipe', '--remote-debugging-socket-name=x', '--user-data-dir=/shared', '-user-data-dir=/shared', ' --profile-directory=Default']) {
    for (const layer of ['top', 'launchOptions', 'contextOptions']) {
      for (const key of ['args', 'ignoreDefaultArgs']) {
        const options = layer === 'top' ? { [key]: [flag] } : { [layer]: { [key]: [flag] } };
        assert.throws(() => prepareOptions(request(options)), /reserved debugging\/user-data-dir\/profile-directory/);
      }
    }
  }
  assert.throws(() => prepareOptions(request({ contextOptions: { userDataDir: '/bad' } })), /top-level userDataDir/);
  assert.throws(() => prepareOptions(request({ cdpPort: 0 })), /host-controlled/);
  assert.throws(() => prepareOptions({ ...request(), cdpPort: 0 }), /Host cdpPort/);
});

test('SDK-only and unknown options fail with actionable migration errors', () => {
  for (const options of [{ humanize: true }, { geoip: true }, { browserVersion: '152' }, { devicePool: [] },
    { humanConfig: {} }, { fontsDir: '/fonts' }, { releaseChannel: 'latest' }]) {
    assert.throws(() => prepareOptions(request(options)), /removed Chromix SDK; remove this option/);
  }
  assert.throws(() => prepareOptions(request({ futureSdkField: true })), /documented Playwright/);
  assert.throws(() => prepareOptions(request({ mode: 'measured' })), /measured mode/);
  assert.throws(() => prepareOptions(request({ adapter: 'puppeteer' })), /adapter: playwright/);
  prepareOptions(request({ humanize: false, geoip: false, mode: 'native', adapter: 'playwright' }));
});

test('proxy and extension overrides retain precedence', () => {
  for (const proxy of [null, false, '']) assert.equal(prepareOptions(request({ proxy })).proxy, undefined);
  assert.equal(prepareOptions(request({ args: ['--proxy-server=http://explicit'] })).proxy, undefined);
  assert.equal(prepareOptions(request({ proxy: 'http://user:password@host:80' })).proxy.password, 'password');
  assert.equal(prepareOptions(request({ contextOptions: { proxy: { server: 'http://nested' } } })).proxy.server, 'http://nested');
  assert.throws(() => prepareOptions(request({ proxy: { server: 'socks5://host:1080', username: 'secret' } })), /Rust proxy bridge/);
  assert.ok(!prepareOptions(request({ extensionPaths: [] })).args.some((arg) => arg.startsWith('--load-extension')));
  assert.ok(!prepareOptions(request({ args: ['--disable-extensions'] })).args.some((arg) => arg.startsWith('--load-extension')));
  assert.ok(prepareOptions(request({ ignoreDefaultArgs: false })).ignoreDefaultArgs.includes('--disable-extensions'));
});

test('binary precedence is context > launch > top-level > host > environment > installed Playwright', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'playwright-binaries-'));
  try {
    const paths = await Promise.all(['context', 'launch', 'top', 'host', 'env', 'installed'].map(async (name) => {
      const path = join(directory, name); await writeFile(path, 'fake', { mode: 0o755 }); return path;
    }));
    const source = { executablePath: paths[2], launchOptions: { executablePath: paths[1] }, contextOptions: { executablePath: paths[0] } };
    const launch = { ...request(source), binaryPath: paths[3] };
    const chromium = { executablePath: () => paths[5], download: () => assert.fail('no download') };
    const env = { CLOAKBROWSER_BINARY_PATH: paths[4] };
    for (let i = 0; i < 6; i++) {
      const actual = prepareOptions(launch);
      assert.equal(await configureBinary(launch, actual, chromium, env), paths[i]);
      assert.equal(actual.executablePath, paths[i]);
      if (i === 0) delete source.contextOptions;
      if (i === 1) delete source.launchOptions;
      if (i === 2) delete source.executablePath;
      if (i === 3) launch.binaryPath = '';
      if (i === 4) delete env.CLOAKBROWSER_BINARY_PATH;
    }
    for (const skipDownload of [true, false]) {
      await assert.rejects(configureBinary({ ...request(), binaryPath: '', skipDownload }, {}, { executablePath: () => '/missing/playwright' }, {}), /automatic downloading is disabled/);
    }
    await assert.rejects(configureBinary(request(), { executablePath: '' }, chromium, {}), /non-empty string/);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test('legacy off and alias overrides are preserved even with shorthand locale', () => {
  const off = prepareOptions({ ...request({ args: ['--fingerprint=off'] }), fingerprint: { screen: { width: 1000 } } });
  assert.deepEqual(off.args.filter((arg) => arg.startsWith('--fingerprint')), ['--fingerprint=off']);
  const original = request({ fingerprintMode: 'fixed', fingerprintSeed: '42', locale: 'en-US',
    args: ['--uxr-locale=zh-CN', '--fingerprint', 'https://example.com', '--load-extension=/explicit'] });
  const actual = prepareOptions(original);
  assert.ok(actual.args.includes('--uxr-locale=zh-CN'));
  assert.ok(!actual.args.includes('--fingerprint-locale=en-US'));
  assert.ok(actual.args.includes('https://example.com'));
  assert.ok(actual.ignoreDefaultArgs.includes('--disable-extensions'));
});

test('random and fixed are seed-only; custom and legacy retain profile defaults', () => {
  const fingerprint = { seed: '456', locale: 'zh-CN', timezone: 'Asia/Shanghai', platform: 'Win32',
    screen: { width: 2560, height: 1440 }, hardwareConcurrency: 16, deviceMemory: 16,
    webgl: { vendor: 'profile vendor', renderer: 'profile renderer' }, storageQuota: 2147483648 };
  for (const fingerprintMode of ['random', 'fixed']) {
    const actual = prepareOptions({ ...request({ fingerprintMode, fingerprintSeed: '123' }), fingerprint });
    const flags = actual.args.filter((arg) => /^--(?:fingerprint|uxr)/.test(arg));
    assert.equal(flags.length, 1);
    assert.match(flags[0], /^--fingerprint=[1-9][0-9]*$/);
    const explicit = prepareOptions({ ...request({ fingerprintMode, fingerprintSeed: '123',
      args: ['--fingerprint-hardware-concurrency=4', '--uxr-screen-width=800', '--fingerprint-storage-quota=7'] }), fingerprint });
    for (const arg of ['--fingerprint-hardware-concurrency=4', '--uxr-screen-width=800', '--fingerprint-storage-quota=7']) assert.ok(explicit.args.includes(arg));
    assert.ok(!explicit.args.includes('--fingerprint-screen-height=1440'));
  }
  for (const options of [{}, { fingerprintMode: 'custom', fingerprintSeed: '123' }]) {
    const actual = prepareOptions({ ...request(options), fingerprint });
    assert.ok(actual.args.includes('--fingerprint-screen-width=2560'));
    assert.ok(actual.args.includes('--fingerprint-hardware-concurrency=16'));
    assert.ok(actual.args.includes('--fingerprint-gpu-vendor=profile vendor'));
  }
});

test('stored byte quotas convert to Chromix integer MiB; raw public flag units stay untouched', () => {
  for (const [bytes, mib] of [[2147483648, 2048], [1048576, 1], [1048577, 2], [1, 1]]) {
    const input = { ...request({ fingerprintMode: 'custom', fingerprintSeed: '9' }), fingerprint: { storageQuota: bytes } };
    assert.ok(prepareOptions(input).args.includes(`--fingerprint-storage-quota=${mib}`));
    assert.equal(input.fingerprint.storageQuota, bytes);
  }
  for (const flag of ['--fingerprint-storage-quota=102400', '--uxr-storage-quota=0']) {
    const actual = prepareOptions({ ...request({ args: [flag] }), fingerprint: { storageQuota: 2147483648 } });
    assert.ok(actual.args.includes(flag));
    assert.ok(!actual.args.includes('--fingerprint-storage-quota=2048'));
  }
});
