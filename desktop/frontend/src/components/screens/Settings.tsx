import { dialog, settings as settingsApi, system, update, onUpdateStatus } from "../../lib/ipc";
import { useEffect, useState, type JSX, type ReactNode } from "react";
import {
  Boxes,
  Check,
  Chrome,
  Copy,
  DownloadCloud,
  Eye,
  EyeOff,
  FileSearch,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Zap,
} from "lucide-react";
import { Pill } from "../atoms";
import { ChromixSettingsEditor } from "./ChromixSettingsEditor";
import { relativeTime } from "../../lib/relativeTime";
import type { AppSettings, SystemInfo, UpdateStatus } from "../../types";

interface Props {
  onImport: () => void;
}

export function Settings({ onImport }: Props): JSX.Element {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [info, setInfo] = useState<SystemInfo | null>(null);
  const [settingsError, setSettingsError] = useState<string | null>(null);
  const [mcpPort, setMcpPort] = useState("");
  useEffect(() => {
    if (settings) setMcpPort(String(settings.mcpHttpPort));
  }, [settings?.mcpHttpPort]);
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [copied, setCopied] = useState(false);
  const [tokenCopied, setTokenCopied] = useState(false);
  const [tokenShown, setTokenShown] = useState(false);
  const [updateStatus, setUpdateStatus] = useState<UpdateStatus | null>(null);
  const [lastChecked, setLastChecked] = useState<number>(0);

  useEffect(() => {
    let unlisten = (): void => {};
    let active = true;
    const failed = (error: unknown): void => {
      if (active) setSettingsError(`Could not load settings: ${error instanceof Error ? error.message : String(error)}`);
    };
    void settingsApi.get().then(setSettings).catch(failed);
    void system.info().then(setInfo).catch(failed);
    void update.status().then(setUpdateStatus).catch(failed);
    void update.lastChecked().then(setLastChecked).catch(failed);
    // Refresh "last checked" on every status change too, so a background
    // auto-check updates the label live while Settings is open.
    void onUpdateStatus((s) => {
      setUpdateStatus(s);
      void update.lastChecked().then(setLastChecked).catch(failed);
    }).then((fn) => {
      if (active) unlisten = fn;
      else fn();
    }).catch(failed);
    return () => {
      active = false;
      unlisten();
    };
  }, [loadAttempt]);

  async function patch(p: Partial<AppSettings>): Promise<void> {
    if (!settings) return;
    try {
      const next = await settingsApi.update(p);
      setSettings(next);
      setSettingsError(null);
    } catch (error) {
      setSettingsError(`Could not save settings: ${String(error)}`);
    }
  }

  async function checkForUpdates(): Promise<void> {
    await update.check();
    setLastChecked(await update.lastChecked());
  }

  function copyMcpUrl(): void {
    if (!info?.mcpHttpUrl) return;
    navigator.clipboard.writeText(info.mcpHttpUrl);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  }

  function copyMcpToken(): void {
    if (!info?.mcpAuthToken) return;
    navigator.clipboard.writeText(info.mcpAuthToken);
    setTokenCopied(true);
    window.setTimeout(() => setTokenCopied(false), 1500);
  }

  const retryLoad = (): void => {
    setSettingsError(null);
    setLoadAttempt((attempt) => attempt + 1);
  };
  const loadError = settingsError && (
    <div role="alert" className="text-[12px] text-red-300 mb-3 break-words">
      <p>{settingsError}</p>
      <button type="button" className="btn-secondary mt-2 rounded-lg px-3 py-1.5" onClick={retryLoad}>Retry settings</button>
    </div>
  );

  if (!settings) {
    return (
      <div className="flex-1 overflow-auto p-8">
        <div className="max-w-[720px] mx-auto text-[13px] text-slate-500">
          {loadError || "Loading…"}
        </div>
      </div>
    );
  }

  const mcpRunning = !!info?.mcpHttpUrl && !info.mcpError;
  const validMcpPort = /^\d+$/.test(mcpPort) && Number(mcpPort) >= 1 && Number(mcpPort) <= 65535;

  return (
    <div role="region" aria-label="Settings" className="flex-1 min-w-0 overflow-auto px-3 py-5 sm:px-8 sm:py-6">
      <div className="max-w-[720px] mx-auto">
        <div className="text-lg font-bold tracking-tight text-slate-100 mb-1.5">Settings</div>
        <div className="text-[13px] text-slate-500 mb-5">
          MCP server, browser startup, archives and build info. Configuration is stored locally.
          Chromix uses Playwright with a local browser executable.
        </div>

        {loadError}

        <Row
          icon={<Zap size={16} strokeWidth={1.5} />}
          title="MCP server"
          desc="Local HTTP transport that Cursor / Claude Desktop / Cline / any MCP client connects to. Requires the auth token below. The MCP tab has ready-to-paste client configs."
        >
          <div className="flex gap-2 items-center flex-wrap">
            <Pill kind={info?.mcpError ? "error" : mcpRunning ? "running" : "idle"} dot={mcpRunning}>
              {info?.mcpError ? "unavailable" : mcpRunning ? `running on :${new URL(info!.mcpHttpUrl).port}` : "off"}
            </Pill>

            {mcpRunning && info && (
              <div
                className="flex-1 min-w-0 basis-[220px] flex items-center gap-2"
                style={{
                  padding: "8px 12px",
                  borderRadius: 10,
                  background: "rgba(255,255,255,0.03)",
                  boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.06)",
                }}
              >
                <span className="flex-1 min-w-0 mono text-[12px] text-slate-300 truncate">
                  {info.mcpHttpUrl}
                </span>
                <button
                  type="button"
                  onClick={copyMcpUrl}
                  className="text-purple-400 hover:text-purple-300 transition-colors"
                  aria-label="Copy URL"
                >
                  {copied ? <Check size={13} /> : <Copy size={13} />}
                </button>
              </div>
            )}
          </div>

          {mcpRunning && info?.mcpAuthToken && (
            <div className="mt-2">
              <div className="text-[11px] text-slate-500 mb-1">
                Auth token — required. Send as{" "}
                <span className="mono text-slate-400">Authorization: Bearer &lt;token&gt;</span>. Keep
                it secret.
              </div>
              <div
                className="flex items-center gap-2"
                style={{
                  padding: "8px 12px",
                  borderRadius: 10,
                  background: "rgba(255,255,255,0.03)",
                  boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.06)",
                }}
              >
                <span className="flex-1 min-w-0 mono text-[12px] text-slate-300 truncate">
                  {tokenShown ? info.mcpAuthToken : "•".repeat(24)}
                </span>
                <button
                  type="button"
                  onClick={() => setTokenShown((v) => !v)}
                  className="text-slate-400 hover:text-slate-200 transition-colors"
                  aria-label={tokenShown ? "Hide token" : "Reveal token"}
                >
                  {tokenShown ? <EyeOff size={13} /> : <Eye size={13} />}
                </button>
                <button
                  type="button"
                  onClick={copyMcpToken}
                  className="text-purple-400 hover:text-purple-300 transition-colors"
                  aria-label="Copy token"
                >
                  {tokenCopied ? <Check size={13} /> : <Copy size={13} />}
                </button>
              </div>
            </div>
          )}

          {info?.mcpError && <div role="alert" className="mt-3 break-words text-[12px] text-red-300">
            <p>MCP could not start: {info.mcpError}</p>
            <p className="mt-1">Profiles remain usable. Choose another port or disable auto-start below, then restart Cloaksession.</p>
          </div>}
          <div className="mt-3 space-y-1.5">
            <label htmlFor="mcp-http-port" className="block text-[12px] text-slate-400">MCP HTTP port</label>
            <div className="flex flex-wrap items-center gap-2">
              <input id="mcp-http-port" type="text" inputMode="numeric" value={mcpPort} onChange={(event) => setMcpPort(event.target.value)} aria-invalid={!validMcpPort} aria-describedby="mcp-port-help" className="min-w-0 w-28 rounded-lg bg-white/5 px-2.5 py-2 text-[12px] text-slate-200" />
              <button type="button" className="btn-secondary rounded-lg px-3 py-2 text-[12px]" disabled={!validMcpPort || Number(mcpPort) === settings.mcpHttpPort} onClick={() => void patch({ mcpHttpPort: Number(mcpPort) })}>Save MCP port</button>
            </div>
            <p id="mcp-port-help" className="text-[11px] text-slate-500">Use a port from 1 to 65535. Port and auto-start changes apply after restarting Cloaksession, not immediately.</p>
          </div>

          <label className="flex items-center gap-2.5 mt-3 text-[12px] text-slate-400 cursor-pointer">
            <input
              type="checkbox"
              checked={settings.mcpHttpEnabled}
              onChange={(e) => void patch({ mcpHttpEnabled: e.target.checked })}
              className="w-3.5 h-3.5 rounded accent-purple-500"
            />
            Auto-start MCP HTTP transport on app launch
          </label>
        </Row>

        <Row
          icon={<Chrome size={16} strokeWidth={1.5} />}
          title="Browser engine"
          desc="Global startup setting. Restart the app to apply; switching engines keeps each engine’s saved configuration."
        >
          <div className="grid gap-2 sm:grid-cols-2">
            {engineOptions.map((option) => {
              const selected = settings.browserEngine === option.value;
              return (
                <button
                  key={option.value}
                  type="button"
                  aria-pressed={selected}
                  onClick={() => void patch({ browserEngine: option.value })}
                  className="text-left p-3 rounded-lg transition-colors"
                  style={{
                    boxShadow: selected
                      ? "inset 0 0 0 1px rgba(168,85,247,0.45)"
                      : "inset 0 0 0 1px rgba(255,255,255,0.07)",
                    background: selected ? "rgba(168,85,247,0.08)" : "rgba(255,255,255,0.025)",
                  }}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-[13px] font-medium text-slate-100">{option.label}</span>
                    {selected && <Pill kind="running">selected</Pill>}
                  </div>
                  <div className="text-[11px] text-slate-500 mt-1 leading-relaxed">
                    {option.description}
                  </div>
                </button>
              );
            })}
          </div>
        </Row>

        {settings.browserEngine === "chromix" && (
          <Row
            icon={<Chrome size={16} strokeWidth={1.5} />}
            title="Chromix · Playwright"
            desc="Choose a fingerprint mode. Advanced options are optional."
          >
            <ChromixSettingsEditor
              value={settings.chromix}
              onSave={async (chromix) => {
                const next = await settingsApi.update({ chromix });
                setSettings(next);
                setSettingsError(null);
              }}
            />
          </Row>
        )}

        <Row
          icon={<FileSearch size={16} strokeWidth={1.5} />}
          title="Browser binary"
          desc={settings.browserEngine === "chromix"
            ? "Required: select your local Chromix executable. Playwright launches it directly; no SDK or browser downloader is used. Restart the app to apply."
            : "Point Cloaksession at your own Chromium / CloakBrowser executable instead of auto-downloading. Restart the app to apply."}
        >
          <div className="flex flex-wrap items-center gap-2">
            <input
              aria-label="Browser binary path"
              type="text"
              value={settings.browserBinaryPath ?? ""}
              onChange={(e) => void patch({ browserBinaryPath: e.target.value })}
              placeholder={settings.browserEngine === "chromix" ? "Required: local Chromix executable" : "Default (auto-download)"}
              className="flex-1 basis-[180px] min-w-0 h-8 px-2.5 text-[12px] text-slate-200 rounded-lg bg-white/[0.03] outline-none"
              style={{ boxShadow: "inset 0 0 0 1px rgba(255,255,255,0.07)" }}
            />
            <button
              type="button"
              className="btn-secondary px-3 h-8 text-[12px] rounded-[9px] whitespace-nowrap"
              onClick={async () => {
                const p = await dialog.pickBrowserBinary();
                if (p) await patch({ browserBinaryPath: p });
              }}
            >
              Browse…
            </button>
            {settings.browserBinaryPath && (
              <button
                type="button"
                className="btn-ghost px-2.5 h-8 text-[12px] rounded-[9px]"
                onClick={() => void patch({ browserBinaryPath: "" })}
              >
                Reset
              </button>
            )}
          </div>
          {settings.browserEngine !== "chromix" && <>
            <label className="flex items-center gap-2.5 mt-3 text-[12px] text-slate-400 cursor-pointer">
              <input type="checkbox" checked={settings.skipBrowserDownload ?? false} onChange={(e) => void patch({ skipBrowserDownload: e.target.checked })} className="w-3.5 h-3.5 rounded accent-purple-500" />
              Skip auto-download (use cached binary or the custom path above)
            </label>
            <div className="text-[11px] text-slate-600 mt-2 leading-relaxed">When on, Cloaksession never fetches a browser runtime. Launch fails if no local binary is available.</div>
          </>}
        </Row>

        <Row
          icon={<Boxes size={16} strokeWidth={1.5} />}
          title="Archives"
          desc=".mzar files are encrypted bundles of profiles — cookies, login state, fingerprints, notes — protected with a passphrase you set at export time."
        >
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              className="btn-secondary px-3 py-[7px] text-[12px] rounded-[9px]"
              onClick={onImport}
            >
              Import .mzar archive
            </button>
            <a
              href="https://github.com/multizenteam/multizen-browser#archives"
              target="_blank"
              rel="noopener"
              className="btn-ghost px-3 py-[7px] text-[12px] rounded-[9px]"
            >
              Read archive format docs
            </a>
          </div>
        </Row>

        <Row
          icon={<DownloadCloud size={16} strokeWidth={1.5} />}
          title="Updates"
          desc={
            info?.platform === "macos" || info?.platform === "darwin"
              ? "Auto-install isn't available on macOS yet — we'll notify you in-app to download the new version."
              : "Cloaksession checks for updates in the background and installs them on restart."
          }
        >
          <div className="flex items-center gap-2.5 flex-wrap">
            <button
              type="button"
              className="btn-secondary px-3 py-[7px] text-[12px] rounded-[9px] inline-flex items-center gap-1.5"
              onClick={() => void checkForUpdates()}
              disabled={updateStatus?.kind === "checking"}
            >
              <RefreshCw
                size={12}
                className={updateStatus?.kind === "checking" ? "animate-spin" : ""}
              />
              Check for updates
            </button>
            <span className="text-[12px] text-slate-400">{updateLabel(updateStatus)}</span>
          </div>
          <div className="text-[11px] text-slate-600 mt-2">
            Last checked: {lastChecked ? relativeTime(new Date(lastChecked).toISOString()) : "never"}
          </div>
          <label className="flex items-center gap-2.5 mt-3 text-[12px] text-slate-400 cursor-pointer">
            <input
              type="checkbox"
              checked={settings.autoUpdate}
              onChange={(e) => void patch({ autoUpdate: e.target.checked })}
              className="w-3.5 h-3.5 rounded accent-purple-500"
            />
            Automatically check for updates
          </label>
        </Row>

        <Row
          icon={<ShieldCheck size={16} strokeWidth={1.5} />}
          title="Anonymous usage"
          desc="Off by default. Help gauge how many people run Cloaksession."
        >
          <label className="flex items-center gap-2.5 text-[12px] text-slate-400 cursor-pointer">
            <input
              type="checkbox"
              checked={settings.usageReporting}
              onChange={(e) => void patch({ usageReporting: e.target.checked })}
              className="w-3.5 h-3.5 rounded accent-purple-500"
            />
            Send an anonymous daily heartbeat
          </label>
          <div className="text-[11px] text-slate-600 mt-2 leading-relaxed">
            When on, sends once a day: app version, OS family, and a random
            single-use token — <b>no</b> account, <b>no</b> persistent ID, and your IP is
            never stored (a coarse country is derived server-side then discarded). No
            profiles, proxies, or browsing are ever included. Set{" "}
            <code className="text-slate-500">MULTIZEN_NO_TELEMETRY=1</code> to force it off.
          </div>
        </Row>

        <Row icon={<Sparkles size={16} strokeWidth={1.5} />} title="About" desc="">
          <div className="mono text-[12px] text-slate-400 leading-relaxed">
            Cloaksession v{info?.appVersion ?? "0.0.0"} · {info?.platform ?? "—"} · Wails v2
          </div>
        </Row>
      </div>
    </div>
  );
}

