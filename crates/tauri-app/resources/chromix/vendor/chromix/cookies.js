// Explicit, authenticated Cookie migration. This does not change OSCrypt or read
// another profile's database. Only the supplied live Chromium context is used.
import { createCipheriv, createDecipheriv, randomBytes, scrypt as scryptCallback } from "node:crypto";
import { promisify } from "node:util";
import { constants } from "node:fs";
import { link, lstat, open, unlink } from "node:fs/promises";
import { dirname, basename, join } from "node:path";
import { isIP } from "node:net";

const scrypt = promisify(scryptCallback);
const MAGIC = Buffer.from("CHROMIX-COOKIES\0\x01", "binary");
const HEADER = MAGIC.length + 16 + 12;
const MAX_BYTES = 16 * 1024 * 1024;
const MAX_COOKIES = 10000;
const fail = () => new Error("Invalid or unsupported portable Cookie data");

function passwordBytes(password) {
  if (typeof password !== "string" || /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(password))
    throw new TypeError("passphrase must be valid Unicode text");
  const bytes = Buffer.from(password, "utf8");
  if (bytes.length < 12 || bytes.length > 1024)
    throw new TypeError("passphrase must contain 12–1024 UTF-8 bytes");
  return bytes;
}

function text(value, maximum, allowEmpty = true) {
  if (typeof value !== "string" || (!allowEmpty && !value) || Buffer.byteLength(value) > maximum ||
      /[\x00-\x1f\x7f]/.test(value) || Buffer.from(value).toString("utf8") !== value) throw fail();
  return value;
}

function hostForDomain(domain) {
  const host = domain.startsWith(".") ? domain.slice(1) : domain;
  const bare = host.startsWith("[") && host.endsWith("]") ? host.slice(1, -1) : host;
  if (isIP(bare) === 6 && !bare.includes("%") && !domain.startsWith(".")) return `[${bare}]`;
  if (!/^[a-z0-9_-]+(?:\.[a-z0-9_-]+)*\.?$/i.test(host) || host.replace(/\.$/, "").length > 253) throw fail();
  return host;
}

export function normalizeCookies(cookies) {
  if (!Array.isArray(cookies) || cookies.length > MAX_COOKIES) throw fail();
  const keys = new Set();
  return cookies.map(input => {
    if (!input || typeof input !== "object" || Array.isArray(input)) throw fail();
    const cookie = {
      name: text(input.name, 4096), value: text(input.value, 16384),
      domain: text(input.domain, 255, false), path: text(input.path, 4096, false),
      secure: input.secure, httpOnly: input.httpOnly,
    };
    hostForDomain(cookie.domain);
    if (!cookie.path.startsWith("/") || typeof cookie.secure !== "boolean" || typeof cookie.httpOnly !== "boolean" ||
        /[;=]/.test(cookie.name) || /;/.test(cookie.value)) throw fail();
    if (input.partitionKeyOpaque === true) throw new Error("Opaque partition Cookies cannot be migrated");
    for (const [key, choices] of [["sameSite", ["Strict", "Lax", "None"]],
      ["priority", ["Low", "Medium", "High"]], ["sourceScheme", ["Unset", "NonSecure", "Secure"]]]) {
      if (input[key] !== undefined) {
        if (!choices.includes(input[key])) throw fail();
        cookie[key] = input[key];
      }
    }
    if (input.expires !== undefined) {
      if (typeof input.expires !== "number" || !Number.isFinite(input.expires) ||
          (input.expires !== -1 && input.expires <= 0)) throw fail();
      if (input.expires !== -1) cookie.expires = input.expires;
    }
    if (input.sourcePort !== undefined) {
      if (!Number.isInteger(input.sourcePort) || input.sourcePort < -1 || input.sourcePort > 65535) throw fail();
      cookie.sourcePort = input.sourcePort;
    }
    if (input.partitionKey !== undefined) {
      const partition = input.partitionKey;
      if (!partition || typeof partition !== "object" || Array.isArray(partition) ||
          typeof partition.hasCrossSiteAncestor !== "boolean") throw fail();
      const site = text(partition.topLevelSite, 2048, false);
      let url;
      try { url = new URL(site); } catch { throw fail(); }
      if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || url.origin !== site) throw fail();
      hostForDomain(url.hostname);
      cookie.partitionKey = { topLevelSite: site, hasCrossSiteAncestor: partition.hasCrossSiteAncestor };
    }
    if (cookie.name.startsWith("__Secure-") && !cookie.secure) throw fail();
    if (cookie.name.startsWith("__Host-") && (!cookie.secure || cookie.path !== "/" || cookie.domain.startsWith("."))) throw fail();
    if ((cookie.sameSite === "None" || cookie.partitionKey) && !cookie.secure) throw fail();
    const key = JSON.stringify([cookie.name, cookie.domain, cookie.path, cookie.partitionKey ?? null]);
    if (keys.has(key)) throw new Error("Duplicate portable Cookie identity");
    keys.add(key);
    return cookie;
  });
}

export async function encryptCookies(cookies, passphrase) {
  const payload = Buffer.from(JSON.stringify({ format: "chromix.cookies", version: 1, cookies: normalizeCookies(cookies) }));
  if (payload.length > MAX_BYTES - HEADER - 16) throw fail();
  const password = passwordBytes(passphrase);
  const salt = randomBytes(16), nonce = randomBytes(12);
  const header = Buffer.concat([MAGIC, salt, nonce]);
  const key = await scrypt(password, salt, 32, { N: 32768, r: 8, p: 1, maxmem: 64 * 1024 * 1024 });
  try {
    const cipher = createCipheriv("aes-256-gcm", key, nonce);
    cipher.setAAD(header);
    return Buffer.concat([header, cipher.update(payload), cipher.final(), cipher.getAuthTag()]);
  } finally { key.fill(0); password.fill(0); payload.fill(0); }
}

