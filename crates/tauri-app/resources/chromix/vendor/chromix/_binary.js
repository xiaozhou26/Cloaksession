// chromix (Node) — binary management: detect platform, download the matching
// bundle from the GitHub Release, verify SHA256SUMS, cache under ~/.cache/chromix.
import { createWriteStream, chmodSync, mkdirSync, createReadStream, mkdtempSync, renameSync, rmSync, realpathSync, statSync, symlinkSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, isAbsolute, join, posix, relative, sep } from "node:path";
import { promisify } from "node:util";
import { pipeline } from "node:stream/promises";
import { createHash } from "node:crypto";
import yauzl from "yauzl";

export const VERSION = "152.0.7977.82";
const REPO = "xiaozhou26/Chromix";
// Release channels track the latest verified binary for each supported major.
// Source/package versions may move ahead while the staged Chromium build runs.
export const CHANNELS = {
  stable: { tag: "v151.0.7922.173" },
  latest: { tag: "v152.0.7977.75" },
};
export const CACHE = process.env.CHROMIX_CACHE_DIR || join(homedir(), ".cache", "chromix");
export const hostFor = (tag) => process.env.CHROMIX_DOWNLOAD_HOST
  || `https://github.com/${REPO}/releases/download/${tag}`;

// platform key -> { asset, kind, launcher }
export const ASSETS = {
  "linux-x64":   { asset: "chromix-linux-x64.zip",   kind: "zip", launcher: "chromix/chromix" },
  "linux-arm64": { asset: "chromix-linux-arm64.zip", kind: "zip", launcher: "chromix/chromix" },
  "win-x64":     { asset: "chromix-win-x64.zip",     kind: "zip", launcher: "chromix/chromix.cmd" },
  "win-arm64":   { asset: "chromix-win-arm64.zip",   kind: "zip", launcher: "chromix/chromix.cmd" },
  "mac-arm64":   { asset: "chromix-mac-arm64.zip",   kind: "zip", launcher: "chromix/chromix" },
  "mac-x64":     { asset: "chromix-mac-x64.zip",     kind: "zip", launcher: "chromix/chromix" },
};

export function resolvePlatform() {
  const { platform, arch } = process;
  if (platform === "linux" && arch === "x64") return "linux-x64";
  if (platform === "linux" && arch === "arm64") return "linux-arm64";
  if (platform === "win32" && arch === "x64") return "win-x64";
  if (platform === "win32" && arch === "arm64") return "win-arm64";
  if (platform === "darwin" && arch === "arm64") return "mac-arm64";
  if (platform === "darwin" && arch === "x64") return "mac-x64";
  return null;
}

export async function sha256(path) {
  const h = createHash("sha256");
  await pipeline(createReadStream(path), h);
  return h.digest("hex");
}

export async function expectedSha(asset, host) {
  try {
    const r = await fetch(`${host}/SHA256SUMS`);
    if (!r.ok) return null;
    for (const line of (await r.text()).split("\n")) {
      const p = line.trim().split(/\s+/);
      if (p.length === 2 && p[1].replace(/^\*/, "") === asset) return p[0].toLowerCase();
    }
  } catch { /* none */ }
  return null;
}

export function binaryPath(plat, root) {
  if (plat.startsWith("mac-"))
    return join(root, "chromix", "Chromium.app", "Contents", "MacOS", "Chromium");
  return join(root, "chromix", plat.startsWith("win-") ? "chrome.exe" : "chrome");
}

function inside(bundle, target) {
  const rel = relative(bundle, target);
  return rel !== ".." && !rel.startsWith(`..${sep}`) && !isAbsolute(rel);
}

export function bundleComplete(plat, root) {
  if (!ASSETS[plat]) return false;
  try {
    return [join(root, ASSETS[plat].launcher), binaryPath(plat, root)].every((path) =>
      inside(join(realpathSync(root), "chromix"), realpathSync(path)) && statSync(path).isFile());
  } catch { return false; }
}

function zipEntries(zip) {
  return new Promise((resolveEntries, reject) => {
    const entries = [];
    zip.on("error", reject);
    zip.on("entry", (entry) => {
      entries.push(entry);
      zip.readEntry();
    });
    zip.on("end", () => resolveEntries(entries));
    zip.readEntry();
  });
}

