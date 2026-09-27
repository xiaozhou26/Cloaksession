import assert from "node:assert/strict";
import { test } from "node:test";
import {
  CHROMIX_FINGERPRINT_FIELDS as fields,
  CHROMIX_FINGERPRINT_GROUPS as groups,
  CHROMIX_FINGERPRINT_SOURCE as source,
  chromixArgsProblem,
  chromixFingerprintNotices,
  parseChromixRawArgs,
  readChromixField,
  readChromixFlag,
  setChromixField,
  setChromixFlag,
  setChromixShadowRoot,
  validateChromixValue,
} from "./chromixFingerprint.ts";

const byId = (id) => {
  const field = fields.find((candidate) => candidate.id === id);
  assert.ok(field, `Missing catalog field: ${id}`);
  return field;
};
const freeze = (value) => {
  if (value && typeof value === "object") {
    for (const child of Object.values(value)) freeze(child);
    Object.freeze(value);
  }
  return value;
};

const publicFlags = [
  "--fingerprint", "--fingerprint-platform", "--fingerprint-gpu-backend",
  "--fingerprint-gpu-vendor", "--fingerprint-gpu-renderer", "--fingerprint-hardware-concurrency",
  "--fingerprint-device-memory", "--fingerprint-screen-width", "--fingerprint-screen-height",
  "--fingerprint-brand", "--fingerprint-brand-version", "--fingerprint-platform-version",
  "--fingerprint-timezone", "--fingerprint-locale", "--fingerprint-storage-quota",
  "--fingerprint-taskbar-height", "--fingerprint-windows-font-metrics", "--fingerprint-font-policy",
  "--fingerprint-font-whitelist", "--fingerprint-audio-render", "--fingerprint-audio-seed",
  "--fingerprint-timer-resolution", "--fingerprint-codec-h264", "--fingerprint-codec-vp8",
  "--fingerprint-codec-vp9", "--fingerprint-codec-av1", "--fingerprint-codec-hevc",
  "--fingerprint-max-touch-points", "--fingerprint-pointer", "--fingerprint-hover",
  "--fingerprint-color-scheme", "--fingerprint-preferred-contrast", "--fingerprint-forced-colors",
  "--fingerprint-reduced-motion", "--fingerprint-reduced-transparency", "--fingerprint-inverted-colors",
  "--fingerprint-hdr", "--fingerprint-keyboard-layout", "--fingerprint-webrtc-ip",
  "--fingerprint-noise", "--fingerprint-allow-3p-cookies", "--fingerprint-sapi-voices",
  "--enable-blink-features",
];

test("catalog covers every expanded public contract table flag and SDK high-risk opt-in", () => {
  const names = new Set(fields.map((field) => field.flag));
  for (const name of publicFlags) assert.ok(names.has(name), name);
  for (const name of ["--fingerprint-devtools-runtime-suppression", "--fingerprint-canvas-bridge", "--fingerprint-canvas-bridge-unsafe"]) assert.ok(names.has(name), name);
  assert.equal(new Set(fields.map((field) => field.id)).size, fields.length);
  assert.equal(new Set(fields.filter((field) => field.flag).map((field) => field.flag)).size, fields.filter((field) => field.flag).length);
  assert.match(source.commit, /^[a-f0-9]{40}$/);
  for (const field of fields) {
    assert.ok(groups.some((group) => group.id === field.group), field.id);
    assert.ok(field.description.length > 20, field.id);
    assert.ok(field.flag || field.sdkKey, field.id);
    assert.equal(readChromixField({}, field).present, false, field.id);
  }
});

test("catalog exposes actual geometry names, units and required SDK properties", () => {
  for (const name of ["screen-avail-width", "screen-avail-height", "device-pixel-ratio", "viewport-width", "viewport-height", "outer-width", "outer-height", "window-x", "window-y"]) assert.equal(byId(`uxr-${name}`).flag, `--uxr-${name}`);
  for (const key of ["userAgent", "fontsDir", "locale", "timezone", "geoip", "colorScheme", "stealthArgs", "headless", "startMaximized"]) assert.equal(byId(`sdk-${key}`).sdkKey, key);
  assert.equal(byId("storage-quota").unit, "MiB");
  assert.equal(byId("timer-resolution").unit, "ms");
  assert.equal(byId("device-memory").unit, "GB");
  assert.deepEqual(byId("hdr").choices, ["native"]);
  assert.deepEqual(byId("keyboard-layout").choices, ["native"]);
  assert.equal(fields.some((field) => field.flag?.includes("fake-srflx")), false);
});

