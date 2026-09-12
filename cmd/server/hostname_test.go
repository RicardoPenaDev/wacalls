package main

import "testing"

func TestNormalizeHostname(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{" sde-ars-rcp-02 ", "SDE-ARS-RCP-02"},   // trim + upper (T-003 example)
		{"SDE-ARS-RCP-02", "SDE-ARS-RCP-02"},     // already canonical
		{"sde-ars-pre-01", "SDE-ARS-PRE-01"},     // lower -> upper
		{"\tSDE-ARS-CLT-04\n", "SDE-ARS-CLT-04"}, // tabs/newlines are outer ws
		{"", ""},
		{"   ", ""},
		{"sde ars rcp 02", "SDE ARS RCP 02"}, // inner spaces preserved, not removed
	}
	for _, c := range cases {
		if got := normalizeHostname(c.in); got != c.want {
			t.Errorf("normalizeHostname(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHostnamesMatch(t *testing.T) {
	// Equal after normalization (case/outer-space only) -> true.
	matches := [][2]string{
		{"SDE-ARS-RCP-02", "sde-ars-rcp-02"},
		{"  SDE-ARS-PRE-01  ", "SDE-ARS-PRE-01"},
		{"sde-ars-clt-04", "SDE-ARS-CLT-04"},
	}
	for _, m := range matches {
		if !hostnamesMatch(m[0], m[1]) {
			t.Errorf("hostnamesMatch(%q, %q) = false, want true", m[0], m[1])
		}
	}
	// Must NOT match: partial/near, legacy variants, blanks (no fuzzy matching).
	noMatches := [][2]string{
		{"SDE-ARS-RCP-02", "SDE-ARS-RCP01"},   // missing hyphen legacy form
		{"SDE-ARS-RCP-02", "SDE-ARS-RCP-01"},  // different number
		{"SDE-ARS-RCP-02", "SDE-ARS-RCP"},     // prefix only
		{"SDE-ARS-RCP-02", "SDE-ARS-RCP-020"}, // longer number
		{"", ""},                              // both blank never match
		{"SDE-ARS-RCP-02", ""},                // blank never resolves
		{"   ", "SDE-ARS-RCP-02"},
	}
	for _, m := range noMatches {
		if hostnamesMatch(m[0], m[1]) {
			t.Errorf("hostnamesMatch(%q, %q) = true, want false", m[0], m[1])
		}
	}
}

func TestParseHostname(t *testing.T) {
	valid := []struct {
		in, sector string
	}{
		{"SDE-ARS-RCP-02", "RCP"}, // official examples (docs/HOSTNAMES.md)
		{"SDE-ARS-PRE-01", "PRE"},
		{"SDE-ARS-CLT-04", "CLT"},
		{"SDE-ARS-COD-01", "COD"},
		{"SDE-BEA-ENF-01", "ENF"},
		{" sde-ars-rcp-02 ", "RCP"}, // normalized before parsing
	}
	for _, c := range valid {
		sector, ok := parseHostname(c.in)
		if !ok || sector != c.sector {
			t.Errorf("parseHostname(%q) = (%q, %v), want (%q, true)", c.in, sector, ok, c.sector)
		}
	}

	// Out-of-pattern (legacy) names: ok=false and no invented sector.
	invalid := []string{
		"SDE-BEA-COORD",    // no number field
		"SDE-ARS-RCP01",    // number glued to sector (3 fields)
		"SDE-BEA-FISIO",    // legacy, no number
		"SDE-ARS-RCP-2",    // single digit
		"SDE-ARS-RCP-002",  // three digits
		"SDE-ARS-RCP-0A",   // non-digit in number
		"SDE-ARS--02",      // empty sector field
		"SDE-ARS-RCP-02-X", // extra field
		"",
		"   ",
	}
	for _, in := range invalid {
		if sector, ok := parseHostname(in); ok || sector != "" {
			t.Errorf("parseHostname(%q) = (%q, %v), want (\"\", false)", in, sector, ok)
		}
	}
}