function entryName(entry) {
  const name = entry.fileName.replace(/\/$/, "");
  const parts = name.split("/");
  // Reject Windows aliases too, even when extracting on POSIX.
  if (parts[0] !== "chromix" || parts.some((part) => !part || /[. ]$/.test(part) ||
      /[\\\\:\x00-\x1f<>"|?*]/.test(part) || /^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(part)))
    throw new Error(`Unsafe ZIP path: ${entry.fileName}`);
  return name;
}

async function extractZip(archive, root) {
  const zip = await promisify(yauzl.open)(archive, {
    lazyEntries: true, autoClose: false, strictFileNames: true,
  });
  try {
    const entries = await zipEntries(zip);
    const names = new Set();
    const links = new Set();
    const checked = entries.map((entry) => {
      const name = entryName(entry);
      const key = name.toLowerCase();
      if (names.has(key)) throw new Error(`Duplicate ZIP path: ${name}`);
      names.add(key);
      const mode = entry.externalFileAttributes >>> 16;
      const type = mode & 0xf000;
      if (![0, 0x4000, 0x8000, 0xa000].includes(type))
        throw new Error(`Unsupported ZIP entry: ${name}`);
      const link = type === 0xa000;
      if (link) links.add(key);
      return { entry, name, link, mode };
    });
    for (const { name } of checked) {
      const parts = name.toLowerCase().split("/");
      if (parts.slice(0, -1).some((_, i) => links.has(parts.slice(0, i + 1).join("/"))))
        throw new Error(`ZIP entry traverses a symlink: ${name}`);
    }
    const openStream = promisify(zip.openReadStream.bind(zip));
    // No archive symlink exists while files are being written.
    for (const { entry, name, link, mode } of checked) {
      if (link) continue;
      const target = join(root, name);
      if (entry.fileName.endsWith("/")) {
        mkdirSync(target, { recursive: true });
      } else {
        mkdirSync(dirname(target), { recursive: true });
        await pipeline(await openStream(entry), createWriteStream(target, { flags: "wx" }));
        if (process.platform !== "win32") chmodSync(target, mode & 0o111 ? 0o755 : 0o644);
      }
    }
    for (const { entry, name, link } of checked) {
      if (!link) continue;
      if (entry.uncompressedSize > 4096) throw new Error(`Unsafe ZIP symlink: ${name}`);
      const chunks = [];
      for await (const chunk of await openStream(entry)) chunks.push(chunk);
      const value = new TextDecoder("utf-8", { fatal: true }).decode(Buffer.concat(chunks));
      if (!value || /[\\\\:\x00]/.test(value) || posix.isAbsolute(value))
        throw new Error(`Unsafe ZIP symlink: ${name}`);
      const target = join(root, name);
      mkdirSync(dirname(target), { recursive: true });
      symlinkSync(value, target);
    }
    for (const { name, link } of checked) {
      if (!link) continue;
      try {
        if (inside(join(realpathSync(root), "chromix"), realpathSync(join(root, name)))) continue;
      } catch { /* Dangling links and cycles are not valid bundle entries. */ }
      throw new Error(`Unsafe ZIP symlink: ${name}`);
    }
  } finally {
    // Windows cannot remove the downloaded archive until its handle is closed.
    await new Promise((resolveClose, reject) => {
      zip.once("close", resolveClose);
      zip.once("error", reject);
      zip.close();
    });
  }
}

export async function ensureNative(plat, host, tag) {
  const { asset, launcher } = ASSETS[plat];
  const root = join(CACHE, tag, plat);
  const launcherPath = join(root, launcher);
  if (bundleComplete(plat, root)) return launcherPath;
  mkdirSync(join(CACHE, tag), { recursive: true });
  const stage = mkdtempSync(join(CACHE, tag, `${plat}-extract-`));
  try {
    const archive = join(stage, asset);
    process.stderr.write(`[chromix] downloading ${host}/${asset} ...\n`);
    const res = await fetch(`${host}/${asset}`);
    if (!res.ok) throw new Error(`download failed: ${res.status}`);
    await pipeline(res.body, createWriteStream(archive));

    const exp = await expectedSha(asset, host);
    if (exp) {
      const act = await sha256(archive);
      if (act !== exp) throw new Error(`SHA256 mismatch for ${asset}: expected ${exp}, got ${act}`);
      process.stderr.write("[chromix] SHA256 verified\n");
    } else {
      process.stderr.write("[chromix] WARNING: no SHA256SUMS published; skipping verification\n");
    }

    await extractZip(archive, stage);
    if (!bundleComplete(plat, stage))
      throw new Error("bundle extracted but launcher or chrome binary missing");
    if (process.platform !== "win32") {
      chmodSync(join(stage, launcher), 0o755);
      chmodSync(binaryPath(plat, stage), 0o755);
    }
    rmSync(archive);
    rmSync(root, { recursive: true, force: true });
    renameSync(stage, root);
    return launcherPath;
  } finally {
    rmSync(stage, { recursive: true, force: true });
  }
}
