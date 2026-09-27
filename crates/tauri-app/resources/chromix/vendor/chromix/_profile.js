// A persistent profile owns one published seed, shared by both browser drivers.
import { randomBytes } from "node:crypto";
import { link, mkdir, open, readFile, unlink } from "node:fs/promises";
import { join } from "node:path";

const SEED_FILE = ".chromix-fingerprint-seed";

async function readSeed(path) {
  const data = await readFile(path, "utf8");
  const seed = Number(data);
  if (!Number.isInteger(seed) || seed < 1 || seed > 0xFFFFFFFF || data !== `${seed}\n`)
    throw new Error(`Invalid Chromix profile seed file: ${path}`);
  return seed;
}

export async function profileSeed(userDataDir) {
  const path = join(userDataDir, SEED_FILE);
  try { return await readSeed(path); }
  catch (error) { if (error.code !== "ENOENT") throw error; }
  await mkdir(userDataDir, { recursive: true });
  const seed = randomBytes(4).readUInt32LE(0) || 1;
  const temporary = join(userDataDir, `${SEED_FILE}.${randomBytes(16).toString("hex")}`);
  const stream = await open(temporary, "wx", 0o600);
  try {
    try {
      await stream.writeFile(`${seed}\n`, "ascii");
      await stream.sync();
    } finally { await stream.close(); }
    // Publish the complete file without replacing a concurrent winner.
    try { await link(temporary, path); }
    catch (error) { if (error.code !== "EEXIST") throw error; }
    return await readSeed(path);
  } finally { await unlink(temporary); }
}
