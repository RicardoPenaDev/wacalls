package main

import "strings"

// Hostname helpers for the support domain (device bindings). Pure functions,
// no I/O. The canonical hostname pattern is documented in docs/HOSTNAMES.md:
//
//	{SECRETARIA}-{UNIDADE}-{SETOR}-{NN}   e.g. SDE-ARS-RCP-02
//
// Safety rules (docs/HOSTNAMES.md, T-003):
//   - normalization only folds case and trims OUTER whitespace;
//   - never inserts hyphens or "fixes" a malformed name into a valid one;
//   - matching is exact after normalization — no fuzzy/partial matching;
//   - the parser never invents secretaria/unidade/setor for out-of-pattern
//     (legacy) names: it reports ok=false instead.
//
// The original hostname is always preserved by the caller for display/audit;
// the normalized form exists only for search and uniqueness.

// normalizeHostname folds a hostname to its canonical comparison form:
// outer whitespace trimmed and ASCII letters upper-cased. Inner characters
// (including inner spaces) are preserved as-is — nothing is inserted, removed
// or "corrected".
func normalizeHostname(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// hostnamesMatch reports whether two hostnames refer to the same device by
// EXACT equality after normalization. Two empty (or whitespace-only) hostnames
// never match, so a blank input can never be resolved by approximation.
func hostnamesMatch(a, b string) bool {
	na := normalizeHostname(a)
	if na == "" {
		return false
	}
	return na == normalizeHostname(b)
}

// parseHostname extracts the sector code from a hostname that follows the
// canonical pattern {SEC}-{UNI}-{SETOR}-{NN}. It returns ok=false for anything
// that does not match the pattern exactly (legacy/aliased names), without
// guessing a sector. The input is normalized first, so case and outer spaces
// do not matter.
//
// A valid hostname has exactly four hyphen-separated fields where:
//   - the first three fields are non-empty and contain only A–Z or 0–9;
//   - the last field is exactly two ASCII digits (NN).
//
// Examples: SDE-ARS-RCP-02 -> ("RCP", true); SDE-BEA-COORD -> ("", false);
// SDE-ARS-RCP01 -> ("", false).
func parseHostname(s string) (sector string, ok bool) {
	parts := strings.Split(normalizeHostname(s), "-")
	if len(parts) != 4 {
		return "", false
	}
	for _, p := range parts[:3] {
		if !isCode(p) {
			return "", false
		}
	}
	if !isTwoDigits(parts[3]) {
		return "", false
	}
	return parts[2], true
}

// isCode reports whether a field is a non-empty run of uppercase ASCII letters
// or digits (the allowed hostname charset after normalization).
func isCode(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// isTwoDigits reports whether s is exactly two ASCII digits.
func isTwoDigits(s string) bool {
	if len(s) != 2 {
		return false
	}
	return s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9'
}
