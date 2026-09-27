// Public switch syntax; keep in parity with the Python launcher.
const OFF = new Set(["off", "false", "0", "disable", "disabled"]);
const BRANDS = new Map([["chrome", "Chrome"], ["google chrome", "Chrome"],
  ["edge", "Edge"], ["microsoft edge", "Edge"], ["opera", "Opera"], ["vivaldi", "Vivaldi"]]);
const BOOLEANS = new Set(["--fingerprint-noise", "--fingerprint-sapi-voices",
  "--fingerprint-allow-3p-cookies", "--fingerprint-windows-font-metrics"]);
const INTEGERS = new Map([
  ["--fingerprint-hardware-concurrency", [1n, 128n]],
  ["--fingerprint-screen-width", [1n, 32768n]],
  ["--fingerprint-screen-height", [1n, 32768n]],
  ["--fingerprint-taskbar-height", [0n, 32768n]],
  ["--fingerprint-storage-quota", [0n, ((1n << 63n) - 1n) / 1048576n]],
]);
const PREFERENCE_BOOLEANS = new Set(["reduced-motion", "reduced-transparency", "inverted-colors"]);
const PREFERENCE_INTEGERS = new Map([["max-touch-points", [0n, 16n]], ["timer-resolution", [0n, 1000n]],
  ["audio-seed", [1n, (1n << 64n) - 1n]]]);
const PREFERENCE_ENUMS = new Map(Object.entries({
  "gpu-backend": ["native", "compatibility"],
  "font-policy": ["native", "restricted"],
  "audio-render": ["native", "isolated"],
  "color-scheme": ["dark", "light"],
  "preferred-contrast": ["more", "less", "no-preference"],
  "forced-colors": ["active", "none", "true", "false", "1", "0"],
  pointer: ["fine", "coarse", "none"], hover: ["hover", "none"], hdr: ["native"],
  "keyboard-layout": ["native", "us", "en-US"],
}));
const CODECS = new Set(["codec-h264", "codec-vp8", "codec-vp9", "codec-av1", "codec-hevc"]);

function validCodec(value) {
  if (["", "native", "disabled"].includes(value)) return true;
  const tokens = value.split(",");
  return tokens.includes("supported") && tokens.every((t) => ["supported", "smooth", "power-efficient"].includes(t));
}

function validatePreferences(args, final) {
  const switches = new Map(args.map((arg) => {
    const i = arg.indexOf("=");
    return i < 0 ? [arg, ""] : [arg.slice(0, i), arg.slice(i + 1)];
  }));
  const value = (name) => switches.get(`--uxr-${name}`) ?? switches.get(`--fingerprint-${name}`);
  const pointer = value("pointer"), hover = value("hover");
  const touch = value("max-touch-points") === undefined ? undefined : Number(value("max-touch-points"));
  if ((pointer === "none" && touch > 0) || (pointer === "coarse" && touch === 0) ||
      (pointer === "none" && hover === "hover"))
    throw new Error("inconsistent pointer, hover and max-touch-points");
  const keyboard = value("keyboard-layout");
  if (keyboard !== undefined && keyboard !== "native" && switches.get("--uxr-synthetic-device-tests") !== "true")
    throw new Error("keyboard-layout overrides require synthetic device tests; use native for real input");
  if (value("font-policy") === "restricted") {
    const families = value("font-whitelist") || "", entries = families.split(",");
    if (!families || Buffer.byteLength(families, "utf8") > 4096 || entries.length > 256 ||
        entries.some((entry) => !entry.replace(/^[ \t]+|[ \t]+$/g, "")) || /[\x00-\x08\x0a-\x1f\x7f]/.test(families))
      throw new Error("restricted font-policy requires 1 to 256 comma-separated installed font families");
  }
  if (final && value("audio-render") === "isolated" && !fingerprintOff(args)) {
    const seed = value("audio-seed") ?? switches.get("--uxr-fingerprint-seed");
    if (seed === undefined) {
      if (!switches.has("--fingerprint"))
        throw new Error("isolated audio requires --fingerprint or a nonzero audio seed");
    } else if (!/^[0-9]+$/.test(seed) || BigInt(seed) < 1n || BigInt(seed) >= (1n << 64n)) {
      throw new Error("isolated audio requires a nonzero uint64 seed");
    }
  }
}

