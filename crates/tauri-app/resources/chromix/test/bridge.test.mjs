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
  binaryPath: '', skipDownload: false,
  proxy: { server: 'http://profile:8080', username: 'secret', password: 'not-on-argv' },
  extensionPaths: ['/extension/default'], startUrl: 'https://example.com',
});

test('preserves every SDK/custom option without mutating nested values', () => {
  const original = {
    userDataDir: '/custom/profile', headless: true, stealthArgs: false,
    humanize: true, humanPreset: 'careful', humanConfig: { seed: 7, custom: [0, null, false] },
    geoip: false, timezone: 'Asia/Shanghai', locale: 'zh-CN',
    viewport: null, userAgent: 'Custom UA', startMaximized: false,
    proxy: { server: 'socks5://explicit:8081', password: 'top-secret' },
    extensionPaths: [], browserVersion: '152', releaseChannel: 'latest',
    args: ['--custom-flag=unchanged', '--fingerprint-storage-quota=42'],
    launchOptions: { env: { SECRET: 'token' }, args: ['--other-flag'], timeout: 90000 },
    contextOptions: { permissions: ['clipboard-read'], args: ['--context-flag'], custom: { a: [1, 2] } },
    futureSdkField: { arbitrary: ['preserved', null, 9] },
  };
  const snapshot = structuredClone(original);
  const actual = prepareOptions(request(original));
  for (const [key, value] of Object.entries(original)) {
    if (!['args', 'launchOptions', 'contextOptions'].includes(key)) assert.deepEqual(actual[key], value);
  }
  const reserved = ['--remote-debugging-address=127.0.0.1', '--remote-debugging-port=19222'];
  assert.deepEqual(actual.args, [...original.args, ...reserved]);
  assert.deepEqual(actual.launchOptions, { ...original.launchOptions, args: [...original.launchOptions.args, ...reserved] });
  assert.deepEqual(actual.contextOptions, { ...original.contextOptions, args: [...original.contextOptions.args, ...reserved] });
  assert.deepEqual(original, snapshot);
});

test('default persona stays SDK-native without legacy fingerprint/geoip defaults', () => {
  const options = prepareOptions(request());
  assert.equal(options.headless, false);
  assert.equal(options.userDataDir, '/tmp/chromix-test-profile');
  assert.deepEqual(options.extensionPaths, ['/extension/default']);
  assert.deepEqual(options.proxy, request().proxy);
  for (const key of ['stealthArgs', 'geoip', 'timezone', 'locale', 'humanize', 'userAgent']) {
    assert.equal(Object.hasOwn(options, key), false);
  }
  assert.deepEqual(options.args, ['--remote-debugging-address=127.0.0.1', '--remote-debugging-port=19222']);
});

test('reserved flags fail in every effective argument and ignoreDefaultArgs layer', () => {
  for (const flag of [
    '--remote-debugging-port=9876', '--remote-debugging-port', '--remote-debugging-address=0.0.0.0',
    '--remote-debugging-pipe', '--remote-debugging-socket-name=x', '--user-data-dir=/shared',
    '--user-data-dir', '-user-data-dir=/shared', ' --profile-directory=Default',
  ]) {
    for (const layer of ['top', 'launchOptions', 'contextOptions']) {
      for (const key of ['args', 'ignoreDefaultArgs']) {
        const options = layer === 'top' ? { [key]: [flag] } : { [layer]: { [key]: [flag] } };
        assert.throws(() => prepareOptions(request(options)), /reserved debugging\/user-data-dir\/profile-directory/);
      }
    }
  }
  for (const layer of ['launchOptions', 'contextOptions']) {
    assert.throws(() => prepareOptions(request({ [layer]: { userDataDir: '/bad' } })), /top-level userDataDir/);
  }
  assert.throws(() => prepareOptions(request({ cdpPort: 0 })), /host-controlled/);
  assert.throws(() => prepareOptions({ ...request(), cdpPort: 0 }), /Host cdpPort/);
});

test('explicit proxy at any SDK layer or args takes precedence over profile proxy', () => {
  for (const value of [null, false, '', { server: 'http://user:9090' }]) {
    const options = prepareOptions(request({ proxy: value }));
    assert.deepEqual(options.proxy, value);
  }
  for (const key of ['launchOptions', 'contextOptions']) {
    const options = prepareOptions(request({ [key]: { proxy: { server: 'http://nested' } } }));
    assert.equal(Object.hasOwn(options, 'proxy'), false);
    assert.equal(options[key].proxy.server, 'http://nested');
  }
  const options = prepareOptions(request({ args: ['--proxy-server=http://explicit'] }));
  assert.equal(Object.hasOwn(options, 'proxy'), false);
});

