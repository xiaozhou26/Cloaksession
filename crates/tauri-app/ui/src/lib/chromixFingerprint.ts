export type ChromixOptions = Record<string, unknown>;
export type ChromixFlagValue = string | null | undefined;
export type ChromixFieldKind = "text" | "enum" | "boolean" | "seed" | "presence" | "feature";

export interface ChromixFingerprintField {
  id: string;
  group: string;
  label: string;
  description: string;
  kind: ChromixFieldKind;
  flag?: string;
  aliases?: readonly string[];
  sdkKey?: string;
  choices?: readonly string[];
  unit?: string;
  integer?: readonly [string, string];
  decimal?: readonly [number, number];
}

export const CHROMIX_FINGERPRINT_SOURCE = {
  repository: "https://github.com/xiaozhou26/Chromix",
  commit: "39b9ea1bd262c2eb6306f3e23279398a6476c845",
  commitDate: "2026-09-19T01:03:28Z",
  flags: "docs/fingerprint-flags.md",
  sdk: "sdk/node/README.md",
  backend: "docs/backend-policy.md",
  normalizer: "sdk/node/_fingerprint.js",
  geometry: "sdk/node/_persona.js",
  nativeAliases: "patches/0036-chrome-app-chrome_main-fingerprint-normalize.patch",
} as const;

export const CHROMIX_FINGERPRINT_GROUPS = [
  { id: "identity", label: "Identity & browser", description: "One immutable launch persona is shared by renderers and workers. An unset field writes no override." },
  { id: "hardware", label: "CPU & memory", description: "Presentation settings do not allocate CPU cores or change V8 heap limits." },
  { id: "display", label: "Screen, work area & viewport", description: "Screen/work-area values are DIP. Window bounds, page viewport and physical screen are distinct. Raw geometry names are verified against the SDK and native normalizer." },
  { id: "gpu", label: "GPU & rendering backend", description: "Native is the ordinary launch default. Compatibility WebGL names do not supply a GPU or change native WebGPU identity, GL limits or software-renderer guards." },
  { id: "fonts", label: "Fonts", description: "Use actual installed fonts. A family name is not a font file or a DirectWrite rasterizer." },
  { id: "regional", label: "Locale & timezone", description: "Explicit regional settings survive fingerprint=off. SDK locale/timezone and raw aliases have their own precedence; none are silently removed." },
  { id: "storage", label: "Storage quota", description: "Quota is a launch-local backend policy, not a disk reservation. Zero is a valid quota." },
  { id: "network", label: "WebRTC & GeoIP", description: "IP overrides change local ICE/SDP/stats presentation, not sockets, STUN success or routing. GeoIP lookup failure stops launch; no fake candidates are generated." },
  { id: "media", label: "Audio, codecs & clocks", description: "Codec restrictions never add support. Audio devices/sample rates and internal scheduling clocks remain native." },
  { id: "preferences", label: "CSS & input preferences", description: "These configure effective WebPreferences. They do not install a touchscreen, HDR display or keyboard layout." },
  { id: "privacy", label: "Noise, cookies & author shadow roots", description: "Noise controls existing perturbation paths, not four independent noise implementations. Cookie and FakeShadowRoot opt-ins remain independent of fingerprint=off." },
  { id: "sdk", label: "SDK launch behavior", description: "Only explicit selections are written. SDK defaults and saved persistent seeds still apply when fields are unset." },
  { id: "advanced", label: "Advanced / high-risk engine controls", description: "Explicit SDK-documented opt-ins. Runtime suppression can break automation; Canvas Bridge removes the sandbox from bridge renderers and forwards drawing operations. Native GPU mode suppresses Bridge." },
] as const;

