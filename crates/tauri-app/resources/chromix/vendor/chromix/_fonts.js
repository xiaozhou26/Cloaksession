// Linux Fontconfig wiring for the bundled Windows font assets.
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync,
         openSync, readSync, closeSync, statSync } from "node:fs";
import { dirname, join, resolve, extname } from "node:path";
import { homedir, tmpdir } from "node:os";
import { createHash } from "node:crypto";
import { hasNativeSocksEnv, withoutNativeSocksEnv } from "./_socks_auth.js";

const FONT_SUFFIXES = new Set([".ttf", ".otf", ".ttc"]);

// Same shape as assets/fonts/fonts.conf.template: expose exactly one font
// directory plus a private cache. Used when the caller supplies their own
// font directory (which then replaces the bundled directory).
const FONTS_CONF_TEMPLATE = `<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <dir>@FONTS_DIR@</dir>
  <cachedir>@CACHE_DIR@</cachedir>
</fontconfig>
`;

function readAt(fd, offset, length) {
  const buf = Buffer.alloc(length);
  const got = readSync(fd, buf, 0, length, offset);
  return got === length ? buf : null;
}

function decodeUtf16be(buf) {
  const swapped = Buffer.allocUnsafe(buf.length - (buf.length % 2));
  for (let i = 0; i + 1 < buf.length; i += 2) {
    swapped[i] = buf[i + 1];
    swapped[i + 1] = buf[i];
  }
  return swapped.toString("utf16le");
}

function decodeName(raw, platformId) {
  try {
    let text = null;
    if (platformId === 0 || platformId === 3) text = decodeUtf16be(raw); // Unicode / Windows
    else if (platformId === 1) text = raw.toString("latin1"); // Macintosh (approx; ASCII-safe)
    if (text === null) return "";
    return text.split("\0").join("").trim();
  } catch {
    return "";
  }
}

// Family names (name IDs 1 and 16) of one sfnt at file offset `base`.
// Reads only the table directory and the `name` table, never whole files.
function sfntFamilyNames(fd, base) {
  const families = new Set();
  try {
    const numBuf = readAt(fd, base + 4, 2);
    if (!numBuf) return families;
    const numTables = Math.min(numBuf.readUInt16BE(0), 256);
    let nameOff = null;
    for (let i = 0; i < numTables; i++) {
      const rec = readAt(fd, base + 12 + i * 16, 16);
      if (!rec) return families;
      if (rec.toString("latin1", 0, 4) === "name") {
        nameOff = rec.readUInt32BE(8);
        break;
      }
    }
    if (nameOff === null) return families;
    const head = readAt(fd, nameOff, 6);
    if (!head) return families;
    const count = Math.min(head.readUInt16BE(2), 4096);
    const strOff = head.readUInt16BE(4);
    for (let i = 0; i < count; i++) {
      const rec = readAt(fd, nameOff + 6 + i * 12, 12);
      if (!rec) return families;
      const platformId = rec.readUInt16BE(0);
      const nameId = rec.readUInt16BE(6);
      const length = rec.readUInt16BE(8);
      const soff = rec.readUInt16BE(10);
      if ((nameId !== 1 && nameId !== 16) || length === 0 || length > 1024) continue;
      const raw = readAt(fd, nameOff + strOff + soff, length);
      if (!raw) continue;
      const text = decodeName(raw, platformId);
      if (text) families.add(text);
    }
  } catch {
    // malformed font -> no names
  }
  return families;
}

function fileFamilyNames(path) {
  let fd;
  try {
    fd = openSync(path, "r");
    const magic = readAt(fd, 0, 4);
    if (!magic) return new Set();
    if (magic.toString("latin1") === "ttcf") {
      const numBuf = readAt(fd, 8, 4);
      if (!numBuf) return new Set();
      const numFonts = Math.min(numBuf.readUInt32BE(0), 64);
      // Read all member offsets up front — parsing seeks the cursor.
      const offsets = [];
      for (let i = 0; i < numFonts; i++) {
        const off = readAt(fd, 12 + i * 4, 4);
        if (!off) break;
        offsets.push(off.readUInt32BE(0));
      }
      const names = new Set();
      for (const off of offsets) for (const n of sfntFamilyNames(fd, off)) names.add(n);
      return names;
    }
    return sfntFamilyNames(fd, 0);
  } catch {
    return new Set();
  } finally {
    if (fd !== undefined) try { closeSync(fd); } catch { /* ignore */ }
  }
}

