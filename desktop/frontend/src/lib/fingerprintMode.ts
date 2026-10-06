import { readChromixFlag } from "./chromixFingerprint";

export type FingerprintMode = "random" | "fixed" | "custom";

export function randomFingerprintSeed(): string {
  const words = crypto.getRandomValues(new Uint32Array(2));
  return (((BigInt(words[0]) << 32n) | BigInt(words[1])) || 1n).toString();
}

export function seedProblem(seed: unknown): string | undefined {
  return typeof seed !== "string" || !/^\d+$/.test(seed) || BigInt(seed) < 1n || BigInt(seed) > 18446744073709551615n
    ? "Enter an unsigned 64-bit decimal seed (1–18446744073709551615). Zero disables native fingerprinting; use Advanced for that."
    : undefined;
}

export function fingerprintMode(options: Record<string, unknown>): FingerprintMode | undefined {
  const mode = options.fingerprintMode;
  return mode === "random" || mode === "fixed" || mode === "custom" ? mode : undefined;
}

export function storedFingerprintSeed(options: Record<string, unknown>): string | undefined {
  if (!seedProblem(options.fingerprintSeed)) return options.fingerprintSeed as string;
  for (const name of ["--uxr-fingerprint-seed", "--fingerprint-seed", "--fingerprint"]) {
    const value = readChromixFlag(options, name).value;
    if (!seedProblem(value)) return value as string;
  }
  return undefined;
}

export function fingerprintOptionsProblem(options: Record<string, unknown>): string | undefined {
  const mode = fingerprintMode(options);
  if (mode === "fixed" || mode === "custom") return seedProblem(options.fingerprintSeed);
  return undefined;
}