const UINT64_MAX = "18446744073709551615";
const publicFlag = (
  name: string, group: string, label: string, description: string,
  extra: Partial<ChromixFingerprintField> = {},
): ChromixFingerprintField => ({ id: name, flag: `--fingerprint-${name}`, group, label, kind: "text", description, ...extra });
const rawFlag = (
  name: string, group: string, label: string, description: string,
  extra: Partial<ChromixFingerprintField> = {},
): ChromixFingerprintField => ({ id: name, flag: `--${name}`, group, label, kind: "text", description, ...extra });
const sdkField = (
  sdkKey: string, group: string, label: string, kind: ChromixFieldKind,
  description: string, extra: Partial<ChromixFingerprintField> = {},
): ChromixFingerprintField => ({ id: `sdk-${sdkKey}`, sdkKey, group, label, kind, description, ...extra });
const alias = (name: string): { aliases: string[] } => ({ aliases: [`--uxr-${name}`] });
const dip = { unit: "DIP", integer: ["1", "32768"] as const };

export const CHROMIX_FINGERPRINT_FIELDS: readonly ChromixFingerprintField[] = [
  rawFlag("fingerprint", "identity", "Fingerprint seed / off", "Nonzero decimal uint64, or off / false / 0 / disable / disabled. Bare --fingerprint requests a generated seed. Unset leaves SDK persistent-seed behavior intact.", { kind: "seed" }),
  publicFlag("platform", "identity", "Platform", "windows / Win32, macos / MacIntel, linux / Linux x86_64. Same-OS generic aliases retain native high-entropy fields; cross-OS choices use desktop defaults.", { kind: "enum", choices: ["windows", "Win32", "macos", "MacIntel", "linux", "Linux x86_64"] }),
  publicFlag("brand", "identity", "Browser brand", "Changes UA and Client Hints branding, not the engine or vendor-specific features. Case-insensitive.", { kind: "enum", choices: ["Chrome", "Edge", "Opera", "Vivaldi"], ...alias("ua-brand") }),
  publicFlag("brand-version", "identity", "Brand version", "Numeric version with up to four components. Chrome also changes its engine token unless uxr-ua-full-version is explicit; other brands retain a separate Chromium version.", alias("ua-brand-version")),
  publicFlag("platform-version", "identity", "Platform version", "Numeric Client Hints version with up to four components. Does not change the host OS.", alias("ua-platform-version")),
  rawFlag("uxr-ua-full-version", "identity", "Chromium engine version", "Explicit engine version takes precedence over Chrome's derived brand version; keep it consistent with the actual binary."),
  sdkField("userAgent", "identity", "User-Agent", "text", "SDK/Playwright context User-Agent override. Does not rewrite the native engine; keep Client Hints and persona coherent."),
  publicFlag("hardware-concurrency", "hardware", "Hardware concurrency", "Default value 8 is supplied by the native implementation. Without synthetic device tests this value is not an effective override.", { integer: ["1", "128"], unit: "cores", ...alias("hw-concurrency") }),
  publicFlag("device-memory", "hardware", "Device memory", "Positive decimal up to 32, rounded to a supported bucket: 0.25, 0.5, 1, 2, 4, 8, 16, 32. Seeded default: 8 GB.", { decimal: [0, 32], unit: "GB", ...alias("device-memory") }),
  publicFlag("screen-width", "display", "Screen width", "Seeded default: 1920 on Windows/Linux, 1440 on macOS. Geometry aliases must agree in synthetic mode.", { ...dip, ...alias("screen-width") }),
  publicFlag("screen-height", "display", "Screen height", "Seeded default: 1080 on Windows/Linux, 900 on macOS. Geometry aliases must agree in synthetic mode.", { ...dip, ...alias("screen-height") }),
  publicFlag("taskbar-height", "display", "Taskbar / dock height", "Seeded default: Windows 48, macOS 95, Linux 0. Must agree with explicit available height; does not resize the OS taskbar.", { unit: "DIP", integer: ["0", "32768"], ...alias("taskbar-height") }),
  rawFlag("uxr-screen-avail-width", "display", "Available screen width", "Work-area width; cannot exceed screen width. Zero is accepted by SDK geometry validation.", { unit: "DIP", integer: ["0", "32768"] }),
  rawFlag("uxr-screen-avail-height", "display", "Available screen height", "Work-area height; cannot exceed screen height. Must equal screen height minus taskbar height when both are set.", { unit: "DIP", integer: ["0", "32768"] }),
  rawFlag("uxr-device-pixel-ratio", "display", "Device pixel ratio", "SDK synthetic geometry accepts 0.25–8. Playwright viewport/deviceScaleFactor can override geometry; this is not physical display scaling.", { decimal: [0.25, 8], unit: "ratio" }),
  rawFlag("uxr-viewport-width", "display", "Raw viewport width", "Supply both raw viewport dimensions. Overrides the SDK synthetic UI-strip template; top-level SDK viewport is a separate context setting.", dip),
  rawFlag("uxr-viewport-height", "display", "Raw viewport height", "Supply both raw viewport dimensions. SDK viewport:null removes inherited screen/DPR defaults; edit that option in the outer SDK JSON editor.", dip),
  rawFlag("uxr-outer-width", "display", "Outer window width", "Native window bounds. Must agree with an explicit --window-size.", dip),
  rawFlag("uxr-outer-height", "display", "Outer window height", "Native window bounds. SDK synthetic geometry requires more than its 85 DIP UI strip.", dip),
  rawFlag("uxr-window-x", "display", "Window X", "Signed window coordinate. Explicit native --window-position takes precedence.", { unit: "DIP" }),
  rawFlag("uxr-window-y", "display", "Window Y", "Signed window coordinate. Explicit native --window-position takes precedence.", { unit: "DIP" }),
  publicFlag("gpu-backend", "gpu", "GPU backend policy", "Native shares one Canvas/WebGL/WebGPU policy and suppresses legacy noise, Bridge and capability/identity overrides. Synthetic tests default to compatibility unless native is explicit.", { kind: "enum", choices: ["native", "compatibility"], ...alias("gpu-backend") }),
  publicFlag("gpu-vendor", "gpu", "WebGL unmasked vendor", "Presentation only in compatibility mode on nonsuppressed contexts. Native/software contexts retain real identity; unknown one-sided hints do not borrow an unrelated counterpart.", alias("webgl-vendor")),
  publicFlag("gpu-renderer", "gpu", "WebGL unmasked renderer", "Same compatibility/context guards as vendor. Public WebGPU keeps the complete native Dawn adapter identity.", alias("webgl-renderer")),
  publicFlag("geolocation", "gpu", "Geolocation presentation", "Native normalizer alias retained for compatibility with the public fingerprint switch family. It does not route traffic or grant browser permission.", alias("geolocation")),
  rawFlag("disable-gpu-fingerprint", "gpu", "Disable GPU fingerprint", "Native normalizer compatibility switch. It is independent from fingerprint-gpu-backend and is preserved as an explicit raw control.", { aliases: ["--uxr-disable-gpu-fingerprint"] }),
  publicFlag("windows-font-metrics", "fonts", "Windows font metrics", "Requires Linux → Windows persona and actual matching Windows font files. Only selected metrics align; otherwise no-op.", { kind: "boolean", ...alias("windows-font-metrics") }),
  publicFlag("font-policy", "fonts", "Font policy", "Restricted checks resolved fonts, fallback and Local Font Access. Downloaded author fonts remain usable.", { kind: "enum", choices: ["native", "restricted"], ...alias("font-policy") }),
  publicFlag("font-whitelist", "fonts", "Installed font families", "Comma-separated 1–256 actual installed family names; maximum 4096 UTF-8 bytes. ASCII case-insensitive; Unicode names preserved.", alias("font-whitelist")),
  sdkField("fontsDir", "fonts", "Font directory", "text", "SDK parses real font files for a default whitelist before restricted-policy validation. On Linux it also loads the directory through Fontconfig."),
  publicFlag("timezone", "regional", "Timezone flag", "IANA timezone. Explicit SDK timezone overwrites the public flag, while an explicit --uxr-timezone still takes precedence.", alias("timezone")),
  publicFlag("locale", "regional", "Locale flag / raw languages", "Language tag and normalized Accept-Language. The raw --uxr-languages field accepts a comma-separated language list and wins over this alias.", alias("languages")),
  sdkField("timezone", "regional", "SDK timezone", "text", "High-level timezone routed to the native flag, not context emulation. Explicit value wins over GeoIP; raw --uxr-timezone wins over the public alias."),
  sdkField("locale", "regional", "SDK locale", "text", "High-level language tag routed to --lang and --fingerprint-locale. Explicit value wins over GeoIP; raw --uxr-languages takes precedence."),
  publicFlag("storage-quota", "storage", "Storage quota", "Integer MiB (1024² bytes), 0–8796093022207. Seeded default: 102400 MiB. Native disk exhaustion, errors and privileged storage policies remain.", { integer: ["0", "8796093022207"], unit: "MiB", ...alias("storage-quota") }),
  publicFlag("webrtc-ip", "network", "WebRTC presentation IP", "Literal IPv4/IPv6 or auto. SDK auto resolves once through the effective proxy; explicit IP wins over GeoIP. No hostname, fabricated srflx or routing guarantee.", alias("webrtc-ip")),
  rawFlag("force-webrtc-ip-handling-policy", "network", "Native WebRTC IP policy", "With a proxy the SDK defaults to disable_non_proxied_udp unless an explicit native policy wins. This policy is not proof of all DNS/HTTP/QUIC routing."),
  sdkField("geoip", "network", "Resolve GeoIP", "boolean", "Resolve timezone, locale and WebRTC exit IP through the effective proxy. HTTP metadata is not independent route evidence; off mode skips IP injection."),
  publicFlag("audio-render", "media", "Audio render policy", "Native by default. Isolated processes the actual output bus using a seed-dependent 2^-20 sample grid; not virtual audio hardware.", { kind: "enum", choices: ["native", "isolated"], ...alias("audio-render") }),
  publicFlag("audio-seed", "media", "Audio isolation seed", "Nonzero decimal uint64. Isolated can fall back to the fingerprint seed. noise=false or fingerprint=off disables the processing.", { integer: ["1", UINT64_MAX], ...alias("audio-seed") }),
  publicFlag("timer-resolution", "media", "Timer resolution", "Decimal integer 0–1000 milliseconds, not microseconds. Zero/unset preserves native precision. Quantizes public wall clocks/timestamps, not internal scheduling.", { unit: "ms", integer: ["0", "1000"], ...alias("timer-resolution") }),
  ...["h264", "vp8", "vp9", "av1", "hevc"].map((codec) => publicFlag(`codec-${codec}`, "media", `${codec.toUpperCase()} codec policy`, "Native or disabled; an explicit empty value also disables this family. Restrictions cover supported entry points, not installation of a codec or all DRM/remote decoders.", { kind: "enum", choices: ["native", "disabled", ""], ...alias(`codec-${codec}`) })),
  publicFlag("sapi-voices", "media", "Synthetic Windows voice table", "Only affects the Windows synthetic fixture. Ordinary launches always retain native voices; this does not install SAPI or synthesize voices.", { kind: "boolean" }),
  publicFlag("max-touch-points", "preferences", "Maximum touch points", "Integer 0–16. fine + positive count supports mixed input; none + positive count or coarse + zero is rejected.", { integer: ["0", "16"], ...alias("max-touch-points") }),
  publicFlag("pointer", "preferences", "Pointer", "none + hover is rejected. This describes effective settings, not physical input hardware.", { kind: "enum", choices: ["fine", "coarse", "none"], ...alias("pointer") }),
  publicFlag("hover", "preferences", "Hover", "Use a combination consistent with pointer and touch count.", { kind: "enum", choices: ["hover", "none"], ...alias("hover") }),
  publicFlag("color-scheme", "preferences", "Color scheme", "Effective CSS query and styling preference. Playwright context colorScheme is a separate emulation setting.", { kind: "enum", choices: ["light", "dark"], ...alias("color-scheme") }),
  publicFlag("preferred-contrast", "preferences", "Preferred contrast", "Effective CSS and styling preference.", { kind: "enum", choices: ["no-preference", "more", "less"], ...alias("preferred-contrast") }),
  publicFlag("forced-colors", "preferences", "Forced colors", "Controls actual author-style color replacement as well as queries.", { kind: "enum", choices: ["active", "none", "true", "false", "1", "0"], ...alias("forced-colors") }),
  ...["reduced-motion", "reduced-transparency", "inverted-colors"].map((name) => publicFlag(name, "preferences", name.split("-").join(" ").replace(/^./, (c) => c.toUpperCase()), "Independent boolean applied to effective WebPreferences. False is an explicit override, not an unset field.", { kind: "boolean", ...alias(name) })),
  publicFlag("hdr", "preferences", "HDR", "Public value must be native; a query string cannot supply an HDR backend.", { kind: "enum", choices: ["native"], ...alias("hdr") }),
  publicFlag("keyboard-layout", "preferences", "Keyboard layout", "Public value must be native. us / en-US require explicit synthetic tests and do not install a physical layout.", { kind: "enum", choices: ["native"], ...alias("keyboard-layout") }),
  sdkField("colorScheme", "preferences", "SDK colorScheme", "enum", "Playwright context colorScheme override: light, dark or no-preference. Keep it consistent with native CSS preferences.", { choices: ["light", "dark", "no-preference"] }),
  publicFlag("noise", "privacy", "Fingerprint perturbation", "false keeps identity seeds but disables existing perturbations, optional graph audio isolation and compatibility Canvas text metrics. Native GPU policy already suppresses legacy GPU noise.", { kind: "boolean" }),
  publicFlag("allow-3p-cookies", "privacy", "Allow third-party cookies", "Launch-only opt-in, default off. Does not change persisted preferences, SameSite/Secure requirements or site-specific blocks.", { kind: "boolean" }),
  rawFlag("enable-blink-features", "privacy", "FakeShadowRoot", "Exposes closed author roots through element.shadowRoot, not UA-internal roots. This control changes only FakeShadowRoot in enable/disable feature lists and preserves other features.", { id: "fake-shadow-root", kind: "feature" }),
  sdkField("stealthArgs", "sdk", "SDK stealth defaults", "boolean", "false skips default stealth arguments and persistent seed I/O. Explicit args are retained; this is not fingerprint=off."),
  sdkField("headless", "sdk", "Headless", "boolean", "SDK launch mode. launchOptions.headless takes precedence if present; no related options are cleared."),
  sdkField("startMaximized", "sdk", "Start maximized", "boolean", "Explicit SDK window behavior. Existing --start-maximized, --window-size or --window-position retain their own precedence."),
  publicFlag("devtools-runtime-suppression", "advanced", "DevTools Runtime suppression", "SDK-documented presence switch. Can break console/binding-based automation. Unset removes this exact switch, not any independent raw alias.", { kind: "presence", ...alias("devtools-runtime-suppression") }),
  publicFlag("canvas-bridge", "advanced", "Canvas Bridge endpoint", "Forwards Canvas/WebGL operations to the configured endpoint. Requires a compatible backend and has sandbox/security implications.", alias("canvas-bridge")),
  publicFlag("canvas-bridge-unsafe", "advanced", "Unsafe Canvas Bridge opt-in", "SDK-documented presence switch. Bridge renderer processes lose their sandbox. Enable only when you accept that boundary.", { kind: "presence", ...alias("canvas-bridge-unsafe") }),
];

