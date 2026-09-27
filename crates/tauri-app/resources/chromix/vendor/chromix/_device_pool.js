// Share the packaged Python validator instead of maintaining two divergent schemas.
import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import { nativeSocksEnv } from './_socks_auth.js';

function start(python, command) {
  return spawn(python, ['-X', 'utf8', '-m', 'chromix._device_launch', command],
    { windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'], shell: false });
}

export function bridge(python, command, request) {
  return new Promise((resolve, reject) => {
    const child = start(python, command);
    let stdout = '', stderr = '';
    const timer = setTimeout(() => { child.kill(); reject(new Error('device pool validator timeout')); }, 120000);
    child.on('error', error => { clearTimeout(timer); reject(error); });
    child.stdout.on('data', data => {
      stdout += data;
      if (stdout.length > 32 * 1024 * 1024) { child.kill(); reject(new Error('device pool report exceeds limit')); }
    });
    child.stderr.on('data', data => { stderr = (stderr + data).slice(-16384); });
    child.stdin.on('error', () => {});
    child.on('close', code => {
      clearTimeout(timer);
      if (code !== 0) return reject(new Error(`device pool validator failed: ${stderr}`));
      try { resolve(JSON.parse(stdout)); } catch (error) { reject(error); }
    });
    child.stdin.end(JSON.stringify(request));
  });
}

async function serve(python) {
  const child = start(python, 'serve');
  let stderr = '';
  child.stderr.on('data', data => { stderr = (stderr + data).slice(-16384); });
  child.stdin.on('error', () => {});
  const closed = new Promise(resolve => child.on('close', resolve));
  const stop = async () => {
    child.stdin.end('\n');
    const timer = setTimeout(() => child.kill(), 5000);
    try { await closed; } finally { clearTimeout(timer); }
  };
  const lines = createInterface({ input: child.stdout });
  try {
    const hello = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('device probe server timeout')), 15000);
      const done = fn => value => { clearTimeout(timer); fn(value); };
      child.once('error', done(reject));
      child.once('close', done(() => reject(new Error(`device probe server exited: ${stderr}`))));
      lines.once('line', done(line => {
        try { resolve(JSON.parse(line)); } catch (error) { reject(error); }
      }));
    });
    return { ...hello, stop };
  } catch (error) {
    child.kill(); await closed; throw error;
  } finally { lines.close(); }
}

export function measuredOptions(options) {
  const allowed = new Set(['devicePool', 'headless', 'browserVersion', 'releaseChannel', 'userDataDir']);
  for (const key of Object.keys(options))
    if (!allowed.has(key)) throw new Error(`devicePool mode rejects launch/context override: ${key}`);
  if (Object.hasOwn(options, 'headless') && typeof options.headless !== 'boolean')
    throw new Error('headless must be boolean');
  if (Object.hasOwn(options, 'userDataDir') &&
      (typeof options.userDataDir !== 'string' || !options.userDataDir))
    throw new Error('userDataDir must be a nonempty path');
  const config = options.devicePool;
  if (!config || typeof config !== 'object' || Array.isArray(config)) throw new Error('devicePool must be an object');
  const { python = 'python', ...pool } = config;
  if (typeof python !== 'string' || !python) throw new Error('devicePool.python must be an executable path');
  if (typeof pool.seed === 'bigint') pool.seed = pool.seed.toString();
  if (typeof pool.seed === 'number' && !Number.isSafeInteger(pool.seed))
    throw new Error('use a string or BigInt for uint64 seeds above Number.MAX_SAFE_INTEGER');
  return { python, pool };
}

export function probeExpression(script, argument = null) {
  // JS Playwright treats strings as expressions, unlike Python's callable detection.
  return `(${script})(${JSON.stringify(argument)})`;
}

