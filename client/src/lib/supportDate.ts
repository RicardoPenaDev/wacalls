/**
 * Safely converts a Unix timestamp in seconds to a JavaScript Date.
 * Returns null if the value is missing, zero, negative, or invalid (NaN/Infinity).
 * Prevents accidental rendering of 01/01/1970 for unset/invalid timestamps.
 */
export function unixSecondsToDate(seconds?: number | null): Date | null {
  if (seconds === undefined || seconds === null || typeof seconds !== "number") {
    return null;
  }
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return null;
  }
  const date = new Date(seconds * 1000);
  if (Number.isNaN(date.getTime())) {
    return null;
  }
  return date;
}

/**
 * Formats a Unix timestamp in seconds as a localized string.
 * Returns fallback (default "—") if invalid or unset.
 */
export function formatUnixSeconds(
  seconds?: number | null,
  options?: Intl.DateTimeFormatOptions,
  fallback = "—",
): string {
  const date = unixSecondsToDate(seconds);
  if (!date) {
    return fallback;
  }
  return date.toLocaleString(undefined, options);
}