export interface ChromixFieldState {
  name: string;
  value: ChromixFlagValue;
  present: boolean;
  count: number;
}

export function chromixArgsProblem(options: ChromixOptions): string | undefined {
  if (options.args === undefined) return undefined;
  if (!Array.isArray(options.args) || options.args.some((arg) => typeof arg !== "string")) {
    return "options.args must be an array of strings. It is preserved; fix it in SDK JSON before editing flags.";
  }
  return undefined;
}

function argsOf(options: ChromixOptions): string[] {
  const problem = chromixArgsProblem(options);
  if (problem) throw new Error(problem);
  return (options.args as string[] | undefined) ?? [];
}

export function chromixArgName(arg: string): string {
  const separator = arg.indexOf("=");
  return separator < 0 ? arg : arg.slice(0, separator);
}

function argValue(arg: string): string | null {
  const separator = arg.indexOf("=");
  return separator < 0 ? null : arg.slice(separator + 1);
}

export function readChromixFlag(options: ChromixOptions, name: string): ChromixFieldState {
  const matches = chromixArgsProblem(options) ? [] : argsOf(options).filter((arg) => chromixArgName(arg) === name);
  return { name, present: matches.length > 0, count: matches.length, value: matches.length ? argValue(matches[matches.length - 1]) : undefined };
}