export function fingerprintOff(args) {
  const flag = args.filter((a) => a.split("=", 1)[0] === "--fingerprint").at(-1);
  return flag !== undefined && OFF.has(flag.slice(flag.indexOf("=") + 1).toLowerCase());
}

export function normalizeFingerprintArgs(args = [], { final = false } = {}) {
  let result = (args || []).map((arg) => {
    if (typeof arg !== "string" || arg.includes("\0"))
      throw new Error("browser arguments must be strings without NUL");
    const separator = arg.indexOf("=");
    const key = separator < 0 ? arg : arg.slice(0, separator);
    const value = separator < 0 ? "" : arg.slice(separator + 1);
    const preference = key.startsWith("--fingerprint-") ? key.slice(14) : key.startsWith("--uxr-") ? key.slice(6) : "";
    if (PREFERENCE_BOOLEANS.has(preference)) {
      const low = value.toLowerCase();
      if (OFF.has(low)) return `${key}=false`;
      if (["", "true", "1", "on", "enable", "enabled"].includes(low)) return `${key}=true`;
      throw new Error(`${key} requires a boolean`);
    } else if (PREFERENCE_INTEGERS.has(preference)) {
      const [min, max] = PREFERENCE_INTEGERS.get(preference);
      if (!/^[0-9]+$/.test(value) || BigInt(value) < min || BigInt(value) > max)
        throw new Error(`${key} requires an integer in [${min}, ${max}]`);
    } else if (PREFERENCE_ENUMS.has(preference)) {
      if (!PREFERENCE_ENUMS.get(preference).includes(value))
        throw new Error(`${key} requires one of ${PREFERENCE_ENUMS.get(preference).join(", ")}`);
    } else if (CODECS.has(preference)) {
      if (!validCodec(value))
        throw new Error(`${key} requires native, disabled or supported[,smooth][,power-efficient]`);
    } else if (key === "--fingerprint") {
      if (OFF.has(value.toLowerCase())) return "--fingerprint=off";
      if (value && (!/^[0-9]+$/.test(value) || BigInt(value) < 1n || BigInt(value) >= 1n << 64n))
        throw new Error("--fingerprint requires a uint64 seed or off/false/0/disable/disabled");
    } else if (key === "--fingerprint-brand") {
      const brand = BRANDS.get(value.toLowerCase());
      if (!brand) throw new Error("--fingerprint-brand must be Chrome, Edge, Opera or Vivaldi");
      return `${key}=${brand}`;
    } else if (BOOLEANS.has(key)) {
      const low = value.toLowerCase();
      if (OFF.has(low)) return `${key}=false`;
      if (["", "true", "1", "on", "enable", "enabled"].includes(low)) return `${key}=true`;
      throw new Error(`${key} requires a boolean`);
    } else if (INTEGERS.has(key)) {
      const [min, max] = INTEGERS.get(key);
      if (!/^[0-9]+$/.test(value) || BigInt(value) < min || BigInt(value) > max)
        throw new Error(`${key} requires an integer in [${min}, ${max}]`);
    } else if (key === "--fingerprint-device-memory") {
      if (!/^(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$/.test(value) || !Number.isFinite(Number(value)) ||
          Number(value) <= 0 || Number(value) > 32)
        throw new Error("--fingerprint-device-memory requires a number greater than 0 and at most 32");
    } else if (["--fingerprint-brand-version", "--fingerprint-platform-version"].includes(key)) {
      if (!/^[0-9]+(?:\.[0-9]+){0,3}$/.test(value) || value.split(".").some((n) => BigInt(n) > 0xffffffffn))
        throw new Error(`${key} requires a numeric version with at most four components`);
      if (key === "--fingerprint-brand-version" && (BigInt(value.split(".")[0]) < 1n || BigInt(value.split(".")[0]) > 0x7fffffffn))
        throw new Error(`${key} requires a positive int32 major version`);
    }
    return arg;
  });
  if (fingerprintOff(result))
    result = result.filter((arg) => arg.split("=", 1)[0] !== "--fingerprint-platform");
  validatePreferences(result, final);
  return result;
}
