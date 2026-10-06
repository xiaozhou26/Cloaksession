export type UnlistenFn = () => void;

declare global {
  interface Window {
    go?: {
      main?: {
        App?: {
          Invoke(command: string, args: Record<string, unknown>): Promise<any>;
        };
      };
    };
    runtime?: {
      EventsOn(name: string, callback: (payload: any) => void): UnlistenFn;
    };
  }
}

/** Go returns decoded JSON values, not a JSON string or a response envelope. */
export async function invoke<T>(command: string, args: Record<string, unknown> = {}): Promise<T> {
  const app = window.go?.main?.App;
  if (!app?.Invoke) throw new Error("Wails command bridge is unavailable. Open Cloaksession in the desktop app.");
  try {
    return await app.Invoke(command, args) as T;
  } catch (error) {
    // Wails rejects Go errors as strings; UI callers expect Error.message.
    throw error instanceof Error ? error : new Error(String(error));
  }
}

/** Keep asynchronous registration in the public IPC API; Wails subscribes synchronously. */
export async function listen<T>(name: string, callback: (payload: T) => void): Promise<UnlistenFn> {
  const runtime = window.runtime;
  if (!runtime?.EventsOn) throw new Error("Wails event bridge is unavailable. Open Cloaksession in the desktop app.");
  const unsubscribe = runtime.EventsOn(name, callback);
  let active = true;
  return () => {
    if (!active) return;
    active = false;
    unsubscribe();
  };
}
