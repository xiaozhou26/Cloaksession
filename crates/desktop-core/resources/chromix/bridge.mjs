import { access, mkdir, stat } from 'node:fs/promises';
import { constants } from 'node:fs';
import { randomBytes, createHash } from 'node:crypto';
import { createInterface } from 'node:readline';
import { pathToFileURL } from 'node:url';

const own = (object, key) => Object.hasOwn(object, key);
const object = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);
const reserved = /^--?(?:remote-debugging(?:-[^=\s]+)?|user-data-dir|profile-directory)(?:=|\s|$)/i;
const extensionFlag = /^--?(?:load-extension|disable-extensions(?:-except)?)(?:=|\s|$)/i;
const proxyFlag = /^--?(?:proxy-server|proxy-pac-url|no-proxy-server)(?:=|\s|$)/i;
const seedFlags = new Set(['--fingerprint', '--fingerprint-seed', '--uxr-fingerprint-seed']);
const keyOf = (arg) => arg.trim().split(/[=\s]/, 1)[0];
const aliasOf = (arg) => {
  const key = keyOf(arg);
  return seedFlags.has(key) ? 'seed' : key.replace(/^--(?:fingerprint|uxr)-/, 'persona-');
};
const playwrightKeys = new Set(`acceptDownloads args baseURL bypassCSP channel chromiumSandbox clientCertificates
  colorScheme contrast deviceScaleFactor downloadsPath env executablePath extraHTTPHeaders forcedColors
  geolocation handleSIGHUP handleSIGINT handleSIGTERM hasTouch headless httpCredentials ignoreDefaultArgs
  ignoreHTTPSErrors isMobile javaScriptEnabled locale logger offline permissions proxy recordHar recordVideo
  reducedMotion screen serviceWorkers slowMo strictSelectors timeout timezoneId tracesDir userAgent viewport`.split(/\s+/));
const shorthandKeys = new Set(['userDataDir', 'launchOptions', 'contextOptions', 'timezone', 'extensionPaths',
  'startMaximized', 'stealthArgs', 'mode', 'adapter', 'fingerprintMode', 'fingerprintSeed']);
const sdkOnly = new Set(['devicePool', 'humanize', 'humanPreset', 'humanConfig', 'geoip', 'browserVersion',
  'releaseChannel', 'fontsDir', 'licenseKey', 'widevine']);

function seed(value) {
  if (typeof value !== 'string' || !/^[0-9]+$/.test(value) || BigInt(value) < 1n || BigInt(value) > 0xffffffffffffffffn) {
    throw new Error('fingerprintSeed must be a decimal string from 1 to 18446744073709551615; 0 disables fingerprinting in Chromix');
  }
  return BigInt(value).toString();
}

function removeSeeds(args) {
  const result = [];
  for (let i = 0; i < args.length; i++) {
    if (!seedFlags.has(keyOf(args[i]))) { result.push(args[i]); continue; }
    if (!/[=\s]/.test(args[i].trim().slice(2)) && /^(?:[0-9]+|off|false|disable|disabled)$/i.test(args[i + 1] ?? '')) i++;
  }
  return result;
}

function profileArgs(request) {
  const fp = request.fingerprint;
  if (!object(fp)) return [];
  const entries = {
    locale: fp.locale, timezone: fp.timezone, 'user-agent': fp.userAgent,
    'screen-width': fp.screen?.width, 'screen-height': fp.screen?.height,
    'hardware-concurrency': fp.hardwareConcurrency, 'device-memory': fp.deviceMemory,
    'gpu-vendor': fp.webgl?.vendor, 'gpu-renderer': fp.webgl?.renderer,
    'fonts-dir': fp.fontsDir,
    'platform-version': fp.clientHints?.secChUaPlatformVersion,
  };
  const platform = { MacIntel: 'macos', Win32: 'windows', 'Linux x86_64': 'linux' }[fp.platform];
  if (platform) entries.platform = platform;
  // Chromix's patched switch accepts MiB; stored profile quotas are bytes.
  if (fp.storageQuota > 0) entries['storage-quota'] = Math.ceil(fp.storageQuota / 1048576);
  return Object.entries(entries).filter(([, value]) => value !== undefined && value !== null && value !== '')
    .map(([key, value]) => `--fingerprint-${key}=${value}`);
}