export async function decryptCookies(data, passphrase) {
  if (!(data instanceof Uint8Array)) throw fail();
  data = Buffer.from(data);
  if (data.length < HEADER + 16 || data.length > MAX_BYTES || !data.subarray(0, MAGIC.length).equals(MAGIC)) throw fail();
  const password = passwordBytes(passphrase);
  const salt = data.subarray(MAGIC.length, MAGIC.length + 16), nonce = data.subarray(MAGIC.length + 16, HEADER);
  const key = await scrypt(password, salt, 32, { N: 32768, r: 8, p: 1, maxmem: 64 * 1024 * 1024 });
  let payload;
  try {
    const decipher = createDecipheriv("aes-256-gcm", key, nonce);
    decipher.setAAD(data.subarray(0, HEADER));
    decipher.setAuthTag(data.subarray(-16));
    payload = Buffer.concat([decipher.update(data.subarray(HEADER, -16)), decipher.final()]);
    const value = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(payload));
    if (value?.format !== "chromix.cookies" || value.version !== 1) throw fail();
    return normalizeCookies(value.cookies);
  } catch { throw new Error("Cannot decrypt portable Cookies: wrong passphrase, damaged data or unsupported payload"); }
  finally { key.fill(0); password.fill(0); payload?.fill(0); }
}

async function readEnvelope(path) {
  if (!(await lstat(path)).isFile()) throw fail();
  const stream = await open(path, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
  try {
    const stat = await stream.stat();
    if (!stat.isFile() || stat.size > MAX_BYTES) throw fail();
    const parts = []; let total = 0;
    while (true) {
      const chunk = Buffer.alloc(Math.min(65536, MAX_BYTES + 1 - total));
      const { bytesRead } = await stream.read(chunk);
      if (!bytesRead) break;
      total += bytesRead;
      if (total > MAX_BYTES) throw fail();
      parts.push(chunk.subarray(0, bytesRead));
    }
    return Buffer.concat(parts);
  } finally { await stream.close(); }
}

async function publish(path, data) {
  const temporary = join(dirname(path), `.${basename(path)}.${randomBytes(16).toString("hex")}.tmp`);
  const stream = await open(temporary, "wx", 0o600);
  try {
    try { await stream.writeFile(data); await stream.sync(); }
    finally { await stream.close(); }
    await link(temporary, path); // fail rather than overwrite a prior export
  } finally { await unlink(temporary); }
}

async function withSession(context, action) {
  const page = await context.newPage();
  let session;
  try {
    session = typeof context.newCDPSession === "function" ? await context.newCDPSession(page) : await page.createCDPSession();
    const { targetInfo } = await session.send("Target.getTargetInfo");
    if (targetInfo.type !== "page") throw new Error("Cookie migration requires the supplied context's page target");
    // Network's page-target methods use that page's storage partition, including
    // persistent/default contexts. Storage.* without an id can select the OTHER
    // default context, and Storage.* with an id requires a browser target.
    // getAllCookies is deprecated but still supported by our pinned Chromium.
    return await action(session, {});
  } finally {
    try { if (session) await session.detach(); } finally { await page.close(); }
  }
}

function cookieParams(cookie) {
  const result = { ...cookie };
  if (!cookie.domain.startsWith(".")) {
    const scheme = cookie.sourceScheme === "Secure" || (cookie.sourceScheme !== "NonSecure" && cookie.secure) ? "https" : "http";
    const port = cookie.sourcePort > 0 ? `:${cookie.sourcePort}` : "";
    result.url = `${scheme}://${hostForDomain(cookie.domain)}${port}/`;
    delete result.domain; // Preserve host-only rather than widening to subdomains.
  }
  return result;
}

export async function exportCookies(context, path, { passphrase } = {}) {
  passwordBytes(passphrase).fill(0);
  const cookies = await withSession(context, async (session, params) =>
    (await session.send("Network.getAllCookies", params)).cookies);
  const data = await encryptCookies(cookies, passphrase);
  await publish(path, data);
  return { exported: cookies.length, formatVersion: 1 };
}

export async function importCookies(context, path, { passphrase } = {}) {
  const cookies = await decryptCookies(await readEnvelope(path), passphrase);
  const now = Date.now() / 1000;
  const active = cookies.filter(cookie => cookie.expires === undefined || cookie.expires > now);
  return withSession(context, async (session, params) => {
    const before = await session.send("Network.getAllCookies", params);
    if (before.cookies.length) throw new Error("Cookie import requires an empty destination context; existing Cookies are never cleared");
    if (active.length) await session.send("Network.setCookies", { ...params, cookies: active.map(cookieParams) });
    const after = normalizeCookies((await session.send("Network.getAllCookies", params)).cookies);
    const canonical = values => values.map(value => JSON.stringify(value)).sort();
    if (JSON.stringify(canonical(after)) !== JSON.stringify(canonical(active)))
      throw new Error("Cookie import verification failed; discard this destination context (no automatic clearing or rollback)");
    return { imported: active.length, skippedExpired: cookies.length - active.length, formatVersion: 1 };
  });
}
