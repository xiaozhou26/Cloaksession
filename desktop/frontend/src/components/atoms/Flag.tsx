import type { JSX } from "react";
import { cn } from "../../lib/cn";

/**
 * Small country flag chip backed by the flag-icons CSS library.
 * Pass a 2-letter ISO country code (lowercase) — e.g. "us", "gb", "de".
 */
export function Flag({ cc, large, className }: { cc?: string; large?: boolean; className?: string }): JSX.Element | null {
  if (!cc) return null;
  return <span className={cn(`fi fi-${cc.toLowerCase()}`, large && "fi-lg", className)} aria-hidden="true" />;
}

/**
 * Map common timezone strings to a country code so we can render a flag
 * next to a profile's locale / proxy. Best-effort, not exhaustive.
 */
// Curated map: same-name "Region/City" zones map to a country code by
// city when the IANA name doesn't already encode the country (e.g.
// `Europe/Luxembourg` → `lu`, `America/New_York` → `us`). Covers the
// timezones the persona generator can produce.
const TZ_TO_CC: Record<string, string> = {
  // Americas
  "America/New_York": "us",
  "America/Chicago": "us",
  "America/Los_Angeles": "us",
  "America/Denver": "us",
  "America/Phoenix": "us",
  "America/Anchorage": "us",
  "America/Honolulu": "us",
  "America/Toronto": "ca",
  "America/Vancouver": "ca",
  "America/Montreal": "ca",
  "America/Mexico_City": "mx",
  "America/Sao_Paulo": "br",
  "America/Argentina/Buenos_Aires": "ar",
  "America/Santiago": "cl",
  "America/Bogota": "co",
  "America/Lima": "pe",
  // Europe
  "Europe/London": "gb",
  "Europe/Dublin": "ie",
  "Europe/Paris": "fr",
  "Europe/Berlin": "de",
  "Europe/Madrid": "es",
  "Europe/Rome": "it",
  "Europe/Amsterdam": "nl",
  "Europe/Brussels": "be",
  "Europe/Luxembourg": "lu",
  "Europe/Zurich": "ch",
  "Europe/Vienna": "at",
  "Europe/Stockholm": "se",
  "Europe/Oslo": "no",
  "Europe/Copenhagen": "dk",
  "Europe/Helsinki": "fi",
  "Europe/Warsaw": "pl",
  "Europe/Prague": "cz",
  "Europe/Budapest": "hu",
  "Europe/Bucharest": "ro",
  "Europe/Sofia": "bg",
  "Europe/Athens": "gr",
  "Europe/Lisbon": "pt",
  "Europe/Riga": "lv",
  "Europe/Vilnius": "lt",
  "Europe/Tallinn": "ee",
  "Europe/Kyiv": "ua",
  "Europe/Kiev": "ua",
  "Europe/Moscow": "ru",
  "Europe/Istanbul": "tr",
  "Europe/Belgrade": "rs",
  "Europe/Zagreb": "hr",
  "Europe/Ljubljana": "si",
  "Europe/Bratislava": "sk",
  "Europe/Malta": "mt",
  "Europe/Andorra": "ad",
  "Europe/Monaco": "mc",
  // Asia / Pacific
  "Asia/Tokyo": "jp",
  "Asia/Shanghai": "cn",
  "Asia/Hong_Kong": "hk",
  "Asia/Taipei": "tw",
  "Asia/Singapore": "sg",
  "Asia/Seoul": "kr",
  "Asia/Bangkok": "th",
  "Asia/Jakarta": "id",
  "Asia/Manila": "ph",
  "Asia/Kuala_Lumpur": "my",
  "Asia/Ho_Chi_Minh": "vn",
  "Asia/Kolkata": "in",
  "Asia/Karachi": "pk",
  "Asia/Dhaka": "bd",
  "Asia/Dubai": "ae",
  "Asia/Riyadh": "sa",
  "Asia/Tel_Aviv": "il",
  "Asia/Jerusalem": "il",
  "Asia/Tehran": "ir",
  // Africa
  "Africa/Cairo": "eg",
  "Africa/Lagos": "ng",
  "Africa/Johannesburg": "za",
  "Africa/Nairobi": "ke",
  "Africa/Casablanca": "ma",
  // Oceania
  "Australia/Sydney": "au",
  "Australia/Melbourne": "au",
  "Australia/Perth": "au",
  "Pacific/Auckland": "nz",
};

export function ccFromTimezone(tz?: string): string | undefined {
  if (!tz) return undefined;
  return TZ_TO_CC[tz];
}

// Cached so we don't reconstruct the formatter on every render.
let regionNamesEn: Intl.DisplayNames | null = null;
function regionFormatter(): Intl.DisplayNames {
  regionNamesEn ??= new Intl.DisplayNames(["en"], { type: "region" });
  return regionNamesEn;
}

/** ISO 3166-1 alpha-2 → "Luxembourg", "United States", etc. */
export function countryNameFromCc(cc?: string): string | undefined {
  if (!cc) return undefined;
  try {
    return regionFormatter().of(cc.toUpperCase());
  } catch {
    return undefined;
  }
}
