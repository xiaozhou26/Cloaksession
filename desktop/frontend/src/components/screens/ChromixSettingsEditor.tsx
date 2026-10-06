import { useState, type JSX } from "react";
import { Button } from "../atoms/Button";
import { SimpleFingerprint } from "../profile/SimpleFingerprint";
import { fingerprintOptionsProblem } from "../../lib/fingerprintMode";
import { ChromixFingerprintForm } from "../profile/ChromixFingerprintForm";
import type { ChromixSettings } from "../../types";

interface Props {
  value?: ChromixSettings;
  onSave: (value: ChromixSettings) => Promise<void>;
}

const DEFAULTS: ChromixSettings = { nodePath: "node", options: {}, environment: {} };
const controlClass = "w-full min-w-0 rounded-lg bg-white/[0.03] px-2.5 py-2 mono text-[12px] text-slate-200 outline-none focus:bg-white/[0.05] disabled:opacity-50";
const controlStyle = { boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" };

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
  const [seedValid, setSeedValid] = useState(true);
  const dirty = nodePath !== value.nodePath
    || options !== JSON.stringify(value.options, null, 2)
    || environment !== JSON.stringify(value.environment, null, 2);

  let structuredOptions: Record<string, unknown> | undefined;
  try {
    structuredOptions = parseObject(options, "Playwright options");
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
      parsedOptions = parseObject(options, "Playwright options");
      const problem = fingerprintOptionsProblem(parsedOptions);
      if (problem) nextErrors.options = problem;
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
        Playwright connects to your local Chromix executable. Select it under Browser binary; no browser download is performed.
      </div>
      <fieldset disabled={saving} className="space-y-4 min-w-0">
        {structuredOptions ? <SimpleFingerprint options={structuredOptions} onValidityChange={setSeedValid} onChange={(next) => {
          setOptions(JSON.stringify(next, null, 2));
          edited();
        }} /> : <p className="text-[12px] text-amber-200">Correct Playwright options JSON in Advanced. Your draft is preserved.</p>}
        <details className="mz-panel min-w-0 p-3">
          <summary className="cursor-pointer text-[12px] text-purple-300">Advanced: Playwright options, Node and environment</summary>
          <div className="mt-3 min-w-0 space-y-4">
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
              <summary className="cursor-pointer text-[12px] text-purple-300">All native fingerprint parameters</summary>
              <div className="mt-3">
                {structuredOptions ? (
                  <ChromixFingerprintForm options={structuredOptions} onChange={(next) => {
                    setOptions(JSON.stringify(next, null, 2));
                    edited();
                  }} />
                ) : <p className="text-[12px] text-amber-200">Correct Playwright options JSON to edit the structured fingerprint fields. Your draft is preserved.</p>}
              </div>
            </details>
            <div>
              <label htmlFor="chromix-options" className="block text-[12px] text-slate-300 mb-1.5">Playwright options JSON</label>
              <textarea
                id="chromix-options"
                value={options}
                onChange={(event) => { setOptions(event.target.value); edited(); }}
                rows={14}
                spellCheck={false}
                className={`${controlClass} resize-y`}
                style={controlStyle}
                aria-invalid={!!errors.options}
                aria-describedby="chromix-options-help"
              />
              <p id="chromix-options-help" className="text-[11px] text-slate-500 mt-1.5 leading-relaxed">
                Raw Playwright options and legacy keys are preserved without a UI allowlist. Unsupported
                legacy keys may be rejected at launch. JSON cannot store functions or callbacks.
              </p>

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
          <p className="text-[11px] text-slate-500">Cloaksession reserves debugging arguments and profile storage. Do not override remote-debugging flags.</p>
          <a href="https://playwright.dev/docs/api/class-browsertype#browser-type-launch-persistent-context" target="_blank" rel="noopener noreferrer" className="text-[12px] text-purple-300">Playwright launch options ↗</a>
          </div>
        </details>
      </fieldset>
      <div className="flex flex-wrap items-center gap-2.5">
        <Button variant="primary" onClick={() => void save()} disabled={saving || !seedValid}>
          {saving ? "Saving…" : "Save Chromix settings"}
        </Button>
        <span role="status" className="text-[12px] text-slate-400">
          {saved ? "Saved. Restart the app to apply." : dirty ? "Unsaved changes — save before leaving this page." : "No unsaved changes."}
        </span>
      </div>
      {errors.options && <ErrorMessage message={errors.options} />}
      {errors.save && <ErrorMessage message={errors.save} />}
    </div>
  );
}

function ErrorMessage({ id, message }: { id?: string; message: string }): JSX.Element {
  return <p id={id} role="alert" className="text-[12px] text-red-300 mt-2 break-words">{message}</p>;
}