export async function collectFontSources(context, page, server) {
  let session;
  try {
    const samples = await page.evaluate(probeExpression(server.font_eval));
    session = await context.newCDPSession(page);
    await session.send('DOM.enable'); await session.send('CSS.enable');
    const { root } = await session.send('DOM.getDocument');
    const { nodeIds } = await session.send('DOM.querySelectorAll',
      { nodeId: root.nodeId, selector: server.font_selector });
    if (nodeIds.length !== samples.length) throw new Error('platform font node count changed');
    for (let i = 0; i < samples.length; ++i) {
      const { fonts } = await session.send('CSS.getPlatformFontsForNode', { nodeId: nodeIds[i] });
      // Match Python's ordinal ordering, independent of the host locale.
      const cmp = (a, b) => {
        const ac = Array.from(a, c => c.codePointAt(0)), bc = Array.from(b, c => c.codePointAt(0));
        for (let j = 0; j < Math.min(ac.length, bc.length); ++j)
          if (ac[j] !== bc[j]) return ac[j] - bc[j];
        return ac.length - bc.length;
      };
      samples[i].platformFonts = fonts.sort((a, b) => cmp(a.familyName, b.familyName) ||
        cmp(a.postScriptName, b.postScriptName) || a.glyphCount - b.glyphCount);
    }
    return { status: 'observed', value: { source: 'CDP.CSS.getPlatformFontsForNode',
      samples, fileBinding: 'not_verified' } };
  } finally {
    try { await session?.detach(); } finally { await page.evaluate(server.font_cleanup); }
  }
}

export async function collectGpuSystem(context) {
  let session, result;
  try {
    session = await context.browser().newBrowserCDPSession();
    const { gpu } = await session.send('SystemInfo.getInfo');
    // Keep native CDP evidence; the shared Python validator projects only
    // volatile counters away and never equates this list with API selections.
    result = { status: 'observed', value: { source: 'CDP.SystemInfo.getInfo', gpu } };
  } catch (error) {
    result = { status: 'error', reason: String(error).slice(0, 2048) };
  } finally {
    try { await session?.detach(); }
    catch (error) {
      const prior = result?.reason ? `${result.reason}; ` : '';
      result = { status: 'error', reason: `${prior}CDP session detach failed: ${error}`.slice(0, 2048) };
    }
  }
  return result;
}

export async function launchMeasured(chromium, binary, options) {
  const { python, pool } = measuredOptions(options);
  const headless = options.headless ?? true;
  const prepared = await bridge(python, 'prepare', {
    options: pool, binary, headless, persistent: options.userDataDir ?? null,
  });
  let browser, context, server;
  try {
    const launch = { executablePath: binary, headless, args: prepared.args, chromiumSandbox: true };
    const env = nativeSocksEnv(undefined, null);
    if (env) launch.env = env;
    if (options.userDataDir) {
      context = await chromium.launchPersistentContext(options.userDataDir,
        { ...launch, viewport: null, serviceWorkers: 'allow' });
    } else {
      browser = await chromium.launch(launch);
      context = await browser.newContext({ viewport: null, serviceWorkers: 'allow' });
    }
    context.setDefaultTimeout(30000);
    server = await serve(python);
    const page = await context.newPage();
    let observation;
    try {
      await page.goto(server.origin, { waitUntil: 'load', timeout: 30000 });
      const probe = probeExpression(server.probe_eval);
      observation = { window: await page.evaluate(probe),
        iframe: await page.frame({ url: server.origin + '/frame' }).evaluate(probe) };
      for (const scope of ['worker', 'shared_worker', 'service_worker'])
        observation[scope] = await page.evaluate(probeExpression(server.worker_eval, scope));
      observation.window.fontBackend = await collectFontSources(context, page, server);
      observation.window.gpuSystem = await collectGpuSystem(context);
    } finally { await page.close(); }
    context.chromixDeviceProfile = await bridge(python, 'verify', { observation, prepared });
    await server.stop(); server = null;
    const close = context.close.bind(context);
    context.close = async (...args) => { try { await close(...args); } finally { await browser?.close(); } };
    return context;
  } catch (error) {
    try { await context?.close(); }
    finally {
      try { await browser?.close(); } finally { await server?.stop(); }
    }
    throw error;
  }
}
