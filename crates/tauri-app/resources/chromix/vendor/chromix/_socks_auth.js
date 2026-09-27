// Native RFC 1929 configuration. No local HTTP bridge, no page authentication.
import { splitProxy, lookupProxy } from "./_network.js";

export const SOCKS_AUTH_ENV = "CHROMIX_SOCKS5_AUTH";

// Windows environment names are case-insensitive. Reserve all spellings on
// every host so a profile cannot accidentally inherit another launch's secret.
export function hasNativeSocksEnv(env) {
  return Object.keys(env ?? {}).some(key => key.toUpperCase() === SOCKS_AUTH_ENV);
}

export function withoutNativeSocksEnv(env) {
  return Object.fromEntries(Object.entries(env ?? {}).filter(([key]) => key.toUpperCase() !== SOCKS_AUTH_ENV));
}

export function nativeSocksConfig(proxy, args = []) {
  const config = splitProxy(proxy);
  if (!config || !/^socks(?:5h?)?:/.test(config.server) ||
      (config.username === undefined && config.password === undefined))
    return { proxy: config, auth: null };
  lookupProxy(args, config);
  for (const key of ["username", "password"]) {
    const value = config[key];
    if (typeof value !== "string" || Buffer.from(value, "utf8").toString("utf8") !== value ||
        Buffer.byteLength(value) < 1 || Buffer.byteLength(value) > 255)
      throw new Error("Native SOCKS5 username and password must each contain 1–255 UTF-8 bytes");
  }
  const url = new URL(config.server);
  const auth = JSON.stringify({ version: 1, host: url.hostname.replace(/^\[|\]$/g, ""),
    port: Number(url.port || 1080), username: config.username, password: config.password });
  return { proxy: { server: `socks5://${url.host}`,
    ...(config.bypass !== undefined ? { bypass: config.bypass } : {}) }, auth };
}

export function nativeSocksEnv(env, auth) {
  if (hasNativeSocksEnv(env))
    throw new Error("Configure native SOCKS5 credentials with proxy, not a raw authentication environment variable");
  if (!auth && !hasNativeSocksEnv(process.env)) return env;
  const result = { ...withoutNativeSocksEnv(process.env), ...(env ?? {}) };
  if (auth) result[SOCKS_AUTH_ENV] = auth;
  return result;
}