export function readChromixField(options: ChromixOptions, field: ChromixFingerprintField): ChromixFieldState {
  if (field.sdkKey) {
    const value = options[field.sdkKey];
    return { name: field.sdkKey, value: value === undefined ? undefined : value === null ? null : typeof value === "object" ? JSON.stringify(value) : String(value), present: Object.prototype.hasOwnProperty.call(options, field.sdkKey), count: Object.prototype.hasOwnProperty.call(options, field.sdkKey) ? 1 : 0 };
  }
  if (field.kind === "feature") {
    const args = chromixArgsProblem(options) ? [] : argsOf(options);
    const disabled = featureTokens(args, "--disable-blink-features").some(isShadowRoot);
    const enabled = featureTokens(args, "--enable-blink-features").some(isShadowRoot);
    return { name: "FakeShadowRoot", value: disabled ? "false" : enabled ? "true" : undefined, present: disabled || enabled, count: Number(disabled) + Number(enabled) };
  }
  // Raw aliases win regardless of their order relative to public flags.
  for (const name of field.aliases ?? []) {
    const state = readChromixFlag(options, name);
    if (state.present) return state;
  }
  return readChromixFlag(options, field.flag!);
}

export function setChromixFlag(options: ChromixOptions, name: string, value: ChromixFlagValue): ChromixOptions {
  const args = argsOf(options);
  const replacement = value === undefined ? undefined : value === null ? name : `${name}=${value}`;
  let found = false;
  const next: string[] = [];
  for (const arg of args) {
    if (chromixArgName(arg) !== name) {
      next.push(arg);
    } else {
      if (!found && replacement !== undefined) next.push(replacement);
      found = true;
    }
  }
  if (!found && replacement !== undefined) next.push(replacement);
  if (next.length === args.length && next.every((arg, index) => arg === args[index])) return options;
  return { ...options, args: next };
}

