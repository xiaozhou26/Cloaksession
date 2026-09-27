import { useState, type JSX } from "react";
import { Button } from "../atoms/Button";
import { ChromixFingerprintForm } from "../profile/ChromixFingerprintForm";
import type { ChromixSettings } from "../../types";

interface Props {
  value?: ChromixSettings;
  onSave: (value: ChromixSettings) => Promise<void>;
}

const DEFAULTS: ChromixSettings = { nodePath: "node", options: {}, environment: {} };
const SDK_README = "https://github.com/xiaozhou26/Chromix/blob/main/sdk/node/README.md";
const controlClass = "w-full min-w-0 rounded-lg bg-white/[0.03] px-2.5 py-2 mono text-[12px] text-slate-200 outline-none focus:bg-white/[0.05] disabled:opacity-50";
const controlStyle = { boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" };

const optionsExample = {
  headless: false,
  proxy: { server: "http://proxy.example:8080", username: "user", password: "replace-me" },
  args: ["--fingerprint=42"],
  stealthArgs: true,
  timezone: "America/New_York",
  locale: "en-US",
  geoip: false,
  humanize: true,
  humanPreset: "default",
  humanConfig: { typingDelay: 70, typingSpread: 40, seed: 42 },
  userAgent: "Custom user agent — replace for your browser",
  viewport: { width: 1440, height: 900 },
  colorScheme: "dark",
  extensionPaths: ["/absolute/path/to/unpacked-extension"],
  browserVersion: "stable",
  releaseChannel: "stable",
  licenseKey: "",
  contextOptions: { acceptDownloads: true, permissions: ["geolocation"] },
  launchOptions: { timeout: 60000, slowMo: 0 },
  userDataDir: "/absolute/path/to/profile",
  startMaximized: false,
  fontsDir: "/absolute/path/to/fonts",
};

const environmentExample = {
  CLOAKBROWSER_BINARY_PATH: "/absolute/path/to/chromix/executable",
  CLOAKBROWSER_VERSION: "stable",
  CLOAKBROWSER_RELEASE_CHANNEL: "stable",
  CLOAKBROWSER_GEOIP_TIMEOUT_SECONDS: "10",
  CLOAKBROWSER_WIDEVINE_CDM: "/absolute/path/to/widevine",
  CLOAKBROWSER_WIDEVINE: "0",
  CHROMIX_CACHE_DIR: "/absolute/path/to/cache",
  CHROMIX_DOWNLOAD_HOST: "https://github.com/xiaozhou26/Chromix/releases/download",
};

function parseObject(text: string, label: string): Record<string, unknown> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (error) {
    throw new Error(`${label}: invalid JSON. ${error instanceof Error ? error.message : String(error)}`);
  }
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error(`${label} must be a top-level JSON object, not an array, null, or primitive.`);
  }
  return parsed as Record<string, unknown>;
}

