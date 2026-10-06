import type { JSX } from "react";
import { Search, Settings as SettingsIcon } from "lucide-react";
import { Cube, Pill, Kbd } from "../atoms";

interface Props {
  totalCount: number;
  runningCount: number;
  mcpUrl: string | null;
  onCmdK: () => void;
  onSettings: () => void;
}

export function TopBar({ totalCount, runningCount, mcpUrl, onCmdK, onSettings }: Props): JSX.Element {
  return (
    <div
      role="banner"
      className="drag-region flex min-w-0 items-center gap-2 sm:gap-3.5 relative flex-shrink-0"
      style={{
        height: 44,
        padding: "0 14px",
        background: "rgba(10,11,15,0.7)",
        backdropFilter: "blur(20px)",
        borderBottom: "1px solid rgba(255,255,255,0.05)",
      }}
    >
      {/* Brand — non-interactive decoration: pointer-events:none so the label
          isn't selectable and pointer events fall through to the drag region. */}
      <div className="flex shrink-0 items-center gap-2 sm:ml-2 pointer-events-none">
        <Cube size={20} />
        <span className="hidden sm:inline font-bold text-[13px] tracking-tight text-slate-100">Cloaksession</span>
      </div>

      {/* Search trigger — center, opens command palette */}
      <button
        type="button"
        onClick={onCmdK}
        aria-label="Search profiles, tags, URLs"
        className="no-drag min-w-0 flex-1 flex items-center gap-2.5 cursor-pointer"
        style={{
          maxWidth: 540,
          margin: "0 auto",
          padding: "6px 12px",
          borderRadius: 10,
          background: "rgba(255,255,255,0.03)",
          boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.06)",
        }}
      >
        <Search size={14} className="shrink-0 text-slate-500" />
        <span className="min-w-0 flex-1 truncate text-left text-[13px] text-slate-500">
          <span className="sm:hidden">Search profiles…</span>
          <span className="hidden sm:inline">Search profiles, tags, urls…</span>
        </span>
        <span className="hidden sm:inline-flex"><Kbd>⌘ K</Kbd></span>
      </button>

      {/* Right cluster */}
      <div className="no-drag flex shrink-0 items-center gap-2 sm:gap-3">
        <div className="hidden sm:flex items-center gap-1.5 whitespace-nowrap text-[11px] text-slate-500">
          <span className="mono text-slate-300 text-[12px]">{runningCount}</span>
          <span>running</span>
          <span className="text-slate-600">·</span>
          <span className="mono text-slate-400 text-[12px]">{totalCount}</span>
          <span>total</span>
        </div>
        <span className="hidden sm:inline-flex whitespace-nowrap">
          <Pill kind={mcpUrl ? "running" : "idle"} dot={!!mcpUrl}>
            MCP {mcpUrl ? `· :${new URL(mcpUrl).port}` : "off"}
          </Pill>
        </span>
        <button
          type="button"
          onClick={onSettings}
          className="w-7 h-7 rounded-lg flex items-center justify-center text-slate-400 hover:bg-white/5 hover:text-slate-200 transition-colors"
          aria-label="Settings"
        >
          <SettingsIcon size={14} />
        </button>
      </div>
    </div>
  );
}
