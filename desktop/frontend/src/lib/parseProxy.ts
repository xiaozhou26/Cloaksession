/**
 * Parse a one-line proxy string into its parts so a paste can auto-fill the
 * whole proxy form — the convenience every anti-detect tool ships (AdsPower,
 * GoLogin, Multilogin all do it).
 *
 * Supported shapes (scheme optional, credentials optional):
 *   host:port
 *   host:port:user:pass                       ← Smartproxy/IPRoyal style
 *   user:pass@host:port                       ← cURL / standard URL style
 *   host:port@user:pass                       ← some panels export this
 *   scheme://host:port[:user:pass]
 *   scheme://user:pass@host:port
 *
 * `scheme` sets the type: any "socks"-prefixed scheme → "socks5",
 * http/https → "http".
 * Without a scheme `type` is left undefined so the caller keeps the type the
 * user already selected.
 *
 * Passwords may contain ':' (taken as everything after the 3rd colon in the
 * colon format) and '@' (kept verbatim when neither side of an '@' looks like
 * host:port). Returns null when the string isn't a usable proxy (no numeric
 * port, or just a bare hostname) — the caller then leaves the field as typed.
 *
 * Note: bracketed IPv6 literals ([::1]:8080) are not supported — proxy panels
 * effectively never export them, and the ':'-splitting would mangle them.
 */
export interface ParsedProxy {
  type?: "http" | "socks5";
  host: string;
  port: number;
  username?: string;
  password?: string;
}

export function parseProxyString(raw: string): ParsedProxy | null {
  let s = raw.trim();
  if (!s) return null;

  // 1. Optional scheme prefix → drives the proxy type.
  let type: ParsedProxy["type"];
  const scheme = /^([a-z][a-z0-9+.-]*):\/\/(.*)$/i.exec(s);
  if (scheme) {
    const name = (scheme[1] ?? "").toLowerCase();
    s = scheme[2] ?? s;
    if (name.startsWith("socks")) type = "socks5";
    else if (name === "http" || name === "https") type = "http";
    // Unknown scheme: keep parsing, leave type for the caller to decide.
  }

  // 2. Credentials separated by '@'. Only split when one side actually looks
  //    like host:port — otherwise the '@' is part of a colon-format password.
  let credsPart: string | undefined;
  let hostPart = s;
  const at = s.lastIndexOf("@");
  if (at !== -1) {
    const left = s.slice(0, at);
    const right = s.slice(at + 1);
    const leftHP = looksLikeHostPort(left);
    const rightHP = looksLikeHostPort(right);
    if (leftHP && rightHP) {
      // Both sides look like host:port (e.g. `host:port@user:<numeric pass>`).
      // Prefer the side whose first segment looks like a real hostname/IP;
      // otherwise default to the right (standard `user:pass@host:port`).
      if (hostLooksReal(left) && !hostLooksReal(right)) {
        hostPart = left;
        credsPart = right;
      } else {
        hostPart = right;
        credsPart = left;
      }
    } else if (rightHP) {
      hostPart = right;
      credsPart = left;
    } else if (leftHP) {
      hostPart = left;
      credsPart = right;
    }
  }

  let username: string | undefined;
  let password: string | undefined;
  if (credsPart !== undefined) {
    const ci = credsPart.indexOf(":");
    if (ci === -1) {
      username = credsPart || undefined;
    } else {
      username = credsPart.slice(0, ci) || undefined;
      password = credsPart.slice(ci + 1) || undefined;
    }
  }

  // 3. hostPart is host:port, optionally with trailing :user:pass.
  const parts = hostPart.split(":");
  if (parts.length < 2) return null; // need at least host:port

  const host = (parts[0] ?? "").trim();
  const port = Number((parts[1] ?? "").trim());
  if (!host || !Number.isInteger(port) || port < 1 || port > 65535) return null;

  // Colon-format credentials, only if '@' didn't already supply them.
  if (username === undefined && parts.length >= 3) {
    username = parts[2] || undefined;
    if (parts.length >= 4) {
      // Re-join so a ':' inside the password survives.
      password = parts.slice(3).join(":") || undefined;
    }
  }

  return { type, host, port, username, password };
}

/** True when `s` is "<something>:<valid port>[:...]". */
function looksLikeHostPort(s: string): boolean {
  const parts = s.split(":");
  if (parts.length < 2 || !parts[0]) return false;
  const port = Number((parts[1] ?? "").trim());
  return Number.isInteger(port) && port >= 1 && port <= 65535;
}

/** True if the first segment looks like a real hostname/IP, not a bare username. */
function hostLooksReal(s: string): boolean {
  const host = (s.split(":")[0] ?? "").trim();
  return host === "localhost" || host.includes(".");
}
