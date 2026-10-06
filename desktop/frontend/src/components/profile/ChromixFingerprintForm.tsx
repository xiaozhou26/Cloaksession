import { useId, useState, type JSX } from "react";
import {
  CHROMIX_FINGERPRINT_FIELDS,
  CHROMIX_FINGERPRINT_GROUPS,
  CHROMIX_FINGERPRINT_SOURCE,
  chromixArgsProblem,
  chromixFingerprintNotices,
  parseChromixRawArgs,
  readChromixField,
  readChromixFlag,
  setChromixField,
  validateChromixValue,
  type ChromixFingerprintField,
  type ChromixFlagValue,
} from "../../lib/chromixFingerprint";

export interface ChromixFingerprintFormProps {
  options: Record<string, unknown>;
  onChange: (options: Record<string, unknown>) => void;
}

type Props = ChromixFingerprintFormProps;

const controlClass = "w-full min-w-0 rounded-lg bg-white/[0.03] px-2.5 py-2 mono text-[12px] text-slate-200 outline-none focus:bg-white/[0.05] disabled:opacity-50";
const controlStyle = { boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)" };
const optionStyle = { background: "#12131a", color: "#e2e8f0" };
const smallButtonClass = "rounded px-2 py-1 text-[11px] text-purple-300 hover:bg-white/[0.05] disabled:opacity-40";

export function ChromixFingerprintForm({ options, onChange }: Props): JSX.Element {
  const id = useId();
  const [query, setQuery] = useState("");
  const notices = chromixFingerprintNotices(options);
  const invalidArgs = !!chromixArgsProblem(options);
  const search = query.trim().toLowerCase();
  const fields = CHROMIX_FINGERPRINT_FIELDS.filter((field) => [field.label, field.flag, field.sdkKey, field.description, ...(field.aliases ?? [])].join(" ").toLowerCase().includes(search));
  const source = `${CHROMIX_FINGERPRINT_SOURCE.repository}/blob/${CHROMIX_FINGERPRINT_SOURCE.commit}`;

  return (
    <section className="min-w-0 space-y-3" aria-label="Chromix fingerprint parameters">
      <div className="space-y-1.5 text-[11px] leading-relaxed text-slate-400">
        <h3 className="text-[13px] font-medium text-slate-200">Chromix fingerprint parameters</h3>
        <p>
          Advanced native flags. Unset fields do not write overrides. Unknown arguments and legacy
          properties are preserved. Support depends on your local Chromix binary, not Playwright.
        </p>
        <p>
          Simple mode and seed take priority over legacy seed flags. Other raw aliases keep native
          precedence. Editing one advanced parameter does not clear other aliases.
        </p>
        <div className="flex flex-wrap gap-x-3 gap-y-1 text-purple-300">
          <a href={`${source}/${CHROMIX_FINGERPRINT_SOURCE.flags}`} target="_blank" rel="noopener noreferrer">Public flags ↗</a>
          <a href={`${source}/${CHROMIX_FINGERPRINT_SOURCE.backend}`} target="_blank" rel="noopener noreferrer">Backend policy ↗</a>
          <span className="mono text-slate-500">{CHROMIX_FINGERPRINT_SOURCE.commit.slice(0, 12)}</span>
        </div>
      </div>
      <div>
        <label htmlFor={`${id}-search`} className="mb-1 block text-[11px] text-slate-400">Find a parameter</label>
        <input id={`${id}-search`} type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Name, --flag, option key or description" className={controlClass} style={controlStyle} />
      </div>
      {notices.length > 0 && (
        <details className="rounded-lg border border-amber-400/20 bg-amber-500/[0.04] p-3" open>
          <summary className="cursor-pointer text-[12px] text-amber-200">Configuration notes ({notices.length})</summary>
          <ul className="mt-2 list-disc space-y-1.5 pl-4 text-[11px] leading-relaxed text-amber-200/80" aria-live="polite">
            {notices.map((notice) => <li key={notice}>{notice}</li>)}
          </ul>
        </details>
      )}
      {CHROMIX_FINGERPRINT_GROUPS.map((group) => {
        const groupFields = fields.filter((field) => field.group === group.id);
        if (!groupFields.length) return null;
        const configured = groupFields.filter((field) => readChromixField(options, field).present).length;
        return (
          <details key={`${group.id}-${search ? "search" : "browse"}`} className="mz-panel min-w-0 p-3" open={!!search || group.id === "identity" || configured > 0}>
            <summary className="cursor-pointer text-[12px] font-medium text-slate-200">
              {group.label}<span className="ml-2 text-[10px] font-normal text-slate-500">{configured} set · {groupFields.length} fields</span>
            </summary>
            <p className="mt-2 text-[11px] leading-relaxed text-slate-500">{group.description}</p>
            <div className="mt-3 grid min-w-0 grid-cols-1 gap-4 sm:grid-cols-2">
              {groupFields.map((field) => <FingerprintField key={field.id} field={field} options={options} onChange={onChange} disabled={invalidArgs && !field.sdkKey} />)}
            </div>
          </details>
        );
      })}
      {fields.length === 0 && <p className="text-[12px] text-slate-500">No matching parameters.</p>}
      <RawArgsEditor options={options} onChange={onChange} />
      <p className="text-[10px] leading-relaxed text-slate-500">
        Additional Playwright options (including viewport, contextOptions and launchOptions) remain available in Advanced JSON.
        Warnings do not normalize or delete stored values. Unsupported legacy options may be rejected at launch.
      </p>
    </section>
  );
}

