import { invoke } from "./wails";

export interface DebugSession {
  debugSessionId: string;
  profileId: string;
  allowedRoot: string;
  status: string;
  error?: string;
}

export interface DebugWindow {
  windowId: number;
  tabs: { targetId: string; title: string; url: string }[];
}

export interface DebugFrame {
  frameIdx: number;
  url: string;
  name: string;
  selected: boolean;
}

export interface DebugResult {
  content?: { type: string; text?: string }[];
  isError?: boolean;
  structuredContent?: { data?: { windows?: DebugWindow[]; frames?: DebugFrame[]; selectedFrame?: { frameIdx: number } } };
}

export const debuggerApi = {
  sessions: () => invoke<{ sessions: DebugSession[] }>("debugger_sessions"),
  attach: (profileId: string) => invoke<DebugSession>("debugger_attach", { profileId }),
  detach: (debugSessionId: string) => invoke("debugger_detach", { debugSessionId }),
  windows: async (debugSessionId: string): Promise<{ windows: DebugWindow[] }> => {
    const result = await invoke<DebugResult>("debugger_windows", { debugSessionId });
    if (result.isError) throw new Error(result.content?.map((item) => item.text ?? "").join("\n") || "Window listing failed");
    return { windows: result.structuredContent?.data?.windows ?? [] };
  },
  call: (debugSessionId: string, name: string, args: Record<string, unknown> = {}) =>
    invoke<DebugResult>("debugger_call", { debugSessionId, name, arguments: args }),
};
