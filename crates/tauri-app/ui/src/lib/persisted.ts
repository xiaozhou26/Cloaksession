import { useCallback, useEffect, useState } from "react";

const NAMESPACE = "multizen.ui";

function read<T>(key: string, fallback: T): T {
  if (typeof window === "undefined") return fallback;
  try {
    const raw = window.localStorage.getItem(`${NAMESPACE}.${key}`);
    if (raw === null) return fallback;
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

function write<T>(key: string, value: T): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(`${NAMESPACE}.${key}`, JSON.stringify(value));
  } catch {
    // localStorage full or disabled — silently drop
  }
}

/**
 * Like useState, but value persists across app restarts under
 * localStorage[`multizen.ui.${key}`].
 *
 * Reads on mount (sync), writes on every change.
 */
export function usePersistedState<T>(key: string, initial: T): [T, (v: T | ((prev: T) => T)) => void] {
  const [value, setValue] = useState<T>(() => read(key, initial));

  useEffect(() => {
    write(key, value);
  }, [key, value]);

  const setter = useCallback(
    (v: T | ((prev: T) => T)) => {
      setValue((prev) => {
        const next = typeof v === "function" ? (v as (prev: T) => T)(prev) : v;
        return next;
      });
    },
    [],
  );

  return [value, setter];
}

/** Read once without subscribing — useful for initial check (e.g. onboarding). */
export function readPersisted<T>(key: string, fallback: T): T {
  return read(key, fallback);
}

/** Write once without state — useful for "mark onboarded" side effects. */
export function writePersisted<T>(key: string, value: T): void {
  write(key, value);
}