function FingerprintField({ field, options, onChange, disabled }: Props & { field: ChromixFingerprintField; disabled: boolean }): JSX.Element {
  const id = useId();
  const [selectedName, setSelectedName] = useState<string>();
  const effective = readChromixField(options, field);
  const state = selectedName ? readChromixFlag(options, selectedName) : effective;
  const error = validateChromixValue(field, state.value);
  const setValue = (value: ChromixFlagValue) => onChange(setChromixField(options, field, value, selectedName));
  const selectField = ["enum", "boolean", "presence", "feature"].includes(field.kind);
  const choices = field.kind === "boolean" || field.kind === "feature" ? ["true", "false"] : field.choices ?? [];
  const encodedValue = state.value === undefined ? "unset" : state.value === null ? "bare" : JSON.stringify(state.value);
  const customValue = state.value !== undefined && state.value !== null && !choices.includes(state.value);
  const rawTarget = state.name.startsWith("--uxr-");

  return (
    <div className="min-w-0 space-y-1.5">
      <div className="flex items-start justify-between gap-2">
        <label htmlFor={`${id}-value`} className="text-[12px] text-slate-300">{field.label}{field.unit && <span className="ml-1 text-slate-500">({field.unit})</span>}</label>
        {state.present && <button type="button" disabled={disabled} className={smallButtonClass} onClick={() => setValue(undefined)} title="Remove only the selected parameter">Unset</button>}
      </div>
      <div className="break-all mono text-[10px] text-slate-500">{field.sdkKey ? `options.${field.sdkKey}` : field.kind === "feature" ? "--enable-blink-features=FakeShadowRoot" : state.name}</div>
      {!!field.aliases?.length && (
        <select
          aria-label={`${field.label} parameter to edit`}
          value={selectedName ?? effective.name}
          onChange={(event) => setSelectedName(event.target.value)}
          disabled={disabled}
          className={`${controlClass} !py-1 !text-[10px]`}
          style={controlStyle}
        >
          {[field.flag!, ...field.aliases].map((name) => <option key={name} value={name} style={optionStyle}>{name}{name === field.flag ? " · public" : " · raw / higher priority"}</option>)}
        </select>
      )}
      {selectField ? (
        <select
          id={`${id}-value`}
          value={encodedValue}
          onChange={(event) => setValue(event.target.value === "unset" ? undefined : event.target.value === "bare" ? null : JSON.parse(event.target.value) as string)}
          disabled={disabled}
          aria-describedby={`${id}-help${error ? ` ${id}-error` : ""}`}
          aria-invalid={!!error}
          className={controlClass}
          style={controlStyle}
        >
          <option value="unset" style={optionStyle}>Not set (browser default)</option>
          {(field.kind === "presence" || state.value === null) && <option value="bare" style={optionStyle}>{field.sdkKey ? "null (stored)" : "Bare flag (present, no value)"}</option>}
          {choices.map((choice) => <option key={choice} value={JSON.stringify(choice)} style={optionStyle}>{choice === "" ? "Explicit empty (=) — disabled" : choice}</option>)}
          {customValue && <option value={encodedValue} style={optionStyle}>{state.value || "Explicit empty (=)"} (stored, preserved)</option>}
        </select>
      ) : (
        <>
          <input
            id={`${id}-value`}
            type="text"
            inputMode={field.integer ? "numeric" : field.decimal ? "decimal" : undefined}
            value={state.value ?? ""}
            onChange={(event) => setValue(event.target.value === "" ? undefined : event.target.value)}
            disabled={disabled}
            spellCheck={false}
            placeholder={state.present ? state.value === null ? "Bare flag (no value)" : "Explicit empty value (=)" : "Not set"}
            aria-invalid={!!error}
            aria-describedby={`${id}-help${error ? ` ${id}-error` : ""}`}
            className={controlClass}
            style={controlStyle}
          />
          {field.kind === "seed" && (
            <div className="flex flex-wrap gap-1">
              <button type="button" disabled={disabled} className={smallButtonClass} onClick={() => setValue(null)}>Bare flag / generate seed</button>
              <button type="button" disabled={disabled} className={smallButtonClass} onClick={() => setValue("off")}>Set off</button>
            </div>
          )}
          {state.present && (state.value === "" || state.value === null) && <p className="text-[10px] text-slate-400">Stored explicitly: {state.value === null ? "bare flag" : "empty string"}. Use Unset to remove.</p>}
        </>
      )}
      <p id={`${id}-help`} className="text-[10px] leading-relaxed text-slate-500">{field.description}</p>
      {rawTarget && <p className="text-[10px] text-purple-300/80">Editing the raw alias. Public aliases are preserved and can be selected separately.</p>}
      {state.count > 1 && <p className="text-[10px] text-amber-200/80">{state.count} occurrences. The last stored value is shown; editing replaces every occurrence of this exact name with one argument.</p>}
      {error && <p id={`${id}-error`} className="text-[10px] leading-relaxed text-amber-200" role="status">{error}</p>}
    </div>
  );
}

