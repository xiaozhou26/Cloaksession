import type { JSX, ReactNode } from "react";

export type PillKind = "running" | "pending" | "error" | "ai" | "idle" | "info";

interface Props {
  kind?: PillKind;
  children: ReactNode;
  glow?: boolean;
  dot?: boolean;
  className?: string;
}

const PALETTE: Record<PillKind, [string, string, string, boolean]> = {
  // [bg, fg, glow, animatedDot]
  running: ["rgba(16,185,129,0.10)", "#34d399", "rgba(16,185,129,0.4)", true],
  pending: ["rgba(245,158,11,0.10)", "#fbbf24", "rgba(245,158,11,0.4)", false],
  error: ["rgba(239,68,68,0.10)", "#f87171", "rgba(239,68,68,0.4)", false],
  ai: ["rgba(168,85,247,0.10)", "#c084fc", "rgba(168,85,247,0.4)", true],
  idle: ["rgba(100,116,139,0.10)", "#94a3b8", "rgba(100,116,139,0.3)", false],
  info: ["rgba(99,102,241,0.10)", "#a5b4fc", "rgba(99,102,241,0.3)", false],
};

export function Pill({ kind = "idle", children, glow, dot, className }: Props): JSX.Element {
  const [bg, fg, glowColor, animated] = PALETTE[kind];
  return (
    <span
      className={`mz-pill ${className ?? ""}`}
      style={{
        background: bg,
        color: fg,
        boxShadow: glow ? `0 0 24px ${glowColor}` : undefined,
      }}
    >
      {dot && (
        <span
          style={{
            width: 5,
            height: 5,
            borderRadius: "50%",
            background: "currentColor",
            animation: animated ? "mz-dotpulse 1.6s ease-in-out infinite" : undefined,
          }}
        />
      )}
      {children}
    </span>
  );
}