export function prepareOptions(request) {
  if (!object(request) || request.type !== 'launch') throw new Error('Expected a launch request');
  if (!Number.isInteger(request.cdpPort) || request.cdpPort < 1 || request.cdpPort > 65535) {
    throw new Error('Host cdpPort must be an integer between 1 and 65535');
  }
  if (!object(request.options)) throw new Error('Playwright options must be an object');
  const original = structuredClone(request.options);
  for (const name of ['launchOptions', 'contextOptions']) {
    if (own(original, name) && !object(original[name])) throw new Error(`${name} must be an object`);
  }
  const layers = [['options', original], ['launchOptions', original.launchOptions], ['contextOptions', original.contextOptions]];
  let args = [];
  for (const [name, layer] of layers) {
    if (!layer) continue;
    for (const [key, value] of Object.entries(layer)) {
      if (['cdpPort', 'debuggingPort', 'remoteDebuggingPort', 'remoteDebuggingAddress'].includes(key)) {
        throw new Error(`${name}.${key} is reserved: CDP is host-controlled on 127.0.0.1`);
      }
      if (name !== 'options' && shorthandKeys.has(key)) {
        throw new Error(`${name}.${key}: use top-level ${key} instead`);
      }
      if (sdkOnly.has(key)) {
        if (['humanize', 'geoip', 'widevine'].includes(key) && value === false) continue;
        throw new Error(`${name}.${key} requires the removed Chromix SDK; remove this option and use Playwright options, raw browser args, or a local executablePath instead`);
      }
      if (!playwrightKeys.has(key) && !shorthandKeys.has(key)) {
        throw new Error(`Unsupported ${name}.${key}; remove it or use a documented Playwright launchPersistentContext option`);
      }
    }
    for (const key of ['args', 'ignoreDefaultArgs']) {
      if (!own(layer, key)) continue;
      if (key === 'ignoreDefaultArgs' && typeof layer[key] === 'boolean') continue;
      if (!Array.isArray(layer[key]) || layer[key].some((arg) => typeof arg !== 'string' || arg.includes('\0'))) {
        throw new Error(`${name}.${key} must be an array of strings without NUL`);
      }
      if (layer[key].some((arg) => reserved.test(arg.trim()))) {
        throw new Error(`${name}.${key} contains a reserved debugging/user-data-dir/profile-directory argument; use top-level userDataDir and host CDP`);
      }
    }
    args.push(...(layer.args ?? []));
  }
  if (own(original, 'mode') && original.mode !== 'native') throw new Error('Remove mode: measured mode requires the removed SDK; direct Playwright supports native mode only');
  if (own(original, 'adapter') && original.adapter !== 'playwright') throw new Error('Use adapter: playwright; other SDK adapters are no longer supported');
  const options = { ...original, ...original.launchOptions, ...original.contextOptions };
  for (const key of [...shorthandKeys, ...sdkOnly]) delete options[key];
  options.userDataDir = original.userDataDir ?? request.userDataDir;
  if (typeof options.userDataDir !== 'string' || !options.userDataDir.trim()) throw new Error('userDataDir must be a non-empty path');
  options.headless ??= false;
  options.viewport ??= null;
  if (own(original, 'timezone') && !own(options, 'timezoneId')) options.timezoneId = original.timezone;
  const explicitProxy = layers.some(([, layer]) => layer && own(layer, 'proxy')) || args.some((arg) => proxyFlag.test(arg.trim()));
  if (!explicitProxy && request.proxy) options.proxy = structuredClone(request.proxy);
  if (options.proxy === false || options.proxy === null || options.proxy === '') delete options.proxy;
  if (typeof options.proxy === 'string') {
    const url = new URL(options.proxy);
    options.proxy = { server: `${url.protocol}//${url.host}`,
      ...(url.username ? { username: decodeURIComponent(url.username) } : {}),
      ...(url.password ? { password: decodeURIComponent(url.password) } : {}) };
  }
  if (options.proxy) {
    if (!object(options.proxy) || typeof options.proxy.server !== 'string') throw new Error('proxy must be a Playwright proxy object or URL');
    if (/^socks5:\/\//i.test(options.proxy.server) && (options.proxy.username || options.proxy.password)) {
      throw new Error('Authenticated SOCKS5 must be passed through the desktop Rust proxy bridge; configure the profile proxy or launch via desktop-core');
    }
  }
  const extensions = own(original, 'extensionPaths') ? original.extensionPaths : request.extensionPaths;
  if (extensions !== undefined && (!Array.isArray(extensions) || extensions.some((path) => typeof path !== 'string'))) {
    throw new Error('extensionPaths must be an array of paths');
  }
  if (extensions?.length && !args.some((arg) => extensionFlag.test(arg.trim()))) {
    args.push(`--load-extension=${extensions.join(',')}`, `--disable-extensions-except=${extensions.join(',')}`);
  }
  if (args.some((arg) => /^--load-extension(?:=|$)/.test(arg)) && options.ignoreDefaultArgs !== true) {
    options.ignoreDefaultArgs = [...new Set([...(Array.isArray(options.ignoreDefaultArgs) ? options.ignoreDefaultArgs : []), '--disable-extensions'])];
  }
  if (original.startMaximized && !args.some((arg) => /^--(?:start-maximized|window-size|window-position)(?:=|$)/.test(arg))) args.push('--start-maximized');
  const mode = original.fingerprintMode;
  if (mode !== undefined && !['random', 'fixed', 'custom'].includes(mode)) throw new Error('fingerprintMode must be random, fixed, or custom');
  if (mode === undefined && own(original, 'fingerprintSeed')) throw new Error('Set fingerprintMode to fixed or custom when specifying fingerprintSeed');
  if (mode !== undefined) {
    const value = mode === 'random' ? (randomBytes(8).readBigUInt64BE() || 1n).toString() : seed(original.fingerprintSeed);
    args = [...removeSeeds(args), `--fingerprint=${value}`];
  }
  const fingerprintOff = args.some((arg) => /^--fingerprint=(?:off|false|0|disable|disabled)$/i.test(arg));
  const enabled = mode !== undefined || (original.stealthArgs !== false && !fingerprintOff);
  if (enabled) {
    const overrides = new Set(args.map(aliasOf));
    // Simple modes leave persona selection to the patched browser seed.
    const defaults = (mode === undefined || mode === 'custom' ? profileArgs(request) : [])
      .filter((arg) => !overrides.has(aliasOf(arg)));
    if (!overrides.has('seed')) {
      const saved = request.fingerprint?.seed;
      const value = typeof saved === 'string' && /^[0-9]+$/.test(saved) && BigInt(saved) > 0n && BigInt(saved) <= 0xffffffffffffffffn
        ? saved : (createHash('sha256').update(saved || request.profileId || request.userDataDir).digest().readBigUInt64BE() || 1n).toString();
      defaults.unshift(`--fingerprint=${value}`);
    }
    args = [...defaults, ...args];
    for (const [key, value] of [['locale', options.locale], ['timezone', options.timezoneId], ['user-agent', options.userAgent]]) {
      if (value !== undefined && !overrides.has(`persona-${key}`)) {
        args = args.filter((arg) => aliasOf(arg) !== `persona-${key}`);
        args.push(`--fingerprint-${key}=${value}`);
      }
    }
  }
  options.args = [...args, '--remote-debugging-address=127.0.0.1', `--remote-debugging-port=${request.cdpPort}`];
  return options;
}

