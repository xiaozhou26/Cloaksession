import { useEffect, useState, type JSX, type ReactNode } from "react";
import { Kbd } from "../atoms";

interface ConfirmProps {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel?: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export function Confirm(props: ConfirmProps): JSX.Element | null {
  useEffect(() => {
    if (!props.open) return;
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === "Escape") props.onCancel();
      if (e.key === "Enter") props.onConfirm();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [props]);

  if (!props.open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: "rgba(5,6,10,0.55)", backdropFilter: "blur(8px)", animation: "mz-fade-in 120ms ease-out" }}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) props.onCancel();
      }}
    >
      <div
        style={{
          width: 420,
          borderRadius: 16,
          background: "rgba(15,16,22,0.92)",
          boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.10), 0 30px 80px rgba(0,0,0,0.6)",
          padding: 22,
          animation: "mz-slide-up 140ms cubic-bezier(0.2,0.8,0.2,1)",
        }}
      >
        <div className="text-[15px] font-bold text-slate-100">{props.title}</div>
        <div className="text-[12.5px] text-slate-400 mt-2 leading-relaxed">{props.description}</div>
        <div className="flex items-center gap-2 mt-5 justify-end">
          <button
            type="button"
            className="btn-ghost px-3 py-[7px] text-[12px] rounded-[9px]"
            onClick={props.onCancel}
          >
            Cancel
            <Kbd>esc</Kbd>
          </button>
          <button
            type="button"
            className={props.destructive ? "px-3 py-[7px] text-[12px] rounded-[9px] cursor-pointer text-red-300" : "btn-brand px-3 py-[7px] text-[12px] rounded-[9px]"}
            style={
              props.destructive
                ? {
                    background: "rgba(239,68,68,0.10)",
                    boxShadow: "inset 0 0 0 1px rgba(239,68,68,0.4)",
                    border: 0,
                  }
                : undefined
            }
            onClick={props.onConfirm}
          >
            {props.confirmLabel ?? "Confirm"}
            <Kbd variant={props.destructive ? "default" : "on-brand"}>⏎</Kbd>
          </button>
        </div>
      </div>
    </div>
  );
}

interface PromptProps {
  open: boolean;
  title: string;
  description?: ReactNode;
  label?: string;
  placeholder?: string;
  inputType?: "text" | "password";
  confirmLabel?: string;
  onSubmit: (value: string) => void | Promise<void>;
  onCancel: () => void;
}

export function Prompt(props: PromptProps): JSX.Element | null {
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (props.open) setValue("");
  }, [props.open]);

  if (!props.open) return null;

  async function submit(): Promise<void> {
    setBusy(true);
    try {
      await props.onSubmit(value);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: "rgba(5,6,10,0.55)", backdropFilter: "blur(8px)", animation: "mz-fade-in 120ms ease-out" }}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) props.onCancel();
      }}
    >
      <div
        style={{
          width: 460,
          borderRadius: 16,
          background: "rgba(15,16,22,0.92)",
          boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.10), 0 30px 80px rgba(0,0,0,0.6)",
          padding: 22,
          animation: "mz-slide-up 140ms cubic-bezier(0.2,0.8,0.2,1)",
        }}
      >
        <div className="text-[15px] font-bold text-slate-100">{props.title}</div>
        {props.description && (
          <div className="text-[12.5px] text-slate-400 mt-2 leading-relaxed">{props.description}</div>
        )}
        {props.label && <div className="text-[11px] font-medium text-slate-500 mt-4 mb-1.5">{props.label}</div>}
        <input
          autoFocus
          type={props.inputType ?? "text"}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder={props.placeholder}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !busy) void submit();
            if (e.key === "Escape") props.onCancel();
          }}
          className="w-full mono text-[13px] text-slate-100 px-3 py-2.5 rounded-[10px] outline-none"
          style={{
            background: "rgba(255,255,255,0.04)",
            boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.08)",
            border: 0,
            marginTop: props.label ? 0 : 16,
          }}
        />
        <div className="flex items-center gap-2 mt-5 justify-end">
          <button type="button" className="btn-ghost px-3 py-[7px] text-[12px] rounded-[9px]" onClick={props.onCancel}>
            Cancel
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={submit}
            className="btn-brand px-3 py-[7px] text-[12px] rounded-[9px]"
          >
            {busy ? "…" : (props.confirmLabel ?? "Submit")}
          </button>
        </div>
      </div>
    </div>
  );
}