test('explicit extension/headless choices are not replaced by defaults', () => {
  assert.deepEqual(prepareOptions(request({ extensionPaths: [] })).extensionPaths, []);
  assert.equal(Object.hasOwn(prepareOptions(request({ args: ['--disable-extensions'] })), 'extensionPaths'), false);
  const options = prepareOptions(request({ launchOptions: { headless: true, args: ['--custom'] } }));
  assert.equal(Object.hasOwn(options, 'headless'), false);
  assert.equal(options.launchOptions.headless, true);
  assert.ok(options.launchOptions.args.includes('--load-extension=/extension/default'));
});

test('measured and alternative adapter boundaries fail instead of silently doing native launches', () => {
  for (const mode of ['measured', 'unknown', null, true]) {
    assert.throws(() => prepareOptions(request({ mode })), /measured mode/);
  }
  assert.throws(() => prepareOptions(request({ adapter: 'puppeteer' })), /only the playwright adapter/);
  assert.throws(
    () => prepareOptions(request({ mode: 'native', adapter: 'playwright', devicePool: ['opaque'] })),
    /devicePool is not available through the Cloaksession CDP sidecar/
  );
});

test('binary precedence is context > launch > host binary > environment, with SDK auto resolution otherwise', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'chromix-binary-'));
  try {
    const paths = await Promise.all(['context', 'launch', 'host', 'environment'].map(async (name) => {
      const path = join(directory, name);
      await writeFile(path, 'fake binary', { mode: 0o755 });
      return path;
    }));
    const sdk = { binaryInfo() { throw new Error('must not consult cache'); } };
    const options = { contextOptions: { executablePath: paths[0] }, launchOptions: { executablePath: paths[1] } };
    const launchRequest = { ...request(), binaryPath: paths[2], skipDownload: true };
    const env = { CLOAKBROWSER_BINARY_PATH: paths[3] };
    assert.equal(await configureBinary(launchRequest, options, sdk, env), paths[0]);
    delete options.contextOptions;
    assert.equal(await configureBinary(launchRequest, options, sdk, env), paths[1]);
    delete options.launchOptions;
    assert.equal(await configureBinary(launchRequest, options, sdk, env), paths[2]);
    launchRequest.binaryPath = '';
    env.CLOAKBROWSER_BINARY_PATH = paths[3];
    assert.equal(await configureBinary(launchRequest, options, sdk, env), paths[3]);
    const cleanEnv = {};
    assert.equal(await configureBinary(request(), {}, sdk, cleanEnv), undefined);
    assert.deepEqual(cleanEnv, {});
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test('skip_download only uses validated local/cache paths and never invokes ensureBinary', async () => {
  const options = { releaseChannel: 'latest', browserVersion: '152' };
  let calls = 0;
  const sdk = {
    binaryInfo(value) { calls++; assert.equal(value, options); return { installed: true, path: process.execPath }; },
    ensureBinary() { throw new Error('download forbidden'); },
  };
  const env = {};
  assert.equal(await configureBinary({ ...request(), skipDownload: true }, options, sdk, env), process.execPath);
  assert.equal(env.CLOAKBROWSER_BINARY_PATH, process.execPath);
  assert.equal(calls, 1);
  await assert.rejects(configureBinary({ ...request(), skipDownload: true }, {}, {
    binaryInfo: () => ({ installed: false, path: null }),
  }, {}), /downloading is disabled/);
  await assert.rejects(configureBinary({ ...request(), binaryPath: '/nonexistent/chromix' }, {}, sdk, {}), /missing or not executable/);
  for (const path of ['', null, false]) {
    await assert.rejects(configureBinary({ ...request(), skipDownload: true }, {
      launchOptions: { executablePath: path },
    }, sdk, {}), /non-empty string/);
  }
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
    loadSdk: async () => ({ launchPersistentContext: async (options) => { actualOptions = options; return context; } }),
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

test('ready follows context/CDP readiness and close calls SDK context.close once', async (t) => {
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

test('EOF closes SDK context normally', async (t) => {
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
  const h = await harness(t, { loadSdk: async () => ({
    launchPersistentContext: () => new Promise((resolve) => { finishLaunch = resolve; }),
  }) });
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
  const bad = await harness(t, { loadSdk: async () => { throw new Error('SDK missing'); } });
  bad.launch();
  await bad.done;
  assert.equal(bad.messages[0].message, 'SDK missing');
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
  assert.equal(explicit.navigations.length, 0);
  explicit.send({ type: 'close' });
  await explicit.done;
});

test('a closed browser emits closed, and a stuck SDK close reaches bounded cleanup', async (t) => {
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
  assert.equal(stuck.messages.at(-1).message, 'Chromix SDK shutdown timed out');
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