const engineOptions: Array<{
  value: AppSettings["browserEngine"];
  label: string;
  description: string;
}> = [
  {
    value: "cloakbrowser",
    label: "CloakBrowser",
    description: "Source-patched Chromium from CloakHQ releases. Primary runtime.",
  },
  {
    value: "chromix",
    label: "Chromix",
    description: "Native fingerprint browser, launched locally through Playwright.",
  },
  {
    value: "cft",
    label: "Chrome for Testing",
    description: "Compatibility fallback using Google's official automation build.",
  },
];

function updateLabel(status: UpdateStatus | null): string {
  switch (status?.kind) {
    case "checking":
      return "Checking…";
    case "up-to-date":
      return "You're on the latest version";
    case "available":
      return `v${status.version} available`;
    case "downloading":
      return `Downloading v${status.version}… ${status.percent}%`;
    case "ready":
      return `v${status.version} ready — restart to update`;
    case "error":
      return `Check failed: ${status.message}`;
    default:
      return "";
  }
}

function Row({
  icon,
  title,
  desc,
  children,
}: {
  icon: ReactNode;
  title: string;
  desc: ReactNode;
  children?: ReactNode;
}): JSX.Element {
  return (
    <div
      className="flex items-start gap-3.5"
      style={{
        padding: "16px 0",
        borderBottom: "1px solid rgba(255,255,255,0.04)",
      }}
    >
      <div
        className="hidden sm:flex items-center justify-center flex-shrink-0"
        style={{
          width: 36,
          height: 36,
          borderRadius: 10,
          background: "rgba(168,85,247,0.10)",
          boxShadow: "inset 0 0 0 1px rgba(168,85,247,0.18)",
          color: "#c084fc",
        }}
      >
        {icon}
      </div>
      <div className="flex-1 min-w-0 break-words">
        <div className="text-[13px] font-semibold text-slate-100">{title}</div>
        {desc && (
          <div className="text-[12px] text-slate-500 mt-1 leading-relaxed max-w-[480px]">
            {desc}
          </div>
        )}
        {children && <div className="mt-2.5">{children}</div>}
      </div>
    </div>
  );
}
