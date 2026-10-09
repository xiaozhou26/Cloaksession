import { useEffect, useRef, useState, type JSX } from "react";
import { Bug, Copy, RefreshCw, Unplug } from "lucide-react";
import type { ProfileSummary } from "../../types";
import { Button } from "../atoms/Button";
import { debuggerApi, type DebugSession, type DebugWindow, type DebugFrame } from "../../lib/debugger";
import { listen } from "../../lib/wails";

export function ReverseDebugger({ profiles }: { profiles: ProfileSummary[] }): JSX.Element {
  const running = profiles.filter((p) => p.isRunning);
  const [profileId, setProfileId] = useState("");
  const [sessions, setSessions] = useState<DebugSession[]>([]);
  const [sessionId, setSessionId] = useState("");
  const [windows, setWindows] = useState<DebugWindow[]>([]);
  const [frames, setFrames] = useState<DebugFrame[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState("");
  const [copied, setCopied] = useState(false);
  const generation = useRef(0);
  const session = sessions.find((s) => s.debugSessionId === sessionId && s.profileId === profileId && s.status === "attached" && running.some((p) => p.id === profileId));
  const runningKey = running.map((p) => p.id).join(",");

  useEffect(() => {
    let active = true;
    debuggerApi.sessions().then((value) => {
      if (active) setSessions(value.sessions ?? []);
    }).catch((e: Error) => { if (active) setError(e.message); });
    return () => { active = false; };
  }, [runningKey]);

  useEffect(() => {
    let active = true;
    let stop: (() => void) | undefined;
    listen<DebugSession>("debugger:session-changed", (changed) => {
      if (!active) return;
      setSessions((items) => [...items.filter((s) => s.debugSessionId !== changed.debugSessionId), ...(changed.status === "detached" ? [] : [changed])]);
      if (changed.profileId === profileId && changed.status === "error") setError(changed.error || "Debugger disconnected. Attach again to reconnect.");
    }).then((unsubscribe) => { if (active) stop = unsubscribe; else unsubscribe(); }).catch((e: Error) => { if (active) setError(e.message); });
    return () => { active = false; stop?.(); };
  }, [profileId]);

  useEffect(() => {
    generation.current++;
    setWindows([]);
    setFrames([]);
    setResult("");
    setCopied(false);
    if (!session) return;
    const version = generation.current;
    debuggerApi.windows(session.debugSessionId).then((value) => {
      if (generation.current === version) setWindows(value.windows ?? []);
    }).catch((e: Error) => { if (generation.current === version) setError(e.message); });
    return () => { generation.current++; };
  }, [session?.debugSessionId]);

  async function act(operation: () => Promise<void>) {
    setBusy(true);
    setError("");
    try { await operation(); }
    catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setBusy(false); }
  }

  async function attach() {
    await act(async () => {
      const connected = await debuggerApi.attach(profileId);
      setSessions((items) => [...items.filter((s) => s.debugSessionId !== connected.debugSessionId), connected]);
      setSessionId(connected.debugSessionId);
    });
  }

  async function call(name: string, args: Record<string, unknown> = {}) {
    if (!session) return;
    await act(async () => {
      const value = await debuggerApi.call(session.debugSessionId, name, args);
      const text = value.content?.filter((item) => item.type === "text").map((item) => item.text ?? "").join("\n") ?? "Completed";
      if (value.isError) throw new Error(text);
      if (name === "select_page") setFrames([]);
      const data = value.structuredContent?.data;
      if (data?.frames) setFrames(data.frames);
      if (data?.selectedFrame) setFrames((items) => items.map((frame) => ({ ...frame, selected: frame.frameIdx === data.selectedFrame!.frameIdx })));
      setResult(text);
    });
  }

  const prompt = session ? `Use Cloaksession reverse debugging with debugSessionId "${session.debugSessionId}" for profile "${profileId}". Call select_page to inspect the current pages, then select_frame when inspecting an iframe. Use list_scripts and search_in_sources for source discovery, break_on_xhr and get_paused_info for debugging, list_network_requests for HTTP evidence, and get_websocket_messages for WebSocket frames. Start capture before reproducing traffic. Explicitly resume after inspecting a breakpoint. Export artifacts inside ${session.allowedRoot}.` : "";

  return (
    <section aria-label="Reverse debugging" className="mz-card p-4 mt-5 min-w-0">
      <div className="flex items-center gap-2 text-[13px] font-semibold text-slate-100">
        <Bug size={16} className="text-violet-400" /> Reverse debugging
      </div>
      <p className="text-[12px] text-slate-400 mt-2 leading-relaxed">
        Attach Patchright to a running profile. Select a browser page, then give the session ID to your AI agent for breakpoints, source inspection, HTTP and WebSocket analysis.
      </p>
      <div className="flex flex-wrap items-end gap-2 mt-4">
        <label className="flex-1 min-w-[180px] text-[11px] text-slate-400">
          Browser profile
          <select aria-label="Browser profile" value={profileId} disabled={busy} onChange={(event) => { setProfileId(event.target.value); setSessionId(""); setError(""); }} className="block mt-1.5 w-full h-9 px-2 rounded-md bg-slate-900 text-slate-200 border border-white/10 focus:outline-violet-400">
            <option value="">Select a running profile</option>
            {running.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
        </label>
        {session ? (
          <Button disabled={busy} leftIcon={<Unplug size={13} />} onClick={() => void act(async () => {
            await debuggerApi.detach(session.debugSessionId);
            setSessions((items) => items.filter((s) => s.debugSessionId !== session.debugSessionId));
          })}>Detach debugger</Button>
        ) : <Button variant="accent" disabled={busy || !running.some((p) => p.id === profileId)} onClick={() => void attach()}>{busy ? "Connecting…" : "Attach debugger"}</Button>}
      </div>
      {sessions.some((s) => s.profileId === profileId && s.status === "attached") && <label className="block text-[11px] text-slate-400 mt-3">
        Debug session
        <select aria-label="Debug session" value={sessionId} disabled={busy} onChange={(event) => setSessionId(event.target.value)} className="block mt-1.5 w-full h-9 px-2 rounded-md bg-slate-900 text-slate-200 border border-white/10 focus:outline-violet-400">
          <option value="">Create a separate session with Attach debugger</option>
          {sessions.filter((s) => s.profileId === profileId && s.status === "attached").map((s) => <option key={s.debugSessionId} value={s.debugSessionId}>{s.debugSessionId}</option>)}
        </select>
        Selecting an existing session shares its page and frame selection with its current users.
      </label>}
      {running.length === 0 && <p className="text-[12px] text-slate-500 mt-3">Launch a profile from All profiles to start debugging.</p>}
      {error && <div role="alert" className="mt-3 rounded-md bg-red-500/10 text-red-300 p-3 text-[12px] break-words">{error}</div>}
      {session && <div className="mt-4 space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[10px] uppercase tracking-wider text-emerald-400">Connected</span>
          <code className="text-[11px] text-slate-300 break-all">{session.debugSessionId}</code>
          <Button size="sm" disabled={busy} leftIcon={<Copy size={12} />} onClick={() => void act(async () => { await navigator.clipboard.writeText(prompt); setCopied(true); })}>{copied ? "Copied" : "Copy agent instructions"}</Button>
        </div>
        <div className="text-[11px] text-slate-500 break-all">Artifacts: {session.allowedRoot}</div>
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" disabled={busy} leftIcon={<RefreshCw size={12} />} onClick={() => void act(async () => { const value = await debuggerApi.windows(session.debugSessionId); setWindows(value.windows ?? []); })}>Refresh windows</Button>
          <Button size="sm" disabled={busy} onClick={() => void call("select_frame")}>List frames</Button>
          <Button size="sm" disabled={busy} onClick={() => void call("get_paused_info")}>Paused state</Button>
          <Button size="sm" disabled={busy} onClick={() => void call("pause_or_resume", { action: "resume", confirm: true })}>Resume execution</Button>
        </div>
        {windows.map((win) => <div key={win.windowId} className="rounded-lg border border-white/10 overflow-hidden">
          <div className="px-3 py-2 text-[10px] mono text-slate-500 bg-white/[0.02]">Window {win.windowId}</div>
          {win.tabs.map((tab) => <button key={tab.targetId} aria-label={`Select ${tab.title || tab.url}`} disabled={busy} onClick={() => void call("select_page", { targetId: tab.targetId })} className="block w-full text-left px-3 py-2.5 hover:bg-violet-500/10 focus-visible:outline-violet-400 disabled:opacity-50 border-t border-white/5">
            <span className="block text-[12px] text-slate-200 truncate">{tab.title || "Untitled page"}</span>
            <span className="block text-[11px] text-slate-500 truncate">{tab.url}</span>
          </button>)}
        </div>)}
        {windows.length === 0 && <p className="text-[12px] text-slate-500">No browser pages found. Refresh after opening a tab.</p>}
        {frames.length > 0 && <div className="space-y-1" aria-label="Frames">
          {frames.map((frame) => <button key={frame.frameIdx} disabled={busy} onClick={() => void call("select_frame", { frameIdx: frame.frameIdx })} className="block w-full text-left rounded-md px-3 py-2 text-[11px] text-slate-300 hover:bg-violet-500/10 focus-visible:outline-violet-400 truncate">
            {frame.selected ? "● " : "○ "}{frame.frameIdx === 0 ? "Main frame" : frame.name || `Frame ${frame.frameIdx}`} — {frame.url}
          </button>)}
        </div>}
        {result && <pre role="status" className="max-h-56 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-black/20 p-3 text-[11px] text-slate-300">{result}</pre>}
        <p className="text-[11px] text-slate-500">Detaching disconnects debugging and keeps the browser running. Network and WebSocket capture starts when its tools are activated.</p>
      </div>}
    </section>
  );
}