async function localBinary(path) {
  if (typeof path !== 'string' || !path.trim()) throw new Error('executablePath must be a non-empty string');
  try {
    if (!(await stat(path)).isFile()) throw new Error('not a file');
    await access(path, process.platform === 'win32' ? constants.F_OK : constants.X_OK);
  } catch {
    throw new Error(`Local browser binary is missing or not executable: ${path}. Set executablePath/browserBinaryPath to your installed Chromix or Chromium binary; automatic downloading is disabled`);
  }
  return path;
}

export async function configureBinary(request, options, chromium, env = process.env) {
  if (options.channel) throw new Error('Use executablePath/browserBinaryPath instead of channel; this runtime requires an explicit local binary or installed default Playwright Chromium');
  const explicit = own(options, 'executablePath') ? options.executablePath : request.binaryPath || env.CLOAKBROWSER_BINARY_PATH;
  // playwright-core never downloads browsers. Only its already installed default is eligible.
  const binary = own(options, 'executablePath') || explicit ? explicit : chromium.executablePath();
  options.executablePath = await localBinary(binary);
  return options.executablePath;
}

export async function waitForCdp(endpoint, signal, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (signal.aborted) throw new Error('Chromix launch cancelled');
    try {
      const response = await fetch(`${endpoint}/json/version`, { signal: AbortSignal.timeout(1000) });
      const info = await response.json();
      if (response.ok && typeof info.webSocketDebuggerUrl === 'string') {
        const url = new URL(info.webSocketDebuggerUrl);
        if (url.hostname === '127.0.0.1' && url.port === new URL(endpoint).port) return;
      }
    } catch { /* CDP starts after the persistent context is created. */ }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error('Chromix CDP did not become ready on the host loopback port within 60 seconds');
}