// Ordered, de-duplicated family names across all fonts in `fontsDir`.
// Parses OpenType name tables (IDs 1 and 16) of .ttf/.otf/.ttc files —
// the same data Fontconfig and DirectWrite resolve families from. Legacy
// .fon bitmap fonts carry no name table and are skipped.
export function fontFamiliesInDir(fontsDir) {
  const names = [];
  const seen = new Set();
  let entries;
  try {
    // Case-insensitive file order keeps the generated whitelist stable
    // across platforms (JS default sort is codepoint: capitals first).
    entries = readdirSync(fontsDir)
      .sort((a, b) => (a.toLowerCase() < b.toLowerCase() ? -1 : a.toLowerCase() > b.toLowerCase() ? 1 : 0));
  } catch {
    return names;
  }
  for (const entry of entries) {
    const path = join(fontsDir, entry);
    if (!FONT_SUFFIXES.has(extname(entry).toLowerCase())) continue;
    try {
      if (!statSync(path).isFile()) continue;
    } catch {
      continue;
    }
    for (const name of [...fileFamilyNames(path)].sort()) {
      if (!seen.has(name.toLowerCase())) {
        seen.add(name.toLowerCase());
        names.push(name);
      }
    }
  }
  return names;
}

// `--uxr-font-whitelist` covering every family found in `fontsDir`.
// Returns null when the directory has no parseable fonts.
export function fontDirWhitelistArg(fontsDir) {
  const families = fontFamiliesInDir(fontsDir);
  return families.length ? `--uxr-font-whitelist=${families.join(",")}` : null;
}

function writeFontconfig(fontsDir, template) {
  try {
    const cacheRoot = process.env.XDG_CACHE_HOME || join(homedir(), ".cache");
    const cacheDir = join(cacheRoot, "chromix", "fontconfig");
    mkdirSync(cacheDir, { recursive: true });
    const escape = (value) => value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
    const config = template
      .split("@FONTS_DIR@").join(escape(fontsDir))
      .split("@CACHE_DIR@").join(escape(cacheDir));
    const digest = createHash("sha256").update(config).digest("hex");
    const configPath = join(tmpdir(), `chromix-fontconfig-${process.getuid?.() ?? 0}-${digest}.conf`);
    try { writeFileSync(configPath, config, { flag: "wx" }); }
    catch (error) { if (error.code !== "EEXIST") throw error; }
    return configPath;
  } catch {
    return null;
  }
}

// `fontsDir` replaces the bundled `fonts/` next to the executable; without
// it, a Linux bundle containing `fonts/` is wired as before.
export function linuxFontEnv(executable, fontsDir) {
  if (process.platform !== "linux") return {};
  if (fontsDir) {
    const config = writeFontconfig(resolve(fontsDir), FONTS_CONF_TEMPLATE);
    return config ? { FONTCONFIG_FILE: config } : {};
  }
  const bundledDir = join(dirname(executable), "fonts");
  const template = join(bundledDir, "fonts.conf.template");
  if (!existsSync(template)) return {};
  try {
    const config = writeFontconfig(bundledDir, readFileSync(template, "utf8"));
    return config ? { FONTCONFIG_FILE: config } : {};
  } catch {
    return {};
  }
}

export function fontLaunchEnv(executable, userEnv, fontsDir) {
  const fontEnv = linuxFontEnv(executable, fontsDir);
  if (Object.keys(fontEnv).length === 0 && userEnv === undefined && !hasNativeSocksEnv(process.env)) return undefined;
  return { ...withoutNativeSocksEnv(process.env), ...fontEnv, ...(userEnv || {}) };
}
