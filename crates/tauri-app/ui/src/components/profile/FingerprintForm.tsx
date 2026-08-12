import { dialog, fingerprint as fingerprintApi, proxy as proxyApi } from "../../lib/ipc";
import { useEffect, useRef, useState, type JSX, type ReactNode } from "react";
import { AlertTriangle, FolderOpen, MapPin, RefreshCw } from "lucide-react";
import type {
  DeviceCatalogEntry,
  FingerprintConfig,
  LocaleCatalogEntry,
  ProxyConfig,
  ProxyGeoResult,
} from "../../types";

/**
 * Coherent fingerprint editor — shared between NewProfileSheet and
 * ProfileEditSheet so create + edit have identical UX.
 *
 * Three coherent dropdowns (Device → Locale → Screen) that filter each other.
 * One Regen button that picks a fresh device + locale + screen combo.
 * Read-only UA chip showing the derived User-Agent.
 *
 * If the parent passes a `proxy`, this component additionally shows:
 *   - "Detect from proxy" button next to Locale → probes ipapi.co through
 *      the proxy and auto-suggests matching locale group
 *   - A warning chip when locale country ≠ proxy country
 */

interface Props {
  fingerprint: FingerprintConfig;
  onChange: (next: FingerprintConfig) => void;
  /** When set, enables the "Detect from proxy" button + coherence warning. */
  proxy?: ProxyConfig | undefined;
}