function featureTokens(args: string[], name: string): string[] {
  return args.filter((arg) => chromixArgName(arg) === name).flatMap((arg) => (argValue(arg) ?? "").split(","));
}

function isShadowRoot(token: string): boolean {
  return token.trim().split(/[<:]/, 1)[0] === "FakeShadowRoot";
}

export function setChromixShadowRoot(options: ChromixOptions, value: ChromixFlagValue): ChromixOptions {
  let next = options;
  for (const [name, enabled] of [["--enable-blink-features", "true"], ["--disable-blink-features", "false"]] as const) {
    const tokens = featureTokens(argsOf(next), name);
    const hadFeature = tokens.some(isShadowRoot);
    const shouldContain = value === enabled;
    if (!hadFeature && !shouldContain) continue;
    const retained = tokens.filter((token) => !isShadowRoot(token));
    if (shouldContain) retained.push("FakeShadowRoot");
    next = setChromixFlag(next, name, retained.some((token) => token.length > 0) ? retained.join(",") : undefined);
  }
  return next;
}

export function setChromixField(options: ChromixOptions, field: ChromixFingerprintField, value: ChromixFlagValue, name?: string): ChromixOptions {
  if (field.sdkKey) {
    const next = { ...options };
    if (value === undefined) delete next[field.sdkKey];
    else if (field.kind === "boolean") next[field.sdkKey] = value === null ? null : value === "true";
    else next[field.sdkKey] = value;
    return next;
  }
  if (field.kind === "feature") return setChromixShadowRoot(options, value);
  const target = name ?? readChromixField(options, field).name;
  if (![field.flag, ...(field.aliases ?? [])].includes(target)) throw new Error("Unknown field parameter.");
  return setChromixFlag(options, target, value);
}

