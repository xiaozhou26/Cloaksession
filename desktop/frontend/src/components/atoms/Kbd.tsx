import type { JSX, ReactNode } from "react";
import { cn } from "../../lib/cn";

interface Props {
  children: ReactNode;
  /**
   * - "default": muted chip on dark surfaces
   * - "on-brand": readable chip on the brand-gradient button (white text)
   */
  variant?: "default" | "on-brand";
  className?: string;
}

export function Kbd({ children, variant = "default", className }: Props): JSX.Element {
  if (variant === "on-brand") {
    return (
      <span
        className={cn("mz-kbd-on-brand", className)}
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          lineHeight: 1,
          padding: "2px 6px",
          borderRadius: 4,
          background: "rgba(255, 255, 255, 0.18)",
          color: "rgba(255, 255, 255, 0.95)",
          boxShadow: "inset 0 -1px 0 rgba(0, 0, 0, 0.18), inset 0 0 0 1px rgba(255, 255, 255, 0.12)",
          letterSpacing: "0.02em",
        }}
      >
        {children}
      </span>
    );
  }
  return <span className={cn("mz-kbd", className)}>{children}</span>;
}
