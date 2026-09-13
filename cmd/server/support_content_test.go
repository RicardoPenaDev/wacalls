package main

import (
	"strings"
	"testing"
)

func TestFormatTicketContent_XSSPrevention(t *testing.T) {
	host := "<script>alert('pwned')</script>"
	compID := `123" onmouseover="alert(1)`
	req := &SupportRequest{
		Description:            `<img src=x onerror=alert('xss')>` + "\n" + `Line 2 <tag>`,
		TicketHostnameInformed: &host,
		TicketGLPIComputerID:   &compID,
		ExternalID:             "wacalls-1234567890abcdef1234567890abcdef",
	}

	content := FormatTicketContent(req)

	// Verify dangerous raw HTML tags are NOT present
	dangerous := []string{
		"<script>", "</script>",
		"<img",
		`" onmouseover=`,
		"<tag>",
	}
	for _, d := range dangerous {
		if strings.Contains(content, d) {
			t.Fatalf("dangerous tag leaked unescaped in ticket content: %q", d)
		}
	}

	// Verify escaped entities are present
	expected := []string{
		"&lt;img src=x onerror=alert(&#39;xss&#39;)&gt;<br>Line 2 &lt;tag&gt;",
		"&lt;script&gt;alert(&#39;pwned&#39;)&lt;/script&gt;",
		"123&#34; onmouseover=&#34;alert(1)",
		"Referência: wacalls-1234567890abcdef1234567890abcdef",
	}
	for _, e := range expected {
		if !strings.Contains(content, e) {
			t.Fatalf("expected escaped segment not found: %q in %q", e, content)
		}
	}
}

func TestFormatTicketContent_DefaultsWhenNil(t *testing.T) {
	req := &SupportRequest{
		Description: "Simple text without device.",
		ExternalID:  "wacalls-ext-1",
	}

	content := FormatTicketContent(req)

	if !strings.Contains(content, "Equipamento informado: não informado") {
		t.Fatalf("expected 'não informado', got: %s", content)
	}
	if !strings.Contains(content, "GLPI Computer ID: não confirmado") {
		t.Fatalf("expected 'não confirmado', got: %s", content)
	}
	if !strings.Contains(content, "Referência: wacalls-ext-1") {
		t.Fatalf("expected external_id, got: %s", content)
	}
}
