// Native Puppeteer adapter. No Playwright driver or page fingerprint injection.
import { buildLaunchOptions as buildSharedLaunchOptions, buildContextOptions,
  humanizePage, resolveHumanConfig } from "./index.js";
import { profileSeed } from "./_profile.js";
import { splitProxy, lookupProxy } from "./_network.js";
import { nativeSocksConfig, hasNativeSocksEnv } from "./_socks_auth.js";

export { ensureBinary, binaryInfo, clearCache, checkForUpdate, VERSION,
  CHROMIUM_VERSION, CHROMIUM_BUILD_VERSION } from "./index.js";
export { exportCookies, importCookies, encryptCookies, decryptCookies } from "./cookies.js";

async function loadPuppeteer() {
  for (const name of ["puppeteer-core", "puppeteer"]) {
    try {
      const module = await import(name);
      return module.default ?? module;
    } catch (error) {
      // Do not mask a broken installed driver as an absent optional dependency.
      if (error.code !== "ERR_MODULE_NOT_FOUND" || !error.message.includes(`'${name}'`)) throw error;
    }
  }
  throw new Error("Install the optional Puppeteer driver: npm i puppeteer-core");
}

function validate(options) {
  if (hasNativeSocksEnv(options.launchOptions?.env))
    throw new Error("Use proxy for native SOCKS5 credentials, not a raw authentication environment variable");
  if (Object.hasOwn(options, "devicePool"))
    throw new Error("Puppeteer devicePool admission is not implemented; use the measured Playwright entry point");
  const contextKeys = Object.keys(options.contextOptions ?? {});
  if (contextKeys.length)
    throw new Error("Puppeteer contextOptions are not Playwright options; configure the returned native context/page explicitly");
  for (const key of ["proxy", "userDataDir", "defaultViewport", "userAgent", "executablePath"]) {
    if (options[key] !== undefined && options.launchOptions?.[key] !== undefined)
      throw new Error(`Specify ${key} once, not at both option levels`);
  }
  if (options.userAgent !== undefined || options.colorScheme !== undefined ||
      options.launchOptions?.userAgent !== undefined || options.launchOptions?.colorScheme !== undefined)
    throw new Error("Puppeteer userAgent/colorScheme are page APIs; use native fingerprint args or configure the page explicitly");
  const userDataDir = options.userDataDir ?? options.launchOptions?.userDataDir;
  if (userDataDir !== undefined && (typeof userDataDir !== "string" || !userDataDir))
    throw new TypeError("userDataDir must be a nonempty path string");
  if (options.viewport !== undefined && (options.defaultViewport !== undefined ||
      options.launchOptions?.defaultViewport !== undefined))
    throw new Error("Specify viewport or defaultViewport, not both");
  const args = options.launchOptions?.args ?? options.args ?? [];
  if (!Array.isArray(args) || args.some(arg => typeof arg !== "string"))
    throw new TypeError("args must be an array of strings");
  if (args.some(arg => /^--user-data-dir(?:=|$)/.test(arg)))
    throw new Error("Use userDataDir, not a raw --user-data-dir, to bind the persistent seed");
  const proxy = splitProxy(options.launchOptions?.proxy ?? options.proxy);
  // Check raw route conflicts even when no GeoIP lookup is requested.
  lookupProxy(args, proxy);
  const nativeProxy = nativeSocksConfig(proxy, args);
  if (proxy && (proxy.username !== undefined || proxy.password !== undefined) && !nativeProxy.auth)
    throw new Error("Puppeteer proxy credentials require a native auth route; page.authenticate is not a browser-wide proxy-auth backend");
  if (nativeProxy.proxy && !/^(?:https?|socks[45]):/.test(nativeProxy.proxy.server))
    throw new Error("Use a Chromium-supported http, https, socks4 or socks5 proxy scheme");
  for (const key of ["chromiumSandbox", "downloadsPath", "firefoxUserPrefs", "tracesDir"]) {
    if (Object.hasOwn(options.launchOptions ?? {}, key))
      throw new Error(`${key} is a Playwright launch option, not a Puppeteer launch option`);
  }
  return nativeProxy.proxy;
}