function RawArgsEditor({ options, onChange }: Props): JSX.Element {
  const id = useId();
  const [draft, setDraft] = useState<{ base: string; text: string }>();
  const [error, setError] = useState<string>();
  const args = Array.isArray(options.args) && options.args.every((arg) => typeof arg === "string") ? options.args as string[] : [];
  const serialized = JSON.stringify(options.args);
  const base = serialized ?? "undefined";
  const text = draft?.text ?? args.join("\n");
  const stale = draft !== undefined && draft.base !== base;
  const problem = chromixArgsProblem(options) ?? (args.some((arg) => /[\r\n]/.test(arg)) ? "An argument contains a line break. Use Advanced JSON to preserve that value exactly." : undefined);

  function apply(): void {
    if (stale || problem) return;
    try {
      const next = parseChromixRawArgs(text);
      onChange({ ...options, args: next });
      setDraft(undefined);
      setError(undefined);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  }

  return (
    <details className="mz-panel min-w-0 p-3">
      <summary className="cursor-pointer text-[12px] text-purple-300">Raw options.args — one flag per line</summary>
      <p id={`${id}-help`} className="my-2 text-[11px] leading-relaxed text-slate-500">
        Complete options.args array, including unknown flags and duplicates. Use --key=value (spaces inside the value are literal);
        no shell splitting or automatic alias cleanup. Apply updates options only; the outer Save still persists it.
      </p>
      <label htmlFor={`${id}-raw`} className="mb-1 block text-[11px] text-slate-400">Browser arguments</label>
      <textarea id={`${id}-raw`} rows={8} value={text} disabled={!!problem} onChange={(event) => { setDraft({ base: draft?.base ?? base, text: event.target.value }); setError(undefined); }} spellCheck={false} className={`${controlClass} resize-y`} style={controlStyle} aria-describedby={`${id}-help`} aria-invalid={!!error} />
      <div className="mt-2 flex flex-wrap gap-2">
        <button type="button" className={smallButtonClass} disabled={!!problem || stale || !draft} onClick={apply}>Apply raw args</button>
        {draft && <button type="button" className={smallButtonClass} onClick={() => { setDraft(undefined); setError(undefined); }}>Discard raw draft / reload current args</button>}
      </div>
      {(error || problem || stale) && <p className="mt-2 text-[11px] text-amber-200" role="alert">{error ?? problem ?? "options.args changed while this raw draft was open. Discard the draft before editing again; newer structured edits will not be overwritten."}</p>}
    </details>
  );
}
