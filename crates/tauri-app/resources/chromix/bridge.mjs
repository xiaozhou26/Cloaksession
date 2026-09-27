import { access, mkdir, stat } from 'node:fs/promises';
import { constants } from 'node:fs';
import { createInterface } from 'node:readline';
import { pathToFileURL } from 'node:url';

const own = (object, key) => Object.hasOwn(object, key);
const object = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);
const reserved = /^--?(?:remote-debugging(?:-[^=\s]+)?|user-data-dir|profile-directory)(?:=|\s|$)/i;
const extensionFlag = /^--?(?:load-extension|disable-extensions(?:-except)?)(?:=|\s|$)/i;
const proxyFlag = /^--?(?:proxy-server|proxy-pac-url|no-proxy-server)(?:=|\s|$)/i;

export function prepareOptions(request) {
  if (!object(request) || request.type !== 'launch') throw new Error('Expected a launch request');
  if (!Number.isInteger(request.cdpPort) || request.cdpPort < 1 || request.cdpPort > 65535) {
    throw new Error('Host cdpPort must be an integer between 1 and 65535');
  }
  if (!object(request.options)) throw new Error('Chromix options must be an object');
  const options = structuredClone(request.options);
  // Measured admission excludes the host-controlled CDP arguments.
  if (own(options, 'mode') && options.mode !== 'native') {
    throw new Error('Chromix Node sidecar does not support measured mode because Cloaksession requires a host CDP endpoint; use native mode with the matching SDK evidence contract');
  }
  if (own(options, 'adapter') && options.adapter !== 'playwright') {
    throw new Error('Chromix Node sidecar supports only the playwright adapter');
  }
  if (own(options, 'devicePool')) {
    throw new Error('devicePool is not available through the Cloaksession CDP sidecar; launch measured contexts directly through the Chromix Node SDK');
  }
  for (const name of ['launchOptions', 'contextOptions']) {
    if (own(options, name) && !object(options[name])) throw new Error(`${name} must be an object`);
  }
  const layers = [['options', options], ['launchOptions', options.launchOptions], ['contextOptions', options.contextOptions]];
  const argumentLists = [];
  for (const [name, layer] of layers) {
    if (!layer) continue;
    for (const key of ['cdpPort', 'debuggingPort', 'remoteDebuggingPort', 'remoteDebuggingAddress']) {
      if (own(layer, key)) throw new Error(`${name}.${key} is reserved: CDP is host-controlled on 127.0.0.1`);
    }
    if (name !== 'options' && own(layer, 'userDataDir')) {
      throw new Error(`${name}.userDataDir conflicts with persistent context; use top-level userDataDir`);
    }
    for (const key of ['args', 'ignoreDefaultArgs']) {
      if (!own(layer, key)) continue;
      if (key === 'ignoreDefaultArgs' && typeof layer[key] === 'boolean') continue;
      if (!Array.isArray(layer[key]) || layer[key].some((arg) => typeof arg !== 'string')) {
        throw new Error(`${name}.${key} must be an array of strings`);
      }
      for (const arg of layer[key]) {
        if (reserved.test(arg.trim())) {
          throw new Error(`${name}.${key} contains a reserved debugging/user-data-dir/profile-directory argument; use top-level userDataDir and host CDP`);
        }
      }
      if (key === 'args') argumentLists.push(layer[key]);
    }
  }
  const topArgs = Array.isArray(options.args) ? [...options.args] : [];
  const launchArgs = Array.isArray(options.launchOptions?.args) ? [...options.launchOptions.args] : [];
  const contextArgs = Array.isArray(options.contextOptions?.args) ? [...options.contextOptions.args] : [];
  const args = [...topArgs, ...launchArgs, ...contextArgs];
  if (!own(options, 'userDataDir')) options.userDataDir = request.userDataDir;
  if (typeof options.userDataDir !== 'string' || !options.userDataDir.trim()) {
    throw new Error('userDataDir must be a non-empty path');
  }
  if (!own(options, 'headless') && !own(options.launchOptions ?? {}, 'headless') && !own(options.contextOptions ?? {}, 'headless')) {
    options.headless = false;
  }
  const explicitProxy = layers.some(([, layer]) => layer && own(layer, 'proxy')) || args.some((arg) => proxyFlag.test(arg));
  if (!explicitProxy && request.proxy) options.proxy = structuredClone(request.proxy);
  const defaultExtensions = !own(options, 'extensionPaths') && !args.some((arg) => extensionFlag.test(arg)) && request.extensionPaths?.length;
  if (defaultExtensions) options.extensionPaths = [...request.extensionPaths];

  // SDK launchOptions and contextOptions can replace args, so protect every effective layer.
  const controlArgs = [`--remote-debugging-address=127.0.0.1`, `--remote-debugging-port=${request.cdpPort}`];
  options.args = [...(options.args ?? []), ...controlArgs];
  for (const name of ['launchOptions', 'contextOptions']) {
    if (options[name] && own(options[name], 'args')) {
      options[name].args.push(...controlArgs);
      if (defaultExtensions) {
        const joined = request.extensionPaths.join(',');
        options[name].args.push(`--load-extension=${joined}`, `--disable-extensions-except=${joined}`);
      }
    }
  }
  return options;
}

async function localBinary(path) {
  if (typeof path !== 'string' || !path.trim()) throw new Error('Chromix binary path must be a non-empty string');
  try {
    if (!(await stat(path)).isFile()) throw new Error('not a file');
    await access(path, process.platform === 'win32' ? constants.F_OK : constants.X_OK);
  } catch {
    throw new Error(`Chromix local binary is missing or not executable: ${path}`);
  }
  return path;
}

export async function configureBinary(request, options, sdk, env = process.env) {
  // Persistent contextOptions override launchOptions in SDK 0.1.0.
  let binary = request.binaryPath || env.CLOAKBROWSER_BINARY_PATH;
  for (const name of ['contextOptions', 'launchOptions']) {
    if (own(options[name] ?? {}, 'executablePath')) {
      binary = await localBinary(options[name].executablePath);
      break;
    }
  }
  if (binary !== undefined && binary !== '') {
    binary = await localBinary(binary);
  } else if (request.skipDownload) {
    const info = sdk.binaryInfo(options);
    if (!info.installed || !info.path) {
      throw new Error('Chromix skip_download requires a local binary or an installed SDK cache entry; downloading is disabled');
    }
    binary = await localBinary(info.path);
  }
  if (binary) env.CLOAKBROWSER_BINARY_PATH = binary;
  return binary;
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
  loadSdk = () => import('@xiaoxiaofeihh/chromix'),
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
          send({ type: 'error', message: 'Chromix SDK shutdown timed out' });
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
        const sdk = await loadSdk();
        await configureBinary(request, options, sdk, env);
        await mkdir(options.userDataDir, { recursive: true });
        if (stopping) return;
        context = await sdk.launchPersistentContext(options);
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
        await openStartUrl(context, request, options);
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
  // Keep SDK console/process.stdout output off the line-delimited control channel.
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