export async function buildLaunchOptions(options = {}) {
  const proxy = validate(options);
  const userDataDir = options.userDataDir ?? options.launchOptions?.userDataDir;
  let args = [...(options.launchOptions?.args ?? options.args ?? [])];
  if (userDataDir && (options.stealthArgs ?? true) &&
      !args.some(arg => arg.split("=", 1)[0] === "--fingerprint"))
    args.push(`--fingerprint=${await profileSeed(userDataDir)}`);
  const prepared = { ...options, args, launchOptions: { ...options.launchOptions,
    ...(options.launchOptions?.args !== undefined ? { args } : {}),
    ...(options.executablePath !== undefined ? { executablePath: options.executablePath } : {}) } };
  const shared = await buildSharedLaunchOptions(prepared);
  const { proxy: ignoredProxy, ...result } = shared;
  if (proxy) {
    result.args = result.args.filter(arg => !/^--proxy-(?:server|bypass-list)(?:=|$)/.test(arg));
    result.args.push(`--proxy-server=${proxy.server}`);
    if (proxy.bypass !== undefined) result.args.push(`--proxy-bypass-list=${proxy.bypass}`);
  }
  if (userDataDir) result.userDataDir = userDataDir;
  const context = buildContextOptions(prepared);
  result.defaultViewport = options.defaultViewport ?? options.launchOptions?.defaultViewport ??
    (context.viewport ? { ...context.viewport,
      ...(context.deviceScaleFactor !== undefined ? { deviceScaleFactor: context.deviceScaleFactor } : {}) } : null);
  // Explicit null must not be replaced by a template or Puppeteer's 800x600 default.
  if (options.defaultViewport === null || options.launchOptions?.defaultViewport === null)
    result.defaultViewport = null;
  return result;
}

const humanized = new WeakSet();
function preparePage(page, config) {
  if (!config || humanized.has(page)) return page;
  const wheel = page.mouse.wheel.bind(page.mouse);
  page.mouse.wheel = (dx, dy) => wheel({ deltaX: dx, deltaY: dy });
  humanizePage(page, config);
  const humanWheel = page.mouse.wheel;
  page.mouse.wheel = ({ deltaX = 0, deltaY = 0 } = {}) => humanWheel(deltaX, deltaY);
  humanized.add(page);
  return page;
}

async function prepareContext(context, config) {
  if (!config) return context;
  for (const page of await context.pages()) preparePage(page, config);
  const newPage = context.newPage.bind(context);
  context.newPage = async (...args) => preparePage(await newPage(...args), config);
  return context;
}

async function prepareBrowser(browser, options) {
  if (!options.humanize) return browser;
  const config = resolveHumanConfig(options.humanPreset, options.humanConfig);
  for (const context of browser.browserContexts()) await prepareContext(context, config);
  const newPage = browser.newPage.bind(browser);
  browser.newPage = async (...args) => preparePage(await newPage(...args), config);
  const createContext = browser.createBrowserContext.bind(browser);
  browser.createBrowserContext = async (...args) => prepareContext(await createContext(...args), config);
  return browser;
}

export async function launch(options = {}) {
  // Validate before loading a driver, writing a seed or fetching a binary.
  validate(options);
  const puppeteer = await loadPuppeteer();
  const launchOptions = await buildLaunchOptions(options);
  const browser = await puppeteer.launch(launchOptions);
  try { return await prepareBrowser(browser, options); }
  catch (error) { await browser.close(); throw error; }
}

export async function launchContext(options = {}) {
  if (options.userDataDir || options.launchOptions?.userDataDir)
    throw new Error("Use launchPersistentContext for userDataDir");
  const browser = await launch(options);
  let context;
  try { context = await browser.createBrowserContext(); }
  catch (error) { await browser.close(); throw error; }
  const closeContext = context.close.bind(context);
  let closing;
  context.close = () => closing ??= (async () => {
    try { await closeContext(); } finally { await browser.close(); }
  })();
  return context;
}

export async function launchPersistentContext(options = {}) {
  if (!options.userDataDir && !options.launchOptions?.userDataDir)
    throw new Error("launchPersistentContext requires userDataDir");
  const browser = await launch(options);
  let context;
  try { context = browser.defaultBrowserContext(); }
  catch (error) { await browser.close(); throw error; }
  // Puppeteer cannot close the default context independently of its browser.
  let closing;
  context.close = () => closing ??= browser.close();
  return context;
}

export async function connect(options = {}) {
  for (const key of ["args", "proxy", "timezone", "timezoneId", "locale", "geoip", "devicePool", "stealthArgs",
    "userDataDir", "launchOptions", "contextOptions", "headless", "executablePath", "browserVersion", "releaseChannel",
    "extensionPaths", "fontsDir", "startMaximized", "viewport", "colorScheme", "userAgent", "env", "channel"])
    if (Object.hasOwn(options, key)) throw new Error(`connect cannot apply launch-scoped option ${key}`);
  const puppeteer = await loadPuppeteer();
  const { humanize, humanPreset, humanConfig, ...connectionOptions } = options;
  const browser = await puppeteer.connect({ defaultViewport: null, ...connectionOptions });
  try { return await prepareBrowser(browser, { humanize, humanPreset, humanConfig }); }
  catch (error) { await browser.disconnect(); throw error; } // never close a connected caller-owned browser
}
