import { useEffect, useId, useState, type JSX } from "react";
import { CHROMIX_FINGERPRINT_FIELDS, chromixArgsProblem, readChromixField, setChromixFlag } from "../../lib/chromixFingerprint";
import { fingerprintMode, randomFingerprintSeed, seedProblem, storedFingerprintSeed, type FingerprintMode } from "../../lib/fingerprintMode";

interface Props {
  options: Record<string, unknown>;
  onChange: (options: Record<string, unknown>) => void;
  inherited?: Record<string, unknown>;
  profile?: boolean;
  onValidityChange?: (valid: boolean) => void;
}

const control = "w-full min-w-0 rounded-lg bg-white/5 px-2.5 py-2 text-[12px] text-slate-200";
const modes: { value: FingerprintMode; label: string; description: string }[] = [
  { value: "random", label: "Random each launch", description: "A fresh cryptographic seed on every launch. The generated seed is never written back to this profile." },
  { value: "fixed", label: "Fixed seed", description: "Reuse one seed for reproducible fingerprint noise across launches with the same browser and configuration." },
  { value: "custom", label: "Custom", description: "Reuse a seed and choose individual native fingerprint parameters below." },
];
const choices: { id: string; label: string; values: string[] }[] = [
  { id: "platform", label: "Platform", values: ["macos", "windows", "linux"] },
  { id: "locale", label: "Language", values: ["en-US", "en-GB", "zh-CN", "ja-JP", "de-DE", "fr-FR"] },
  { id: "timezone", label: "Timezone", values: ["America/New_York", "America/Los_Angeles", "Europe/London", "Europe/Berlin", "Asia/Shanghai", "Asia/Tokyo", "UTC"] },
  { id: "hardware-concurrency", label: "CPU cores", values: ["2", "4", "8", "16"] },
  { id: "device-memory", label: "Memory (GB)", values: ["2", "4", "8", "16", "32"] },
];