export function ChromixSettingsEditor({ value = DEFAULTS, onSave }: Props): JSX.Element {
  const [nodePath, setNodePath] = useState(value.nodePath);
  const [options, setOptions] = useState(() => JSON.stringify(value.options, null, 2));
  const [environment, setEnvironment] = useState(() => JSON.stringify(value.environment, null, 2));
  const [errors, setErrors] = useState<{ nodePath?: string; options?: string; environment?: string; save?: string }>({});
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const dirty = nodePath !== value.nodePath
    || options !== JSON.stringify(value.options, null, 2)
    || environment !== JSON.stringify(value.environment, null, 2);

  let structuredOptions: Record<string, unknown> | undefined;
  try {
    structuredOptions = parseObject(options, "SDK options");
  } catch {
    // Keep invalid JSON drafts intact until the user corrects them.
  }

  function edited(): void {
    setErrors({});
    setSaved(false);
  }

  async function save(): Promise<void> {
    const nextErrors: typeof errors = {};
    let parsedOptions: Record<string, unknown> = {};
    let parsedEnvironment: Record<string, unknown> = {};
    if (!nodePath.trim()) nextErrors.nodePath = "Enter a Node.js executable path or node.";
    try {
      parsedOptions = parseObject(options, "SDK options");
    } catch (error) {
      nextErrors.options = (error as Error).message;
    }
    try {
      parsedEnvironment = parseObject(environment, "Environment");
      if (Object.values(parsedEnvironment).some((item) => typeof item !== "string")) {
        nextErrors.environment = "Environment values must all be strings (including numbers and booleans, written in quotes).";
      }
    } catch (error) {
      nextErrors.environment = (error as Error).message;
    }
    setErrors(nextErrors);
    setSaved(false);
    if (Object.keys(nextErrors).length) return;

    setSaving(true);
    try {
      await onSave({
        nodePath: nodePath.trim(),
        options: parsedOptions,
        environment: parsedEnvironment as Record<string, string>,
      });
      setNodePath(nodePath.trim());
      setOptions(JSON.stringify(parsedOptions, null, 2));
      setEnvironment(JSON.stringify(parsedEnvironment, null, 2));
      setSaved(true);
    } catch (error) {
      setErrors({ save: `Could not save Chromix settings: ${error instanceof Error ? error.message : String(error)}` });
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-4 min-w-0" aria-label="Chromix configuration">
      <div className="text-[12px] text-slate-400 leading-relaxed">
        Global settings for all Chromix profiles. Save explicitly, then restart the app to apply
        changes to browser startup. Switching engines does not erase this configuration.
      </div>
      <fieldset disabled={saving} className="space-y-4 min-w-0">
        <div>
          <label htmlFor="chromix-node-path" className="block text-[12px] text-slate-300 mb-1.5">Node.js executable</label>
          <input
            id="chromix-node-path"
            type="text"
            value={nodePath}
            onChange={(event) => { setNodePath(event.target.value); edited(); }}
            placeholder="node"
            spellCheck={false}
            className={controlClass}
            style={controlStyle}
            aria-invalid={!!errors.nodePath}
            aria-describedby={errors.nodePath ? "chromix-node-error" : "chromix-node-help"}
          />
          <p id="chromix-node-help" className="text-[11px] text-slate-500 mt-1.5">
            Default: node (from PATH). Use an absolute executable path if the desktop app cannot find Node.js.
          </p>
          {errors.nodePath && <ErrorMessage id="chromix-node-error" message={errors.nodePath} />}
        </div>
        <details className="mz-panel min-w-0 p-3">
          <summary className="cursor-pointer text-[12px] text-purple-300">Global fingerprint parameters</summary>
          <div className="mt-3">
            {structuredOptions ? (
              <ChromixFingerprintForm options={structuredOptions} onChange={(next) => {
                setOptions(JSON.stringify(next, null, 2));
                edited();
              }} />
            ) : <p className="text-[12px] text-amber-200">Correct SDK options JSON to edit the structured fingerprint fields. Your draft is preserved.</p>}
          </div>
        </details>
        <div>
          <label htmlFor="chromix-options" className="block text-[12px] text-slate-300 mb-1.5">SDK options JSON</label>
          <textarea
            id="chromix-options"
            value={options}
            onChange={(event) => { setOptions(event.target.value); edited(); }}
            rows={14}
            spellCheck={false}
            className={`${controlClass} resize-y`}
            style={controlStyle}
            aria-invalid={!!errors.options}
            aria-describedby={errors.options ? "chromix-options-error" : "chromix-options-help"}
          />
          <p id="chromix-options-help" className="text-[11px] text-slate-500 mt-1.5 leading-relaxed">
            A complete SDK options object, not an AppSettings wrapper. All official SDK fields and
            unknown keys are preserved and passed through without a UI allowlist. Defaults to {"{}"}.
            The installed SDK validates supported options at launch. JSON cannot store functions or callbacks.
          </p>
          {errors.options && <ErrorMessage id="chromix-options-error" message={errors.options} />}
        </div>
        <div>
          <label htmlFor="chromix-environment" className="block text-[12px] text-slate-300 mb-1.5">Environment JSON</label>
          <textarea
            id="chromix-environment"
            value={environment}
            onChange={(event) => { setEnvironment(event.target.value); edited(); }}
            rows={6}
            spellCheck={false}
            className={`${controlClass} resize-y`}
            style={controlStyle}
            aria-invalid={!!errors.environment}
            aria-describedby={errors.environment ? "chromix-environment-error" : "chromix-environment-help"}
          />
          <p id="chromix-environment-help" className="text-[11px] text-slate-500 mt-1.5 leading-relaxed">
            Environment overrides for the Chromix Node process. A JSON object with string values only;
            defaults to {"{}"}. Stored locally in settings; do not share secrets in screenshots or exports.
          </p>
          {errors.environment && <ErrorMessage id="chromix-environment-error" message={errors.environment} />}
        </div>
      </fieldset>
      <div className="flex flex-wrap items-center gap-2.5">
        <Button variant="primary" onClick={() => void save()} disabled={saving}>
          {saving ? "Saving…" : "Save Chromix settings"}
        </Button>
        <span role="status" className="text-[12px] text-slate-400">
          {saved ? "Saved. Restart the app to apply." : dirty ? "Unsaved changes — save before leaving this page." : "No unsaved changes."}
        </span>
      </div>
      {errors.save && <ErrorMessage message={errors.save} />}
      <div className="mz-panel p-3 text-[11px] text-slate-400 leading-relaxed space-y-2 break-words">
        <p>
          <b className="text-slate-300">Automation boundary:</b> SDK humanize only affects SDK Playwright
          page objects. Existing MCP/CDP actions still use Cloaksession’s current driver, not the SDK’s
          humanized mouse or keyboard methods.
        </p>
        <p>
          <b className="text-slate-300">Measured devices:</b> devicePool requires the matching Python SDK
          and matching browser binary through a separate measured-device entry point. Cloaksession's
          CDP sidecar rejects devicePool and measured mode because that entry point excludes host CDP
          overrides. The example below is for direct SDK use only.
        </p>
        <p>
          <b className="text-slate-300">Host controls:</b> Cloaksession supplies profile storage by default and reserves
          debugging arguments, including --remote-debugging-port, --remote-debugging-address and
          --remote-debugging-pipe. Do not override them in args, launchOptions or contextOptions.
        </p>
        <a href={SDK_README} target="_blank" rel="noopener noreferrer" className="inline-block text-purple-400 hover:text-purple-300">
          Official Chromix Node SDK README ↗
        </a>
      </div>
      <details className="mz-panel p-3 text-[12px] min-w-0">
        <summary className="cursor-pointer text-purple-300">Complete SDK JSON examples</summary>
        <p className="text-[11px] text-slate-500 mt-3 leading-relaxed">
          Reference examples, not defaults or a field whitelist. Replace paths, credentials and persona
          values before use. Omit userDataDir to keep Cloaksession’s per-profile storage; an explicit
          path overrides it and must not be shared by concurrent profiles. Availability depends on the
          installed SDK and matching native binary; consult the official README above.
        </p>
        <Example title="SDK options example" value={optionsExample} />
        <Example title="Environment example" value={environmentExample} />
        <Example title="devicePool options example (separate measured-device mode)" value={{
          devicePool: { python: "python", host: "/absolute/path/to/host-record.json", records: ["/absolute/path/to/record.json"], seed: "42" },
        }} />
      </details>
    </div>
  );
}

function ErrorMessage({ id, message }: { id?: string; message: string }): JSX.Element {
  return <p id={id} role="alert" className="text-[12px] text-red-300 mt-2 break-words">{message}</p>;
}

function Example({ title, value }: { title: string; value: Record<string, unknown> }): JSX.Element {
  return (
    <div className="mt-3 min-w-0">
      <div className="text-[11px] text-slate-400 mb-1.5">{title}</div>
      <pre className="mono text-[11px] text-slate-300 p-2.5 rounded-lg bg-black/20 whitespace-pre-wrap break-all">
        {JSON.stringify(value, null, 2)}
      </pre>
    </div>
  );
}