test("reading an empty catalog does not inject any SDK or engine defaults", () => {
  const options = freeze({ future: { preserve: false } });
  for (const field of fields) assert.equal(readChromixField(options, field).value, undefined);
  assert.deepEqual(chromixFingerprintNotices(options), []);
  assert.deepEqual(options, { future: { preserve: false } });
});

for (const field of fields) {
  test(`isolated edit preserves unrelated arguments and SDK properties: ${field.id}`, () => {
    const options = freeze({
      args: ["--future-flag=left=right", "--future-flag=second", "--fingerprint-unlisted=false"],
      future: { zero: 0, off: false, nested: ["preserve"] },
      contextOptions: { args: ["--nested-unknown"] },
    });
    const value = field.kind === "presence" ? null : field.kind === "boolean" || field.kind === "feature" ? "false" : field.choices?.[0] ?? "123";
    const edited = setChromixField(options, field, value);
    assert.notEqual(edited, options);
    assert.equal(edited.future, options.future);
    assert.equal(edited.contextOptions, options.contextOptions);
    assert.deepEqual(edited.args.slice(0, 3), options.args);
    assert.equal(readChromixField(edited, field).value, value);
    const reset = setChromixField(edited, field, undefined);
    assert.equal(readChromixField(reset, field).present, false);
    assert.deepEqual(reset, options);
  });
}

test("exact uint64 seed and zero quota survive JSON round trips without Number conversion", () => {
  let options = setChromixField({}, byId("fingerprint"), "18446744073709551615");
  options = setChromixField(options, byId("audio-seed"), "18446744073709551614");
  options = setChromixField(options, byId("storage-quota"), "0");
  options = JSON.parse(JSON.stringify(options));
  assert.deepEqual(options.args, ["--fingerprint=18446744073709551615", "--fingerprint-audio-seed=18446744073709551614", "--fingerprint-storage-quota=0"]);
  assert.equal(validateChromixValue(byId("fingerprint"), "18446744073709551615"), undefined);
  assert.ok(validateChromixValue(byId("fingerprint"), "18446744073709551616"));
  assert.ok(validateChromixValue(byId("audio-seed"), "0"));
  assert.equal(validateChromixValue(byId("storage-quota"), "8796093022207"), undefined);
  assert.ok(validateChromixValue(byId("storage-quota"), "8796093022208"));
});

test("off/false, bare flags, explicit empty codec and leading zero strings are distinct", () => {
  for (const value of ["off", "false", "0", "disable", "DISABLED", "00000000000000000042", "", null]) {
    const options = setChromixField({}, byId("fingerprint"), value);
    assert.equal(readChromixField(options, byId("fingerprint")).value, value);
    assert.equal(validateChromixValue(byId("fingerprint"), value), undefined);
  }
  const options = { args: ["--fingerprint-noise=false", "--fingerprint-codec-hevc=", "--fingerprint-allow-3p-cookies"], geoip: false };
  assert.equal(readChromixField(options, byId("noise")).value, "false");
  assert.equal(readChromixField(options, byId("codec-hevc")).value, "");
  assert.equal(readChromixField(options, byId("allow-3p-cookies")).value, null);
  assert.equal(readChromixField(options, byId("sdk-geoip")).value, "false");
  assert.deepEqual(setChromixField({}, byId("sdk-geoip"), "false"), { geoip: false });
});

test("editing duplicates replaces every exact-name occurrence without touching similar names or values", () => {
  const options = freeze({ args: ["--fingerprint=1", "--fingerprint-noise=false", "--fingerprint", "--other=a=b", "--fingerprint=3", "--fingerprint-audio-seed=7"] });
  assert.equal(readChromixFlag(options, "--fingerprint").value, "3");
  assert.equal(readChromixFlag(options, "--fingerprint").count, 3);
  const edited = setChromixFlag(options, "--fingerprint", "off");
  assert.deepEqual(edited.args, ["--fingerprint=off", "--fingerprint-noise=false", "--other=a=b", "--fingerprint-audio-seed=7"]);
  assert.deepEqual(setChromixFlag(edited, "--fingerprint", undefined).args, ["--fingerprint-noise=false", "--other=a=b", "--fingerprint-audio-seed=7"]);
  assert.equal(setChromixFlag(edited, "--not-present", undefined), edited);
});

