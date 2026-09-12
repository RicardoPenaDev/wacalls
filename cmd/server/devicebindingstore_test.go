package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func newTestDeviceBindingStore(t *testing.T) (*deviceBindingStore, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "devbind_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st, err := newDeviceBindingStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	return st, ctx
}

func (s *deviceBindingStore) countForTenant(t *testing.T, ctx context.Context, tenantID, normalized string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM device_bindings WHERE tenant_id=? AND hostname_normalized=?`,
		tenantID, normalized).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDeviceBindingUpsertCreateAndRead(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)

	in := DeviceBinding{TenantID: "t1", OwnerID: "o1", Hostname: " sde-ars-rcp-02 ", SectorCode: "RCP"}
	got, err := st.Upsert(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "" {
		t.Fatal("expected generated id")
	}
	if got.Hostname != "sde-ars-rcp-02" { // trimmed, original case preserved for display
		t.Errorf("hostname not preserved verbatim: %q", got.Hostname)
	}
	if got.HostnameNormalized != "SDE-ARS-RCP-02" {
		t.Errorf("hostname_normalized = %q, want SDE-ARS-RCP-02", got.HostnameNormalized)
	}
	if got.MatchStatus != "pending" {
		t.Errorf("default match_status = %q, want pending", got.MatchStatus)
	}
	if got.CreatedAt == 0 || got.UpdatedAt == 0 {
		t.Errorf("timestamps not set: %+v", got)
	}

	byID, err := st.Get(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if byID.ID != got.ID || byID.HostnameNormalized != got.HostnameNormalized {
		t.Errorf("Get mismatch: %+v vs %+v", byID, got)
	}
}

func TestDeviceBindingUpsertIdempotent(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)

	first, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "SDE-ARS-RCP-02"})
	if err != nil {
		t.Fatal(err)
	}
	// Same device, different case/spacing -> same normalized key -> no new row.
	second, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "  sde-ars-rcp-02 "})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Errorf("idempotent upsert changed id: %q -> %q", first.ID, second.ID)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Errorf("created_at changed on update: %d -> %d", first.CreatedAt, second.CreatedAt)
	}
	if n := st.countForTenant(t, ctx, "t1", "SDE-ARS-RCP-02"); n != 1 {
		t.Fatalf("expected exactly 1 row, got %d", n)
	}

	// Repeated upserts stay safe (idempotent).
	for range 5 {
		if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "SDE-ARS-RCP-02"}); err != nil {
			t.Fatal(err)
		}
	}
	if n := st.countForTenant(t, ctx, "t1", "SDE-ARS-RCP-02"); n != 1 {
		t.Fatalf("after repeats expected 1 row, got %d", n)
	}
}

func TestDeviceBindingUpdateExternalIDs(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)

	// Enrichment: fill GLPI/Tactical ids on an existing binding.
	if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "SDE-ARS-RCP-02"}); err != nil {
		t.Fatal(err)
	}
	enriched, err := st.Upsert(ctx, DeviceBinding{
		TenantID: "t1", Hostname: "SDE-ARS-RCP-02",
		GLPIComputerID: "G1", TacticalAgentID: "A1", TacticalClientID: "C1", TacticalSiteID: "S1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if enriched.GLPIComputerID != "G1" || enriched.TacticalAgentID != "A1" ||
		enriched.TacticalClientID != "C1" || enriched.TacticalSiteID != "S1" {
		t.Fatalf("ids not persisted: %+v", enriched)
	}

	// Update: change ids to new non-empty values.
	updated, err := st.Upsert(ctx, DeviceBinding{
		TenantID: "t1", Hostname: "SDE-ARS-RCP-02",
		GLPIComputerID: "G2", TacticalAgentID: "A2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GLPIComputerID != "G2" || updated.TacticalAgentID != "A2" {
		t.Fatalf("ids not updated: %+v", updated)
	}
	// A blank incoming value must NOT clear a stored id (enrichment semantics).
	if updated.TacticalClientID != "C1" || updated.TacticalSiteID != "S1" {
		t.Fatalf("blank incoming cleared stored ids: %+v", updated)
	}
}

func TestDeviceBindingTenantIsolation(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)

	a, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "SDE-ARS-RCP-02", GLPIComputerID: "G-T1"})
	if err != nil {
		t.Fatal(err)
	}
	// Same hostname under a different tenant must coexist as a separate row.
	b, err := st.Upsert(ctx, DeviceBinding{TenantID: "t2", Hostname: "SDE-ARS-RCP-02", GLPIComputerID: "G-T2"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("expected distinct rows per tenant")
	}

	// FindByHostname is tenant-scoped and does not leak across tenants.
	f1, ok1, err := st.FindByHostname(ctx, "t1", "sde-ars-rcp-02")
	if err != nil || !ok1 {
		t.Fatalf("t1 lookup failed: ok=%v err=%v", ok1, err)
	}
	if f1.ID != a.ID || f1.GLPIComputerID != "G-T1" {
		t.Fatalf("t1 got wrong row: %+v", f1)
	}
	f2, ok2, err := st.FindByHostname(ctx, "t2", "SDE-ARS-RCP-02")
	if err != nil || !ok2 {
		t.Fatalf("t2 lookup failed: ok=%v err=%v", ok2, err)
	}
	if f2.ID != b.ID || f2.GLPIComputerID != "G-T2" {
		t.Fatalf("t2 got wrong row: %+v", f2)
	}

	// Search is tenant-scoped too.
	res, err := st.Search(ctx, "t1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != a.ID {
		t.Fatalf("t1 search leaked tenants: %+v", res)
	}
}

func TestDeviceBindingFindByHostname(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)

	if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "SDE-ARS-RCP-02"}); err != nil {
		t.Fatal(err)
	}

	// Present, case-insensitive.
	if _, ok, err := st.FindByHostname(ctx, "t1", "  sde-ars-rcp-02 "); err != nil || !ok {
		t.Fatalf("expected found, ok=%v err=%v", ok, err)
	}
	// Absent (valid hostname) -> (zero, false, nil), not an error.
	got, ok, err := st.FindByHostname(ctx, "t1", "SDE-ARS-PRE-01")
	if err != nil || ok || got.ID != "" {
		t.Fatalf("expected not found without error, got %+v ok=%v err=%v", got, ok, err)
	}
	// Missing tenant / blank hostname -> distinguishable errors.
	if _, _, err := st.FindByHostname(ctx, "", "SDE-ARS-RCP-02"); !errors.Is(err, ErrMissingTenant) {
		t.Fatalf("expected ErrMissingTenant, got %v", err)
	}
	if _, _, err := st.FindByHostname(ctx, "t1", "   "); !errors.Is(err, ErrInvalidHostname) {
		t.Fatalf("expected ErrInvalidHostname, got %v", err)
	}
}

func TestDeviceBindingGetNotFound(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)
	if _, err := st.Get(ctx, "does-not-exist"); !errors.Is(err, ErrDeviceBindingNotFound) {
		t.Fatalf("expected ErrDeviceBindingNotFound, got %v", err)
	}
}

func TestDeviceBindingSearchPrefix(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)
	hosts := []string{"SDE-ARS-RCP-02", "SDE-ARS-PRE-01", "SDE-BEA-ENF-01"}
	for _, h := range hosts {
		if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: h}); err != nil {
			t.Fatal(err)
		}
	}

	// Prefix filters and is case-insensitive; ordered by normalized hostname.
	res, err := st.Search(ctx, "t1", "sde-ars")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].HostnameNormalized != "SDE-ARS-PRE-01" || res[1].HostnameNormalized != "SDE-ARS-RCP-02" {
		t.Fatalf("unexpected prefix search result: %+v", res)
	}
	// Empty query lists all for the tenant.
	all, err := st.Search(ctx, "t1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 rows for empty query, got %d", len(all))
	}
	// No match -> empty (non-nil) slice, no error.
	none, err := st.Search(ctx, "t1", "XYZ")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no matches, got %+v", none)
	}
	// Missing tenant -> error.
	if _, err := st.Search(ctx, "", "SDE"); !errors.Is(err, ErrMissingTenant) {
		t.Fatalf("expected ErrMissingTenant, got %v", err)
	}
}

func TestDeviceBindingUpsertInvalidInput(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)
	if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "   "}); !errors.Is(err, ErrInvalidHostname) {
		t.Fatalf("blank hostname: expected ErrInvalidHostname, got %v", err)
	}
	if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "", Hostname: "SDE-ARS-RCP-02"}); !errors.Is(err, ErrMissingTenant) {
		t.Fatalf("blank tenant: expected ErrMissingTenant, got %v", err)
	}
}

func TestDeviceBindingConflictOnReusedID(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)

	first, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "SDE-ARS-RCP-02"})
	if err != nil {
		t.Fatal(err)
	}
	// Reusing an existing id for a different (tenant, hostname) hits the primary
	// key: the (tenant, hostname) upsert target does not apply, so it surfaces
	// as a distinguishable conflict rather than a raw driver error.
	_, err = st.Upsert(ctx, DeviceBinding{ID: first.ID, TenantID: "t1", Hostname: "SDE-ARS-PRE-01"})
	if !errors.Is(err, ErrDeviceBindingConflict) {
		t.Fatalf("expected ErrDeviceBindingConflict, got %v", err)
	}
}

// statusOf reads the persisted match_status for a (tenant, hostname).
func statusOf(t *testing.T, st *deviceBindingStore, ctx context.Context, tenant, host string) string {
	t.Helper()
	b, ok, err := st.FindByHostname(ctx, tenant, host)
	if err != nil || !ok {
		t.Fatalf("FindByHostname(%q,%q): ok=%v err=%v", tenant, host, ok, err)
	}
	return b.MatchStatus
}

func TestDeviceBindingMatchStatusInsertDefault(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)
	// (1) insert without status -> pending.
	got, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "SDE-ARS-RCP-02"})
	if err != nil {
		t.Fatal(err)
	}
	if got.MatchStatus != "pending" {
		t.Fatalf("insert default = %q, want pending", got.MatchStatus)
	}
}

func TestDeviceBindingMatchStatusPreservedOnBlankUpdate(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)
	const host = "SDE-ARS-RCP-02"

	// Seed with an explicit status, then upsert again WITHOUT a status and
	// assert the stored status is preserved (not reset to pending).
	for _, want := range []string{"pending", "matched", "conflict"} {
		if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: host, MatchStatus: want}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: host}); err != nil { // blank status
			t.Fatal(err)
		}
		if got := statusOf(t, st, ctx, "t1", host); got != want {
			t.Fatalf("blank update: status = %q, want preserved %q", got, want)
		}
	}
}

func TestDeviceBindingMatchStatusExplicitTransitions(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)
	const host = "SDE-ARS-RCP-02"

	if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: host}); err != nil {
		t.Fatal(err) // starts pending
	}
	// (5) pending -> matched, (6) matched -> conflict, (7) conflict -> pending.
	for _, to := range []string{"matched", "conflict", "pending"} {
		got, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: host, MatchStatus: to})
		if err != nil {
			t.Fatal(err)
		}
		if got.MatchStatus != to {
			t.Fatalf("explicit transition to %q got %q", to, got.MatchStatus)
		}
	}
	// Case-insensitive/normalized input is accepted and stored lower-case.
	got, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: host, MatchStatus: "  MATCHED "})
	if err != nil || got.MatchStatus != "matched" {
		t.Fatalf("normalized status: got %q err %v", got.MatchStatus, err)
	}
}

func TestDeviceBindingEnrichKeepsStatus(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)
	const host = "SDE-ARS-RCP-02"

	if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: host, MatchStatus: "matched"}); err != nil {
		t.Fatal(err)
	}
	// (8) Enrich GLPI/Tactical ids WITHOUT a status: ids fill in, status stays.
	got, err := st.Upsert(ctx, DeviceBinding{
		TenantID: "t1", Hostname: host, GLPIComputerID: "G1", TacticalAgentID: "A1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.GLPIComputerID != "G1" || got.TacticalAgentID != "A1" {
		t.Fatalf("ids not enriched: %+v", got)
	}
	if got.MatchStatus != "matched" {
		t.Fatalf("enrich downgraded status to %q, want matched", got.MatchStatus)
	}
}

func TestDeviceBindingInvalidMatchStatus(t *testing.T) {
	st, ctx := newTestDeviceBindingStore(t)
	// (9) Arbitrary status is rejected, not silently accepted.
	for _, bad := range []string{"open", "done", "matched!", "unknown", "resolved"} {
		if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "SDE-ARS-RCP-02", MatchStatus: bad}); !errors.Is(err, ErrInvalidMatchStatus) {
			t.Fatalf("status %q: expected ErrInvalidMatchStatus, got %v", bad, err)
		}
	}
	// All documented states are accepted.
	for _, ok := range []string{"pending", "matched", "conflict", "missing_glpi", "missing_tactical", "disabled"} {
		if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "SDE-ARS-PRE-01", MatchStatus: ok}); err != nil {
			t.Fatalf("status %q rejected: %v", ok, err)
		}
	}
}

func TestDeviceBindingSchemaInitTwice(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "twice.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// (10) Initializing the schema twice on the same DB must be a no-op.
	if _, err := newDeviceBindingStore(ctx, db); err != nil {
		t.Fatalf("first init: %v", err)
	}
	st, err := newDeviceBindingStore(ctx, db)
	if err != nil {
		t.Fatalf("second init: %v", err)
	}
	if _, err := st.Upsert(ctx, DeviceBinding{TenantID: "t1", Hostname: "SDE-ARS-RCP-02"}); err != nil {
		t.Fatalf("store unusable after double init: %v", err)
	}
}
