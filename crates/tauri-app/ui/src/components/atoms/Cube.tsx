import type { JSX } from "react";
// Vite serves files in `public/` at the root path. Use an absolute
// string literal (`/logo.png`) rather than an `?url` import — Vite's
// `?url` suffix targets source-tree assets, not public-dir files, and
// Tauri's webview resolves `/logo.png` against the bundled assets.
const logoUrl = "/logo.png";

interface Props {
  size?: number;
  glow?: boolean;
  className?: string;
}

/**
 * Brand mark — the 3D-ish cube with orange→pink→purple→blue gradient.
 * Renders the bundled PNG asset; falls back to a CSS gradient div.
 */
export function Cube({ size = 28, glow = true, className }: Props): JSX.Element {
  return (
    <img
      src={logoUrl}
      alt="Cloaksession"
      width={size}
      height={size}
      className={className}
      style={{
        display: "block",
        flexShrink: 0,
        filter: glow ? `drop-shadow(0 ${size * 0.15}px ${size * 0.5}px rgba(255, 61, 138, 0.35))` : undefined,
      }}
    />
  );
}