async function openStartUrl(context, request, options) {
  const url = request.startUrl;
  if (typeof url !== 'string' || !/^(https?:\/\/|about:blank$)/.test(url)) return;
  const allArgs = [options.args, options.launchOptions?.args, options.contextOptions?.args].flat().filter(Boolean);
  if (allArgs.some((arg) => /^(https?:\/\/|about:)/.test(arg))) return;
  const pages = context.pages();
  if (pages.some((page) => !['about:blank', 'chrome://newtab/', 'chrome://new-tab-page/'].includes(page.url()))) return;
  const page = pages[0] ?? await context.newPage();
  try {
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  } catch {
    process.stderr.write('[chromix bridge] Profile start URL navigation did not finish; context remains open\n');
  }
}

export function runBridge({
  input = process.stdin,
  send = (message) => process.stdout.write(`${JSON.stringify(message)}\n`),
  loadPlaywright = () => import('playwright-core'),
  ready = waitForCdp,
  env = process.env,
  signal,
  closeTimeoutMs = 10_000,
  forceExit = () => {},
} = {}) {
  return new Promise((resolve) => {
    const lines = createInterface({ input, crlfDelay: Infinity });
    const controller = new AbortController();
    let context;
    let started = false;
    let launching = false;
    let stopping = false;
    let finished = false;
    let failed = false;
    let closeTask;
    let closeTimer;
    const finish = () => {
      if (finished) return;
      finished = true;
      clearTimeout(closeTimer);
      signal?.removeEventListener('abort', stop);
      lines.close();
      resolve();
    };
    const closeContext = () => {
      if (!context) return Promise.resolve();
      closeTask ??= Promise.resolve().then(() => context.close());
      return closeTask;
    };
    const fail = async (error) => {
      if (!failed && !finished) send({ type: 'error', message: error.message ?? String(error) });
      failed = true;
      await stop();
    };
    async function stop() {
      if (finished) return;
      stopping = true;
      controller.abort();
      if (!closeTimer) {
        closeTimer = setTimeout(() => {
          send({ type: 'error', message: 'Playwright shutdown timed out' });
          forceExit(1);
          finish();
        }, closeTimeoutMs);
      }
      try {
        await closeContext();
      } catch (error) {
        if (!failed) send({ type: 'error', message: `Chromix close failed: ${error.message}` });
        failed = true;
      }
      if (!launching && !finished) {
        if (!failed) send({ type: 'closed' });
        finish();
      }
    }
    async function launch(request) {
      launching = true;
      try {
        const options = prepareOptions(request);
        const { chromium } = await loadPlaywright();
        await configureBinary(request, options, chromium, env);
        await mkdir(options.userDataDir, { recursive: true });
        if (stopping) return;
        const explicitStartUrl = options.args.find((arg) => /^(https?:\/\/|about:blank$)/.test(arg));
        options.args = options.args.filter((arg) => !/^(https?:\/\/|about:blank$)/.test(arg));
        const { userDataDir, ...launchOptions } = options;
        context = await chromium.launchPersistentContext(userDataDir, launchOptions);
        context.once?.('close', () => {
          if (!stopping) {
            stopping = true;
            controller.abort();
            send({ type: 'closed' });
            finish();
          }
        });
        if (stopping) return;
        const endpoint = `http://127.0.0.1:${request.cdpPort}`;
        await ready(endpoint, controller.signal);
        if (stopping) return;
        await openStartUrl(context, { ...request, startUrl: explicitStartUrl ?? request.startUrl }, options);
        if (!stopping) send({ type: 'ready', cdpEndpoint: endpoint, pid: process.pid });
      } catch (error) {
        if (!stopping) {
          failed = true;
          send({ type: 'error', message: error.message ?? String(error) });
        }
        stopping = true;
      } finally {
        launching = false;
        if (stopping) await stop();
      }
    }
    lines.on('line', (line) => {
      if (finished || stopping) return;
      try {
        const message = JSON.parse(line);
        if (!started) {
          started = true;
          void launch(message);
        } else if (message.type === 'close') {
          void stop();
        } else {
          void fail(new Error('Expected close after the launch request'));
        }
      } catch {
        void fail(new Error('Invalid bridge JSON request'));
      }
    });
    lines.on('close', () => { if (!finished) void stop(); });
    input.on('error', (error) => { void fail(error); });
    signal?.addEventListener('abort', stop, { once: true });
    if (signal?.aborted) void stop();
  });
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  // Keep Playwright console/process.stdout output off the line-delimited control channel.
  const writeProtocol = process.stdout.write.bind(process.stdout);
  process.stdout.write = process.stderr.write.bind(process.stderr);
  const controller = new AbortController();
  process.once('SIGTERM', () => controller.abort());
  process.once('SIGINT', () => controller.abort());
  await runBridge({
    send: (message) => writeProtocol(`${JSON.stringify(message)}\n`),
    signal: controller.signal,
    forceExit: (code) => process.exit(code),
  });
  process.exit(0);
}