test("raw aliases win independently of array order and never erase public aliases", () => {
  const field = byId("hardware-concurrency");
  assert.deepEqual(field.aliases, ["--uxr-hw-concurrency"]);
  for (const args of [["--uxr-hw-concurrency=2", "--fingerprint-hardware-concurrency=8"], ["--fingerprint-hardware-concurrency=8", "--uxr-hw-concurrency=2"]]) {
    const options = freeze({ args });
    assert.equal(readChromixField(options, field).name, "--uxr-hw-concurrency");
    assert.equal(readChromixField(options, field).value, "2");
    const next = setChromixField(options, field, "4");
    assert.ok(next.args.includes("--fingerprint-hardware-concurrency=8"));
    assert.ok(next.args.includes("--uxr-hw-concurrency=4"));
    const publicEdit = setChromixField(next, field, "16", field.flag);
    assert.equal(readChromixField(publicEdit, field).value, "4");
    assert.equal(readChromixFlag(publicEdit, field.flag).value, "16");
    assert.equal(readChromixField(setChromixField(publicEdit, field, undefined), field).value, "16");
    assert.throws(() => setChromixField(options, field, "1", "--unrelated"), /Unknown field/);
  }
});

test("special native alias spellings match source, including brand and language lists", () => {
  for (const [id, name] of [["gpu-vendor", "webgl-vendor"], ["gpu-renderer", "webgl-renderer"], ["brand", "ua-brand"], ["brand-version", "ua-brand-version"], ["platform-version", "ua-platform-version"], ["locale", "languages"]]) assert.deepEqual(byId(id).aliases, [`--uxr-${name}`]);
  const options = { args: ["--uxr-languages=en-US,ja-JP", "--fingerprint-locale=de-DE"] };
  assert.equal(readChromixField(options, byId("locale")).value, "en-US,ja-JP");
  assert.deepEqual(setChromixField(options, byId("locale"), "fr-FR").args, ["--uxr-languages=fr-FR", "--fingerprint-locale=de-DE"]);
});

test("geometry alias conflicts and high-level SDK conflicts are warned, not silently normalized", () => {
  const options = freeze({ args: ["--uxr-synthetic-device-tests=true", "--uxr-screen-width=1440", "--fingerprint-screen-width=1920", "--fingerprint-timezone=Europe/London"], timezone: "Asia/Tokyo", launchOptions: { args: ["--native=value"] }, contextOptions: { args: ["--other=value"] }, devicePool: {}, viewport: null });
  const notices = chromixFingerprintNotices(options).join("\n");
  for (const text of ["conflicting geometry aliases", "SDK timezone overwrites", "launchOptions.args exists", "contextOptions.args exists", "devicePool", "viewport"]) assert.ok(notices.includes(text), text);
  assert.deepEqual(setChromixField(options, byId("screen-width"), "1600").args, ["--uxr-synthetic-device-tests=true", "--uxr-screen-width=1600", "--fingerprint-screen-width=1920", "--fingerprint-timezone=Europe/London"]);
});

test("FakeShadowRoot changes only its tokens, preserves other features, and supports explicit false", () => {
  const field = byId("fake-shadow-root");
  const options = freeze({ args: ["--enable-blink-features=Alpha,FakeShadowRoot", "--unknown=false", "--enable-blink-features=Beta:param/value", "--disable-blink-features=Gamma,FakeShadowRoot"] });
  assert.equal(readChromixField(options, field).value, "false");
  const enabled = setChromixShadowRoot(options, "true");
  assert.deepEqual(enabled.args, ["--enable-blink-features=Alpha,Beta:param/value,FakeShadowRoot", "--unknown=false", "--disable-blink-features=Gamma"]);
  assert.equal(readChromixField(enabled, field).value, "true");
  const disabled = setChromixShadowRoot(enabled, "false");
  assert.deepEqual(disabled.args, ["--enable-blink-features=Alpha,Beta:param/value", "--unknown=false", "--disable-blink-features=Gamma,FakeShadowRoot"]);
  const unset = setChromixShadowRoot(disabled, undefined);
  assert.deepEqual(unset.args, ["--enable-blink-features=Alpha,Beta:param/value", "--unknown=false", "--disable-blink-features=Gamma"]);
  assert.equal(readChromixField(unset, field).present, false);
});