export function FingerprintForm({ fingerprint, onChange, proxy }: Props): JSX.Element {
  const [devices, setDevices] = useState<ReadonlyArray<DeviceCatalogEntry>>([]);
  const [locales, setLocales] = useState<ReadonlyArray<LocaleCatalogEntry>>([]);
  const [detecting, setDetecting] = useState(false);
  const [detectError, setDetectError] = useState<string | null>(null);
  const [detected, setDetected] = useState<ProxyGeoResult | null>(null);
  const latestFingerprint = useRef(fingerprint);
  const reconcileRequest = useRef(0);
  latestFingerprint.current = fingerprint;

  useEffect(() => {
    void fingerprintApi.devices().then(setDevices);
    void fingerprintApi.locales().then(setLocales);
  }, []);

  // Reset detection state when proxy changes
  useEffect(() => {
    setDetected(null);
    setDetectError(null);
  }, [proxy?.host, proxy?.port, proxy?.username]);

  const currentDevice = devices.find((d) => d.family === fingerprint.device);
  const currentLocale = locales.find((l) => l.locale === fingerprint.locale);

  async function regen(): Promise<void> {
    const request = ++reconcileRequest.current;
    const next = await fingerprintApi.generate();
    if (request === reconcileRequest.current) onChange(next);
  }

  async function reconcile(
    patch: Parameters<typeof fingerprintApi.reconcile>[1],
  ): Promise<void> {
    const request = ++reconcileRequest.current;
    const next = await fingerprintApi.reconcile(latestFingerprint.current, patch);
    if (request === reconcileRequest.current) onChange(next);
  }

  async function detectFromProxy(): Promise<void> {
    if (!proxy || !proxy.host) return;
    setDetecting(true);
    setDetectError(null);
    try {
      const geo = await proxyApi.detectGeo(proxy);
      setDetected(geo);
      // Resolve locale for the proxy's country. Falls back to a culturally
      // adjacent locale for countries that don't have an explicit preset
      // (e.g. Liechtenstein → de-DE, San Marino → it-IT).
      const localeId = await fingerprintApi.localeForCountry(
        geo.country,
      );
      if (!localeId) {
        setDetectError(
          `Proxy is in ${geo.countryName} (${geo.country}) — no locale preset matches. Pick a locale manually below.`,
        );
        return;
      }
      const match = locales.find((l) => l.id === localeId);
      if (!match) {
        setDetectError(
          `Locale ${localeId} not in catalog. Pick one manually.`,
        );
        return;
      }
      // The proxy's actual timezone always wins — it's ground truth from
      // the exit IP. Only fall back to the locale preset if the probe
      // returned no timezone (rare). Using the locale's timezone instead
      // would recreate the locale/timezone mismatch anti-bot systems flag
      // (e.g. en-GB locale + Europe/London tz for a Singapore proxy).
      const tz = geo.timezone || match.timezones[0];
      // Pass the proxy's detected country so the fingerprint country
      // matches the exit IP even when the locale is a fallback (e.g.
      // en-GB locale for a SG proxy — without this, country would be GB
      // from the locale, mismatching the SG proxy and re-triggering the
      // anti-bot warning).
      await reconcile({
        localeId: match.id,
        timezone: tz,
        country: geo.country.toUpperCase(),
      });
    } catch (e) {
      setDetectError((e as Error).message);
    } finally {
      setDetecting(false);
    }
  }

  // Proxy-coherence warning: locale country must match detected proxy
  // country. Case-insensitive — the fingerprint stores uppercase (e.g.
  // "SG") while the geo probe returns lowercase (e.g. "sg").
  const showProxyMismatch =
    detected &&
    fingerprint.country.toUpperCase() !== detected.country.toUpperCase();

  return (
    <div className="space-y-3">
      {/* Top toolbar: actions that operate on the WHOLE fingerprint live
          here so it's visually obvious they aren't tied to any single
          field below. Regen replaces every value with a new coherent
          set; Match proxy aligns locale/timezone with the proxy's geo. */}
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => void regen()}
          className="flex-1 inline-flex items-center justify-center gap-1.5 h-8 text-[12px] font-medium text-purple-200 hover:text-white transition-colors rounded-lg"
          style={{
            background:
              "linear-gradient(180deg, rgba(168,85,247,0.18), rgba(168,85,247,0.10))",
            boxShadow:
              "inset 0 0 0 1px rgba(168,85,247,0.35), 0 1px 0 rgba(255,255,255,0.05)",
          }}
          title="Generate a new coherent device + locale + screen + UA"
        >
          <RefreshCw size={12} strokeWidth={2.25} />
          Regenerate fingerprint
        </button>
        {proxy && proxy.host && (
          <button
            type="button"
            onClick={() => void detectFromProxy()}
            disabled={detecting}
            className="inline-flex items-center gap-1.5 h-8 px-3 text-[12px] font-medium text-slate-300 hover:text-slate-100 transition-colors rounded-lg disabled:opacity-50"
            style={{
              background: "rgba(255,255,255,0.03)",
              boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)",
            }}
            title="Probe the proxy and align locale/timezone with its geo"
          >
            <MapPin size={12} strokeWidth={2} />
            {detecting ? "Detecting…" : "Match proxy"}
          </button>
        )}
      </div>

      <Field label="Device">
        <Select
          value={fingerprint.device}
          onChange={(v) => void reconcile({ device: v as never })}
          options={devices.map((d) => ({ value: d.family, label: d.label }))}
        />
      </Field>

      <Field label="Locale">
        <Select
          value={currentLocale?.id ?? fingerprint.locale}
          onChange={(v) => void reconcile({ localeId: v })}
          options={(() => {
            const options = locales.map((l) => ({ value: l.id, label: l.label }));
            if (!currentLocale && fingerprint.locale) {
              options.unshift({ value: fingerprint.locale, label: `${fingerprint.locale} (custom)` });
            }
            return options;
          })()}
        />
      </Field>

      <Field label="Timezone">
        <Select
          value={fingerprint.timezone}
          onChange={(v) => void reconcile({ timezone: v })}
          options={(() => {
            // The proxy's actual timezone (from the geo probe) may not be
            // in the locale's cultural preset (e.g. en-GB presets only
            // Europe/London, but a SG proxy gives Asia/Singapore). Without
            // including the current value as an option, the <select> shows
            // the first preset instead of the real value — looking like
            // "Match proxy did nothing".
            const presets = currentLocale?.timezones ?? [];
            const opts = presets.map((tz) => ({ value: tz, label: tz }));
            if (
              fingerprint.timezone &&
              !presets.includes(fingerprint.timezone)
            ) {
              opts.unshift({
                value: fingerprint.timezone,
                label: `${fingerprint.timezone} (proxy)`,
              });
            }
            return opts;
          })()}
        />
      </Field>

      <Field label="Screen size">
        <Select
          value={`${fingerprint.screen.width}×${fingerprint.screen.height}`}
          onChange={(v) => {
            const m = v.match(/^(\d+)×(\d+)$/);
            if (!m) return;
            void reconcile({
              screen: { width: Number(m[1]), height: Number(m[2]) },
            });
          }}
          options={(() => {
            const screens = currentDevice?.screens ?? [];
            const options = screens.map((s) => ({
              value: `${s.width}×${s.height}`,
              label: s.label,
            }));
            const currentValue = `${fingerprint.screen.width}×${fingerprint.screen.height}`;
            if (!options.some((option) => option.value === currentValue)) {
              options.unshift({
                value: currentValue,
                label: `${fingerprint.screen.width} × ${fingerprint.screen.height} (custom)`,
              });
            }
            return options;
          })()}
        />
      </Field>

      {/* Computed-UA preview — read-only. This is the result of the
          choices above, not a user input. */}
      <div
        className="px-2.5 py-2 rounded-lg text-[10px] mono text-slate-500 leading-relaxed truncate"
        style={{
          background: "rgba(255,255,255,0.02)",
          boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.05)",
        }}
        title={fingerprint.userAgent}
      >
        <span className="text-slate-600">UA · </span>
        {shortUA(fingerprint.userAgent)}
      </div>

      {/* Direct editors for every persisted value that is not derived by the
          device/locale reconciliation controls above. Keeping these values in
          the same object is important: create and edit both round-trip the
          authoritative Rust FingerprintConfig without shadow state. */}
      <Field label="User-Agent">
        <input
          type="text"
          value={fingerprint.userAgent}
          onChange={(e) => onChange({ ...fingerprint, userAgent: e.target.value })}
          className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
          style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
        />
      </Field>
      <div className="grid grid-cols-2 gap-2.5">
        <Field label="Platform">
          <input
            type="text"
            value={fingerprint.platform}
            onChange={(e) => onChange({ ...fingerprint, platform: e.target.value })}
            className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
            style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
          />
        </Field>
        <Field label="Country">
          <input
            type="text"
            maxLength={2}
            value={fingerprint.country}
            onChange={(e) => onChange({ ...fingerprint, country: e.target.value.toUpperCase() })}
            className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
            style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
          />
        </Field>
      </div>
      <Field label="Languages" desc="Comma-separated navigator.languages values.">
        <input
          type="text"
          value={fingerprint.languages.join(", ")}
          onChange={(e) =>
            onChange({
              ...fingerprint,
              languages: e.target.value.split(",").map((s) => s.trim()).filter(Boolean),
            })
          }
          className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
          style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
        />
      </Field>
      <Field label="Accept-Language">
        <input
          type="text"
          value={fingerprint.acceptLanguage}
          onChange={(e) => onChange({ ...fingerprint, acceptLanguage: e.target.value })}
          className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
          style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
        />
      </Field>
      <div className="grid grid-cols-3 gap-2.5">
        <Field label="DPR">
          <input
            type="number"
            min={0}
            step="0.01"
            value={fingerprint.dpr}
            onChange={(e) => {
              const value = Number(e.target.value);
              if (Number.isFinite(value) && value > 0) onChange({ ...fingerprint, dpr: value });
            }}
            className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
            style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
          />
        </Field>
        <Field label="Hardware concurrency">
          <input
            type="number"
            min={1}
            step={1}
            value={fingerprint.hardwareConcurrency}
            onChange={(e) => {
              const value = Number(e.target.value);
              if (Number.isInteger(value) && value > 0) {
                onChange({ ...fingerprint, hardwareConcurrency: value });
              }
            }}
            className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
            style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
          />
        </Field>
        <Field label="Device memory (GB)">
          <input
            type="number"
            min={1}
            step={1}
            value={fingerprint.deviceMemory}
            onChange={(e) => {
              const value = Number(e.target.value);
              if (Number.isInteger(value) && value > 0) {
                onChange({ ...fingerprint, deviceMemory: value });
              }
            }}
            className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
            style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
          />
        </Field>
      </div>
      <Field label="Available screen size" desc="Clear both values to omit availScreen.">
        <div className="grid grid-cols-[1fr_1fr_auto] gap-2.5 items-end">
          <input
            type="number"
            min={1}
            value={fingerprint.availScreen?.width ?? ""}
            placeholder="width"
            onChange={(e) => {
              const width = Number(e.target.value);
              if (!Number.isInteger(width) || width <= 0) {
                if (e.target.value === "") onChange({ ...fingerprint, availScreen: null });
                return;
              }
              onChange({
                ...fingerprint,
                availScreen: { width, height: fingerprint.availScreen?.height ?? fingerprint.screen.height },
              });
            }}
            className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
            style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
          />
          <input
            type="number"
            min={1}
            value={fingerprint.availScreen?.height ?? ""}
            placeholder="height"
            onChange={(e) => {
              const height = Number(e.target.value);
              if (!Number.isInteger(height) || height <= 0) {
                if (e.target.value === "") onChange({ ...fingerprint, availScreen: null });
                return;
              }
              onChange({
                ...fingerprint,
                availScreen: { width: fingerprint.availScreen?.width ?? fingerprint.screen.width, height },
              });
            }}
            className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
            style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
          />
          {fingerprint.availScreen && (
            <button type="button" onClick={() => onChange({ ...fingerprint, availScreen: null })} className="h-9 px-2.5 text-[11px] text-slate-400 rounded-lg" style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}>Clear</button>
          )}
        </div>
      </Field>
      <Field label="WebGL vendor">
        <input
          type="text"
          value={fingerprint.webgl.vendor}
          onChange={(e) => onChange({ ...fingerprint, webgl: { ...fingerprint.webgl, vendor: e.target.value } })}
          className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
          style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
        />
      </Field>
      <Field label="WebGL renderer">
        <input
          type="text"
          value={fingerprint.webgl.renderer}
          onChange={(e) => onChange({ ...fingerprint, webgl: { ...fingerprint.webgl, renderer: e.target.value } })}
          className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
          style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
        />
      </Field>
      <Field label="Client hints" desc="All values are persisted verbatim and used by the selected browser engine where supported.">
        <div className="space-y-1.5">
          {(Object.keys(fingerprint.clientHints) as Array<keyof FingerprintConfig["clientHints"]>).map((key) => (
            <input
              key={key}
              type="text"
              aria-label={key}
              value={fingerprint.clientHints[key]}
              onChange={(e) => onChange({ ...fingerprint, clientHints: { ...fingerprint.clientHints, [key]: e.target.value } })}
              className="w-full px-2.5 h-8 rounded-lg bg-white/[0.03] text-[11px] text-slate-200 outline-none mono"
              style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
            />
          ))}
        </div>
      </Field>

      <Field label="Storage quota (MB)" desc="Pins navigator.storage.estimate(). Empty = engine default.">
        <input
          type="number"
          min={0}
          value={
            fingerprint.storageQuota == null
              ? ""
              : fingerprint.storageQuota / 1_000_000
          }
          onChange={(e) => {
            const value = e.target.value;
            if (value === "") {
              onChange({ ...fingerprint, storageQuota: null });
              return;
            }
            const mb = Number(value);
            if (Number.isFinite(mb) && mb >= 0) {
              onChange({
                ...fingerprint,
                storageQuota: mb > 0 ? Math.round(mb * 1_000_000) : null,
              });
            }
          }}
          placeholder="0 (default)"
          className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none"
          style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
        />
      </Field>
      <Field
        label="Font directory"
        desc="Folder CloakBrowser loads fonts from (--fingerprint-fonts-dir). Leave empty for the OS default (C:\\Windows\\Fonts on Windows)."
      >
        <div className="flex gap-1.5">
          <input
            type="text"
            value={fingerprint.fontsDir ?? ""}
            onChange={(e) =>
              onChange({
                ...fingerprint,
                fontsDir: e.target.value === "" ? null : e.target.value,
              })
            }
            placeholder="C:\Windows\Fonts (default)"
            className="flex-1 min-w-0 px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
            style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
          />
          <button
            type="button"
            onClick={async () => {
              const picked = await dialog.pickDirectory();
              if (picked) {
                onChange({ ...fingerprint, fontsDir: picked });
              }
            }}
            className="inline-flex items-center justify-center h-9 px-3 text-[11px] font-medium text-slate-300 hover:text-slate-100 transition-colors rounded-lg flex-shrink-0"
            style={{
              background: "rgba(255,255,255,0.03)",
              boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)",
            }}
            title="Browse for a font folder"
          >
            <FolderOpen size={13} strokeWidth={2} />
            <span className="ml-1.5">Browse…</span>
          </button>
          {fingerprint.fontsDir && (
            <button
              type="button"
              onClick={() => onChange({ ...fingerprint, fontsDir: undefined })}
              className="inline-flex items-center justify-center h-9 px-2.5 text-[11px] font-medium text-slate-400 hover:text-slate-200 transition-colors rounded-lg flex-shrink-0"
              style={{
                background: "rgba(255,255,255,0.03)",
                boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)",
              }}
              title="Reset to engine default"
            >
              Clear
            </button>
          )}
        </div>
      </Field>

      {/* Canvas noise seed — passed to CloakBrowser as --fingerprint=<seed>.
          Same seed → same canvas/audio/WebGL readback noise across launches;
          change the seed → rotate just the noise without touching device/UA.
          Empty = deterministic from the profile id (stable per profile). */}
      <Field
        label="Canvas noise seed"
        desc="Rotates CloakBrowser canvas/audio/WebGL noise. Same seed = same noise. Empty = stable per-profile default."
      >
        <div className="flex gap-1.5">
          <input
            type="text"
            value={fingerprint.seed ?? ""}
            onChange={(e) =>
              onChange({
                ...fingerprint,
                seed: e.target.value.trim() || undefined,
              })
            }
            placeholder="(profile default)"
            className="flex-1 min-w-0 px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none mono"
            style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" }}
          />
          <button
            type="button"
            onClick={() => {
              // Random 8-hex seed — same format the backend hashes to.
              const arr = new Uint8Array(4);
              crypto.getRandomValues(arr);
              const seed =
                Array.from(arr, (b) => b.toString(16).padStart(2, "0"))
                  .join("");
              onChange({ ...fingerprint, seed });
            }}
            className="inline-flex items-center justify-center h-9 px-3 text-[11px] font-medium text-slate-300 hover:text-slate-100 transition-colors rounded-lg flex-shrink-0"
            style={{
              background: "rgba(255,255,255,0.03)",
              boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)",
            }}
            title="Generate a random seed to rotate canvas/audio/WebGL noise"
          >
            <RefreshCw size={13} strokeWidth={2} />
            <span className="ml-1.5">Randomize</span>
          </button>
          {fingerprint.seed && (
            <button
              type="button"
              onClick={() =>
                onChange({ ...fingerprint, seed: undefined })
              }
              className="inline-flex items-center justify-center h-9 px-2.5 text-[11px] font-medium text-slate-400 hover:text-slate-200 transition-colors rounded-lg flex-shrink-0"
              style={{
                background: "rgba(255,255,255,0.03)",
                boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)",
              }}
              title="Reset to per-profile default"
            >
              Clear
            </button>
          )}
        </div>
      </Field>

      {/* Proxy-coherence warnings */}
      {detectError && (
        <div
          className="flex items-start gap-2 px-2.5 py-2 rounded-lg text-[11px] text-amber-300"
          style={{
            background: "rgba(245,158,11,0.06)",
            boxShadow: "inset 0 0 0 1px rgba(245,158,11,0.25)",
          }}
        >
          <AlertTriangle size={12} className="mt-0.5 flex-shrink-0" />
          <span>{detectError}</span>
        </div>
      )}
      {showProxyMismatch && (
        <div
          className="flex items-start gap-2 px-2.5 py-2 rounded-lg text-[11px] text-amber-300"
          style={{
            background: "rgba(245,158,11,0.06)",
            boxShadow: "inset 0 0 0 1px rgba(245,158,11,0.25)",
          }}
        >
          <AlertTriangle size={12} className="mt-0.5 flex-shrink-0" />
          <span>
            Locale country is{" "}
            <strong className="text-amber-200 uppercase">{fingerprint.country}</strong>{" "}
            but proxy IP is in{" "}
            <strong className="text-amber-200 uppercase">{detected.country}</strong>
            {" — anti-bot systems flag this. Pick a matching locale or proxy."}
          </span>
        </div>
      )}
      {detected && !showProxyMismatch && (
        <div
          className="flex items-start gap-2 px-2.5 py-2 rounded-lg text-[11px] text-emerald-300"
          style={{
            background: "rgba(16,185,129,0.06)",
            boxShadow: "inset 0 0 0 1px rgba(16,185,129,0.25)",
          }}
        >
          <MapPin size={12} className="mt-0.5 flex-shrink-0" />
          <span>
            Coherent: proxy resolves to{" "}
            <strong className="text-emerald-200">
              {detected.city ? `${detected.city}, ` : ""}
              {detected.countryName}
            </strong>{" "}
            ({detected.ip}) — matches locale.
          </span>
        </div>
      )}
    </div>
  );
}

