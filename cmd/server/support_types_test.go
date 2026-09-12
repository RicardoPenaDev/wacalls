package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateIdempotencyKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid standard", "1234567890abcdef", false},
		{"valid max length", strings.Repeat("a", 128), false},
		{"valid safe characters", "abc-DEF_123.456~789:XYZ", false},
		{"too short", "short-key-123", true},
		{"too long", strings.Repeat("a", 129), true},
		{"contains space", "12345678 90abcdef", true},
		{"contains newline", "12345678\n90abcdef", true},
		{"contains unicode", "1234567890abcdeç", true},
		{"contains at sign", "1234567890abc@def", true},
		{"contains slash", "1234567890abc/def", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIdempotencyKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateIdempotencyKey(%q) error = %v, wantErr %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeDescription(t *testing.T) {
	raw := "line1\r\nline2\rline3\nline4"
	want := "line1\nline2\nline3\nline4"
	got := NormalizeDescription(raw)
	if got != want {
		t.Fatalf("NormalizeDescription() = %q, want %q", got, want)
	}
}

func TestCalculatePayloadFingerprintV2(t *testing.T) {
	canonical := CanonicalPayloadV2{
		V:         2,
		TenantID:  "tenant-1",
		SessionID: "sess-1",
		ChatJID:   "5511999999999@s.whatsapp.net",
		DeviceBindingID: OptionalField[string]{
			Present: false,
			Value:   "",
		},
		Hostname: OptionalField[string]{
			Present: true,
			Value:   "sde-ars-rcp-02",
		},
		RequesterName: "Maria Silva",
		Title:         "Impressora com defeito",
		Description:   "Linha 1\r\nLinha 2",
		CategoryID: OptionalField[string]{
			Present: false,
			Value:   "",
		},
		LocationID: OptionalField[string]{
			Present: false,
			Value:   "",
		},
		Priority: OptionalField[int]{
			Present: true,
			Value:   3,
		},
	}

	fp1, err := CalculatePayloadFingerprintV2(canonical)
	if err != nil {
		t.Fatalf("CalculatePayloadFingerprintV2 error: %v", err)
	}
	if len(fp1) != 64 {
		t.Fatalf("fingerprint length = %d, want 64", len(fp1))
	}

	// Deterministic
	fp2, err := CalculatePayloadFingerprintV2(canonical)
	if err != nil {
		t.Fatalf("CalculatePayloadFingerprintV2 error: %v", err)
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprint not deterministic: %q vs %q", fp1, fp2)
	}

	// Any semantic difference must yield different fingerprint
	mutated := canonical
	mutated.Title = "Impressora offline"
	fp3, _ := CalculatePayloadFingerprintV2(mutated)
	if fp1 == fp3 {
		t.Fatalf("fingerprint collision on title modification: %q", fp1)
	}

	// CRLF normalization produces same fingerprint as LF
	lfCanonical := canonical
	lfCanonical.Description = "Linha 1\nLinha 2"
	fp4, _ := CalculatePayloadFingerprintV2(lfCanonical)
	if fp1 != fp4 {
		t.Fatalf("fingerprint differed on newline representation: %q vs %q", fp1, fp4)
	}
}

func TestGenerateExternalID(t *testing.T) {
	tenantID := "tenant-alpha"
	reqID := "550e8400-e29b-41d4-a716-446655440000"

	ext1 := GenerateExternalID(tenantID, reqID)
	if !strings.HasPrefix(ext1, "wacalls-") {
		t.Fatalf("GenerateExternalID() prefix missing: %q", ext1)
	}
	if len(ext1) != 40 {
		t.Fatalf("GenerateExternalID() length = %d, want 40", len(ext1))
	}

	ext2 := GenerateExternalID(tenantID, reqID)
	if ext1 != ext2 {
		t.Fatalf("GenerateExternalID() not deterministic: %q vs %q", ext1, ext2)
	}

	// Different request ID -> different external ID
	ext3 := GenerateExternalID(tenantID, "660e8400-e29b-41d4-a716-446655440001")
	if ext1 == ext3 {
		t.Fatalf("GenerateExternalID() collided on different request IDs: %q", ext1)
	}
}

func TestGenerateProcessingToken(t *testing.T) {
	tok1, err := GenerateProcessingToken()
	if err != nil {
		t.Fatalf("GenerateProcessingToken error: %v", err)
	}
	if len(tok1) != 32 {
		t.Fatalf("token length = %d, want 32", len(tok1))
	}

	tok2, err := GenerateProcessingToken()
	if err != nil {
		t.Fatalf("GenerateProcessingToken error: %v", err)
	}
	if tok1 == tok2 {
		t.Fatalf("GenerateProcessingToken generated identical tokens: %q", tok1)
	}
}

func TestStateNewNotAllowed(t *testing.T) {
	if isValidSyncState("new") {
		t.Fatal("isValidSyncState('new') must be false: StateNew must never be a valid persistent state")
	}

	states := []SupportSyncState{
		StateProcessing,
		StateSynced,
		StateRetryableError,
		StateUnknown,
		StateFailed,
	}
	for _, s := range states {
		if !isValidSyncState(s) {
			t.Fatalf("isValidSyncState(%q) want true", s)
		}
	}
}

func TestProcessingTokenNotLeakedInJSON(t *testing.T) {
	req := SupportRequest{
		ID:              "req-1",
		TenantID:        "tenant-1",
		ProcessingToken: "super-secret-token-1234567890ab",
		SyncState:       StateProcessing,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal(req) error: %v", err)
	}

	jsonStr := string(data)
	if strings.Contains(jsonStr, "super-secret-token") || strings.Contains(jsonStr, "processingToken") || strings.Contains(jsonStr, "processing_token") {
		t.Fatalf("ProcessingToken leaked into JSON: %s", jsonStr)
	}
}