test("removing absent FakeShadowRoot does not rewrite any feature list", () => {
  const options = freeze({ args: ["--enable-blink-features=Alpha", "--enable-blink-features=Beta", "--disable-blink-features=Gamma"] });
  assert.equal(setChromixShadowRoot(options, undefined), options);
  assert.deepEqual(setChromixShadowRoot({ args: ["--enable-blink-features=FakeShadowRoot"] }, undefined), { args: [] });
  assert.deepEqual(setChromixShadowRoot({ args: ["--enable-blink-features=FakeShadowRoot<Trial,Alpha"] }, undefined), { args: ["--enable-blink-features=Alpha"] });
});

test("raw editor preserves duplicates, spaces, extra equals signs, Unicode and numeric precision", () => {
  const args = ["--fingerprint=18446744073709551615", "--custom=value=with=equals", "--custom=duplicate", "--fingerprint-font-whitelist=Arial,微软雅黑", "--user-agent=Example with spaces", "--fingerprint-codec-av1=", "--bare"];
  assert.deepEqual(parseChromixRawArgs(args.join("\r\n")), args);
  assert.deepEqual(parseChromixRawArgs("\n\n"), []);
  for (const invalid of ["--fingerprint 42", "'--fingerprint=42'", "--bad\0=value", "not-a-flag", " --leading-space=bad"]) assert.throws(() => parseChromixRawArgs(invalid), /Line 1/);
});

test("malformed args are not discarded; SDK properties can still be edited safely", () => {
  for (const args of [null, {}, "--fingerprint=42", ["--fine", 0]]) {
    const options = freeze({ args, future: false });
    assert.ok(chromixArgsProblem(options));
    assert.throws(() => setChromixFlag(options, "--fingerprint", "42"), /array of strings/);
    assert.equal(readChromixField(options, byId("fingerprint")).present, false);
    assert.deepEqual(setChromixField(options, byId("sdk-geoip"), "false"), { args, future: false, geoip: false });
    assert.ok(chromixFingerprintNotices(options).length > 0);
  }
});

test("numeric and enum validation follows public units and bounds without mutation", () => {
  for (const [id, accepted, rejected] of [
    ["hardware-concurrency", ["1", "128"], ["0", "129", "1.5"]],
    ["screen-width", ["1", "32768"], ["0", "32769", "1e3"]],
    ["timer-resolution", ["0", "7", "1000"], ["-1", "0.5", "1001"]],
    ["device-memory", [".25", "0.5", "32"], ["0", "33", "1e1"]],
    ["uxr-device-pixel-ratio", ["0.25", "8", "1e0"], ["0.24", "8.1"]],
    ["brand-version", ["152.0.0.0", "1"], ["0", "2147483648", "1.2.3.4.5"]],
    ["platform-version", ["0", "10.0.0"], ["1.4294967296", "not-version"]],
    ["hdr", ["native"], ["true", "hdr"]],
    ["keyboard-layout", ["native"], ["us", "en-US"]],
    ["codec-h264", ["native", "disabled", "", "supported,smooth,power-efficient"], ["enabled", "fake"]],
  ]) {
    for (const value of accepted) assert.equal(validateChromixValue(byId(id), value), undefined, `${id}=${value}`);
    for (const value of rejected) assert.ok(validateChromixValue(byId(id), value), `${id}=${value}`);
  }
});

test("public boolean aliases retain exact spellings and never become unset", () => {
  const field = byId("noise");
  for (const value of ["true", "1", "on", "enable", "enabled", "false", "0", "off", "disable", "disabled", "OFF", "", null]) {
    assert.equal(validateChromixValue(field, value), undefined);
    assert.equal(readChromixField(setChromixField({}, field, value), field).value, value);
  }
  assert.ok(validateChromixValue(field, "nope"));
});

test("off mode preserves all stored overrides and reports native-policy boundaries", () => {
  const options = freeze({ args: ["--fingerprint=42", "--fingerprint-gpu-vendor=Custom GPU", "--fingerprint-pointer=none", "--fingerprint-max-touch-points=1", "--fingerprint-font-policy=restricted", "--uxr-viewport-width=1000"], locale: "en-US" });
  const edited = setChromixField(options, byId("fingerprint"), "off");
  assert.deepEqual(edited.args.slice(1), options.args.slice(1));
  assert.equal(edited.locale, "en-US");
  const notices = chromixFingerprintNotices(edited).join("\n");
  for (const text of ["fingerprint=off", "pointer / hover / max-touch-points", "Restricted font policy", "Supply both", "requires compatibility"]) assert.ok(notices.includes(text), text);
});
