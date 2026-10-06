import { useEffect, useState, type JSX } from "react";
import { settings } from "../../lib/ipc";
import { fingerprintOptionsProblem } from "../../lib/fingerprintMode";
import { SimpleFingerprint } from "./SimpleFingerprint";
import { ChromixFingerprintForm } from "./ChromixFingerprintForm";

interface Props {
  options: Record<string, unknown>;
  onChange: (options: Record<string, unknown>) => void;
}

export function ChromixProfileOptions({ options, onChange }: Props): JSX.Element {
  const [json, setJson] = useState(() => JSON.stringify(options, null, 2));
  const [inherited, setInherited] = useState<Record<string, unknown>>({});
  useEffect(() => { void settings.get().then((value) => setInherited(value.chromix.options)).catch(() => {}); }, []);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setJson(JSON.stringify(options, null, 2));
    setError(null);
  }, [options]);

  function applyJson(): void {
    try {
      const parsed: unknown = JSON.parse(json);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
        throw new Error("Playwright options must be a JSON object.");
      }
      const problem = fingerprintOptionsProblem({ ...inherited, ...parsed as Record<string, unknown> });
      if (problem) throw new Error(problem);
      onChange(parsed as Record<string, unknown>);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <div className="min-w-0 space-y-4">
      <div className="text-[12px] text-slate-400 leading-relaxed">
        These settings apply when Chromix is selected in Settings → Browser engine.
        Profiles inherit global Playwright options until you override them. Arrays and nested
        objects replace the matching global value. Other engines use the original Fingerprint section.
      </div>
      <SimpleFingerprint options={options} onChange={onChange} inherited={inherited} profile />
      <details className="rounded-lg border border-white/10 p-3">
        <summary className="cursor-pointer text-[12px] text-slate-300">Advanced: native flags and profile Playwright JSON</summary>
        <p className="my-2 text-[11px] text-slate-500">
          Raw Playwright options and legacy keys are retained. Unsupported legacy options may be
          rejected at launch. Apply JSON to save it; opening Advanced never changes your profile.
        </p>
        <details className="my-3">
          <summary className="cursor-pointer text-[12px] text-purple-300">All native fingerprint parameters</summary>
          <ChromixFingerprintForm options={options} onChange={onChange} />
        </details>
        <textarea
          aria-label="Profile Playwright options"
          className="h-40 w-full min-w-0 resize-y rounded-lg bg-white/5 p-2 font-mono text-[12px] text-slate-200 sm:h-56"
          rows={12}
          spellCheck={false}
          value={json}
          onChange={(event) => setJson(event.target.value)}
        />
        {error && <p role="alert" className="mt-2 text-[12px] text-red-400">{error}</p>}
        <button type="button" className="btn-secondary mt-2 rounded-lg px-3 py-2 text-[12px]" onClick={applyJson}>
          Apply profile JSON
        </button>
      </details>
    </div>
  );
}