function Field({
  label,
  desc,
  children,
  right,
}: {
  label: string;
  desc?: string;
  children: ReactNode;
  right?: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <div className="text-[11px] font-medium text-slate-500">{label}</div>
        {right && (
          <>
            <div className="flex-1" />
            {right}
          </>
        )}
      </div>
      {desc && (
        <div className="text-[10px] text-slate-600 leading-relaxed -mt-0.5">{desc}</div>
      )}
      {children}
    </div>
  );
}

function Select({
  value,
  onChange,
  options,
}: {
  value: string;
  onChange: (v: string) => void;
  options: ReadonlyArray<{ value: string; label: string }>;
}): JSX.Element {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="w-full px-2.5 h-9 rounded-lg bg-white/[0.03] text-[12px] text-slate-200 outline-none cursor-pointer focus:bg-white/[0.05] transition-colors"
      style={{
        boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)",
        fontFamily: "var(--font-mono)",
        fontWeight: 500,
        appearance: "none",
        backgroundImage:
          'url(\'data:image/svg+xml;utf8,<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="rgb(100,116,139)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>\')',
        backgroundRepeat: "no-repeat",
        backgroundPosition: "right 10px center",
        paddingRight: 30,
      }}
    >
      {options.map((o) => (
        <option
          key={o.value}
          value={o.value}
          style={{ background: "#12131a", color: "#e2e8f0" }}
        >
          {o.label}
        </option>
      ))}
    </select>
  );
}

function shortUA(ua?: string): string {
  if (!ua) return "auto";
  const m = ua.match(/Chrome\/(\d+)\..*\((.*?)\)/);
  if (!m) return ua.slice(0, 48);
  const platform = m[2]?.split(";")[0]?.trim() ?? "";
  return `Chrome ${m[1]} / ${platform}`;
}