export function parseChromixRawArgs(text: string): string[] {
  return text.split(/\r?\n/).filter((line) => line.trim() !== "").map((line, index) => {
    if (line.includes("\0") || !/^--[A-Za-z0-9][A-Za-z0-9-]*(?:=.*)?$/.test(line)) {
      throw new Error(`Line ${index + 1}: use one --flag or --flag=value per line, without shell quotes around the argument.`);
    }
    return line;
  });
}

const TRUE_VALUES = ["true", "1", "on", "enable", "enabled"];
const FALSE_VALUES = ["false", "0", "off", "disable", "disabled"];

export function validateChromixValue(field: ChromixFingerprintField, value: ChromixFlagValue): string | undefined {
  if (value === undefined) return undefined;
  if (field.kind === "seed") {
    if (value === null || value === "" || FALSE_VALUES.includes(value.toLowerCase())) return undefined;
    return !/^[0-9]+$/.test(value) || BigInt(value) < 1n || BigInt(value) > BigInt(UINT64_MAX) ? `Use a nonzero decimal uint64 (1–${UINT64_MAX}), a bare flag, or off.` : undefined;
  }
  if (field.kind === "boolean") {
    return value === null || value === "" || [...TRUE_VALUES, ...FALSE_VALUES].includes(value.toLowerCase()) ? undefined : "Use a public boolean spelling: true/1/on/enable/enabled or false/0/off/disable/disabled.";
  }
  if (field.kind === "presence") return value === null ? undefined : "This documented opt-in uses a bare presence switch; remove it to leave it unset.";
  if (field.integer) {
    const [min, max] = field.integer;
    return value === null || !/^[0-9]+$/.test(value) || BigInt(value) < BigInt(min) || BigInt(value) > BigInt(max) ? `Use a decimal integer from ${min} to ${max}${field.unit ? ` ${field.unit}` : ""}.` : undefined;
  }
  if (field.decimal) {
    const [min, max] = field.decimal;
    const numeric = Number(value);
    return value === null || !/^(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$/.test(value) || !Number.isFinite(numeric) || numeric < min || numeric > max || (min === 0 && numeric === 0) || (field.id === "device-memory" && /[eE]/.test(value)) ? `Use a decimal ${min === 0 ? "greater than 0" : `at least ${min}`} and at most ${max}.` : undefined;
  }
  if (field.id === "brand-version" || field.id === "platform-version") {
    if (value === null || !/^[0-9]+(?:\.[0-9]+){0,3}$/.test(value) || value.split(".").some((part) => BigInt(part) > 4294967295n)) return "Use a numeric version with at most four uint32 components.";
    if (field.id === "brand-version" && (BigInt(value.split(".")[0]) < 1n || BigInt(value.split(".")[0]) > 2147483647n)) return "Brand major version must be a positive int32.";
  }
  if (field.kind === "enum" && !field.choices?.includes(value ?? "")) {
    if (field.id === "brand" && ["chrome", "google chrome", "edge", "microsoft edge", "opera", "vivaldi"].includes((value ?? "").toLowerCase())) return undefined;
    if (field.id === "platform" && field.choices?.some((choice) => choice.toLowerCase() === value?.toLowerCase())) return undefined;
    if (field.id.startsWith("codec-") && value?.split(",").includes("supported") && value.split(",").every((token) => ["supported", "smooth", "power-efficient"].includes(token))) return undefined;
    return `Public values: ${field.choices?.map((choice) => choice === "" ? "(explicit empty = disabled)" : choice).join(", ")}. Existing custom values are preserved.`;
  }
  return undefined;
}

export function chromixFingerprintNotices(options: ChromixOptions): string[] {
  const notices: string[] = [];
  const argsProblem = chromixArgsProblem(options);
  if (argsProblem) notices.push(argsProblem);
  for (const key of ["launchOptions", "contextOptions"]) {
    const nested = options[key];
    if (nested && typeof nested === "object" && !Array.isArray(nested) && Object.prototype.hasOwnProperty.call(nested, "args")) notices.push(`${key}.args exists. The SDK can select this argument array instead of options.args. This form edits only options.args; review the override in SDK JSON. It has not been changed.`);
  }
  if (Object.prototype.hasOwnProperty.call(options, "devicePool")) notices.push("devicePool is measured-device mode and rejects ordinary field/launch/context overrides. Editing these fields does not certify a device; remove conflicts explicitly in SDK JSON.");
  if (options.viewport !== undefined) notices.push("SDK viewport is explicitly configured, independently of raw screen/viewport flags. It is preserved; edit it in SDK JSON (null keeps native viewport behavior).");
  if (argsProblem) return notices;
  for (const field of CHROMIX_FINGERPRINT_FIELDS) {
    if (!field.flag || !field.aliases?.length) continue;
    const publicState = readChromixFlag(options, field.flag);
    const raw = field.aliases.map((name) => readChromixFlag(options, name)).find((state) => state.present);
    if (raw && publicState.present && raw.value !== publicState.value) notices.push(`${raw.name} takes precedence over ${field.flag}; both are retained.${["screen-width", "screen-height", "taskbar-height"].includes(field.id) ? " The SDK rejects conflicting geometry aliases in synthetic mode." : ""}`);
  }
  for (const name of ["timezone", "locale"]) {
    if (typeof options[name] === "string" && options[name] && readChromixFlag(options, `--fingerprint-${name}`).present) notices.push(`SDK ${name} overwrites --fingerprint-${name} at launch; a raw --uxr-${name === "locale" ? "languages" : name} still wins. Both stored settings are retained.`);
  }
  const fieldValue = (id: string) => readChromixField(options, CHROMIX_FINGERPRINT_FIELDS.find((field) => field.id === id)!).value;
  const fingerprint = fieldValue("fingerprint");
  if (typeof fingerprint === "string" && FALSE_VALUES.includes(fingerprint.toLowerCase())) notices.push("fingerprint=off disables persona overrides at launch; explicit regional settings, cookie and FakeShadowRoot opt-ins remain independent. Stored fields have not been erased.");
  const pointer = fieldValue("pointer"), touch = fieldValue("max-touch-points"), hover = fieldValue("hover");
  if ((pointer === "none" && typeof touch === "string" && /^[0-9]+$/.test(touch) && BigInt(touch) > 0n) || (pointer === "coarse" && typeof touch === "string" && /^0+$/.test(touch)) || (pointer === "none" && hover === "hover")) notices.push("The SDK rejects this pointer / hover / max-touch-points combination. none cannot have touch or hover; coarse cannot have zero touch points.");
  if (fieldValue("font-policy") === "restricted" && !fieldValue("font-whitelist") && !options.fontsDir) notices.push("Restricted font policy requires 1–256 installed font families or a fontsDir containing parseable real fonts.");
  const width = fieldValue("uxr-viewport-width"), height = fieldValue("uxr-viewport-height");
  if ((width === undefined) !== (height === undefined)) notices.push("Supply both --uxr-viewport-width and --uxr-viewport-height; the SDK rejects incomplete synthetic viewport pairs.");
  if ((fieldValue("gpu-vendor") !== undefined || fieldValue("gpu-renderer") !== undefined) && fieldValue("gpu-backend") !== "compatibility") notices.push("Explicit WebGL vendor/renderer presentation requires compatibility GPU mode. Native mode and software-context guards retain real identity.");
  return notices;
}