export function SimpleFingerprint({ options, onChange, inherited = {}, profile = false, onValidityChange }: Props): JSX.Element {
  const id = useId();
  const effective = { ...inherited, ...options };
  const mode = fingerprintMode(options);
  const effectiveMode = fingerprintMode(effective);
  const storedSeed = storedFingerprintSeed(effective) ?? "";
  const [seed, setSeed] = useState(storedSeed);
  const [error, setError] = useState<string>();
  useEffect(() => { setSeed(storedSeed); setError(undefined); }, [storedSeed, mode]);
  useEffect(() => { onValidityChange?.(!error); }, [error, onValidityChange]);

  function choose(value: FingerprintMode): void {
    const next = { ...options, fingerprintMode: value };
    onChange(value === "random" ? next : { ...next, fingerprintSeed: storedFingerprintSeed(effective) ?? randomFingerprintSeed() });
  }

  function changeSeed(value: string): void {
    setSeed(value);
    const problem = seedProblem(value);
    setError(problem);
    if (!problem) onChange({ ...options, fingerprintMode: mode === "custom" ? "custom" : "fixed", fingerprintSeed: value });
  }

  function setParameter(fieldId: string, value: string, current = options): Record<string, unknown> {
    const field = CHROMIX_FINGERPRINT_FIELDS.find((field) => field.id === fieldId)!;
    let next = current;
    // An explicit simple selection replaces competing aliases for this field only.
    if (next.args === undefined && inherited.args !== undefined) next = { ...next, args: inherited.args };
    for (const name of [field.flag!, ...(field.aliases ?? [])]) next = setChromixFlag(next, name, undefined);
    return setChromixFlag(next, field.aliases?.[0] ?? field.flag!, value || undefined);
  }

  const screenWidth = readChromixField(effective, CHROMIX_FINGERPRINT_FIELDS.find((field) => field.id === "screen-width")!).value;
  const screenHeight = readChromixField(effective, CHROMIX_FINGERPRINT_FIELDS.find((field) => field.id === "screen-height")!).value;
  const screen = screenWidth && screenHeight ? `${screenWidth}x${screenHeight}` : "";
  const screens = ["1366x768", "1440x900", "1920x1080", "2560x1440"];
  const argsProblem = chromixArgsProblem(effective);

  return (
    <section aria-label="Fingerprint mode" className="min-w-0 space-y-3">
      <h3 className="text-[13px] font-medium text-slate-200">Fingerprint mode</h3>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
        {modes.map((item) => (
          <button type="button" key={item.value} aria-pressed={mode === item.value} onClick={() => choose(item.value)} className="rounded-lg border p-3 text-left text-[12px]" style={{ borderColor: mode === item.value ? "#a855f7" : "#ffffff20", background: mode === item.value ? "#a855f71a" : "transparent" }}>
            {item.label}
          </button>
        ))}
      </div>
      {!mode && <p className="text-[11px] text-slate-400" role="note">
        {profile ? `Using global defaults${effectiveMode ? ` (${modes.find((item) => item.value === effectiveMode)?.label})` : " / existing advanced options"}.` : "Using existing options and the stable per-profile seed. Choose Random each launch for a fresh seed each time."}
        {" "}Nothing changes until you choose a mode. Existing options are preserved.
      </p>}
      {mode && <p className="text-[12px] text-slate-400">{modes.find((item) => item.value === mode)?.description}</p>}
      {mode && <p className="text-[11px] text-slate-500">The selected mode and seed take priority over old seed flags, including nested argument arrays. Random and fixed modes use only the seed, without the old profile fingerprint defaults. Custom and existing legacy options retain those profile fields. Explicit non-seed arguments are preserved and still apply. Random mode ignores any saved seed.</p>}
      {profile && mode && <button type="button" className="text-[12px] text-purple-300" onClick={() => {
        const next = { ...options };
        delete next.fingerprintMode;
        delete next.fingerprintSeed;
        onChange(next);
      }}>Use global defaults / existing options</button>}
      {(mode === "fixed" || mode === "custom") && <div className="space-y-2">
        <label htmlFor={`${id}-seed`} className="block text-[12px] text-slate-300">Fingerprint seed</label>
        <div className="flex flex-wrap gap-2">
          <input id={`${id}-seed`} type="text" inputMode="numeric" value={seed} onChange={(event) => changeSeed(event.target.value)} aria-invalid={!!error} aria-describedby={`${id}-seed-help`} className={`${control} flex-1 basis-[180px] font-mono`} />
          <button type="button" className="btn-secondary rounded-lg px-3 py-2 text-[12px]" onClick={() => changeSeed(randomFingerprintSeed())}>Random seed</button>
        </div>
        <p id={`${id}-seed-help`} className="text-[11px] text-slate-500">Random seed picks a value once and saves it. It does not enable random-per-launch mode. Stored as exact decimal text, never rounded.</p>
        {error && <p role="alert" className="text-[12px] text-red-300">{error} The last valid seed is unchanged.</p>}
      </div>}
      {mode === "custom" && <fieldset disabled={!!argsProblem} className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
        {choices.map((choice) => {
          const field = CHROMIX_FINGERPRINT_FIELDS.find((field) => field.id === choice.id)!;
          const value = readChromixField(effective, field).value ?? "";
          return <label key={choice.id} className="min-w-0 space-y-1 text-[12px] text-slate-400">{choice.label}
            <select aria-label={choice.label} className={control} value={value} onChange={(event) => onChange(setParameter(choice.id, event.target.value))}>
              <option value="">Browser default / other advanced settings</option>
              {choice.values.map((value) => <option key={value} value={value}>{value}</option>)}
              {value && !choice.values.includes(value) && <option value={value}>{value} (stored)</option>}
            </select>
          </label>;
        })}
        <label className="min-w-0 space-y-1 text-[12px] text-slate-400">Screen size
          <select aria-label="Screen size" className={control} value={screen} onChange={(event) => {
            const [width = "", height = ""] = event.target.value.split("x");
            onChange(setParameter("screen-height", height, setParameter("screen-width", width)));
          }}>
            <option value="">Browser default / other advanced settings</option>
            {screens.map((value) => <option key={value} value={value}>{value.replace("x", " × ")}</option>)}
            {screen && !screens.includes(screen) && <option value={screen}>{screen} (stored)</option>}
          </select>
        </label>
        <p className="text-[11px] text-slate-500 sm:col-span-2">Native flag support depends on the local Chromix binary. These choices do not change physical hardware. More parameters and stored aliases are in Advanced.</p>
      </fieldset>}
      {argsProblem && mode === "custom" && <p role="alert" className="text-[12px] text-amber-200">Correct the argument array in Advanced before editing custom parameters.</p>}
    </section>
  );
}
