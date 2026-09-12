package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wacalls/internal/testdb"
)

func forEachBackend(t *testing.T, fn func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect)) {
	t.Helper()
	backendEnv := strings.ToLower(strings.TrimSpace(os.Getenv("WACALLS_TEST_BACKEND")))
	mariadbDSN := strings.TrimSpace(os.Getenv("WACALLS_TEST_MARIADB_DSN"))

	var backends []testdb.Backend

	switch backendEnv {
	case "sqlite":
		backends = []testdb.Backend{testdb.BackendSQLite}
	case "mariadb":
		if mariadbDSN == "" {
			t.Fatal("WACALLS_TEST_BACKEND is set to mariadb, but WACALLS_TEST_MARIADB_DSN is empty")
		}
		backends = []testdb.Backend{testdb.BackendMariaDB}
	case "all":
		if mariadbDSN == "" {
			t.Fatal("WACALLS_TEST_BACKEND is set to all, but WACALLS_TEST_MARIADB_DSN is empty")
		}
		backends = []testdb.Backend{testdb.BackendSQLite, testdb.BackendMariaDB}
	default:
		backends = append(backends, testdb.BackendSQLite)
		if mariadbDSN != "" {
			backends = append(backends, testdb.BackendMariaDB)
		} else {
			t.Log("Note: MariaDB store contract skipped because WACALLS_TEST_MARIADB_DSN is not set")
		}
	}

	for _, b := range backends {
		dialect := DialectSQLite
		if b == testdb.BackendMariaDB {
			dialect = DialectMariaDB
		}
		t.Run(string(b), func(t *testing.T) {
			tdb := testdb.OpenTestDB(t, b)
			fn(t, tdb, dialect)
		})
	}
}

func sampleCreateInput(tenantID, key, fp string) CreateSupportTicketInput {
	return CreateSupportTicketInput{
		OwnerID:            "user-1",
		TenantID:           tenantID,
		SessionID:          "sess-1",
		ChatJID:            "5511999999999@s.whatsapp.net",
		HostnameInformed:   "SDE-ARS-RCP-02",
		RequesterName:      "Maria Silva",
		Title:              "Impressora offline",
		Description:        "Não imprime desde ontem",
		Priority:           3,
		IdempotencyKey:     key,
		PayloadFingerprint: fp,
		ActorUserID:        "user-1",
	}
}

func TestSupportStore_InitSchemaTwice(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store1, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("first newSupportStore failed: %v", err)
		}

		in := sampleCreateInput("t-init", "idemp-init-key-1", strings.Repeat("a", 64))
		created, err := store1.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		// Re-init schema on the same database
		store2, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("second newSupportStore failed: %v", err)
		}

		got, err := store2.GetByID(ctx, "t-init", created.ID)
		if err != nil {
			t.Fatalf("GetByID failed after second initSchema: %v", err)
		}
		if got.ID != created.ID {
			t.Fatalf("mismatched id after second init: got %q, want %q", got.ID, created.ID)
		}
	})
}

func TestSupportStore_CreateAtomicAndEvents(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		in := sampleCreateInput("t-atomic", "idemp-atomic-123", strings.Repeat("b", 64))
		req, err := store.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		if req.SyncState != StateProcessing {
			t.Fatalf("expected initial sync_state 'processing', got %q", req.SyncState)
		}
		if req.AttemptCount != 1 {
			t.Fatalf("expected attempt_count 1, got %d", req.AttemptCount)
		}
		if len(req.ProcessingToken) != 32 {
			t.Fatalf("expected 32-char processing token, got %q", req.ProcessingToken)
		}
		if req.ProcessingStartedAt <= 0 {
			t.Fatalf("expected processing_started_at > 0, got %d", req.ProcessingStartedAt)
		}
		if req.CreatedAt <= 0 || req.UpdatedAt <= 0 {
			t.Fatalf("expected valid timestamps, got created_at=%d, updated_at=%d", req.CreatedAt, req.UpdatedAt)
		}

		events, err := store.ListEvents(ctx, in.TenantID, req.ID)
		if err != nil {
			t.Fatalf("ListEvents failed: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("expected exactly 2 initial events, got %d", len(events))
		}

		if events[0].Action != ActionCreated {
			t.Errorf("event[0].Action = %q, want %q", events[0].Action, ActionCreated)
		}
		if events[1].Action != ActionTicketClaimed {
			t.Errorf("event[1].Action = %q, want %q", events[1].Action, ActionTicketClaimed)
		}
		for i, ev := range events {
			if ev.ActorType != ActorTypeUser {
				t.Errorf("event[%d].ActorType = %q, want %q", i, ev.ActorType, ActorTypeUser)
			}
			if ev.ActorUserID == nil || *ev.ActorUserID != in.ActorUserID {
				t.Errorf("event[%d].ActorUserID mismatch", i)
			}
			if ev.TenantID != in.TenantID {
				t.Errorf("event[%d].TenantID = %q, want %q", i, ev.TenantID, in.TenantID)
			}
		}
	})
}

func TestSupportStore_RollbackOnCreatedFailure(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		// Inject failure specifically on the first event ('created')
		switch dialect {
		case DialectMariaDB:
			_, err = tdb.DB.ExecContext(ctx, `CREATE TRIGGER trg_fail_created BEFORE INSERT ON support_request_events
				FOR EACH ROW
				BEGIN
					IF NEW.action = 'created' THEN
						SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected failure on created';
					END IF;
				END;`)
		default: // SQLite
			_, err = tdb.DB.ExecContext(ctx, `CREATE TRIGGER trg_fail_created BEFORE INSERT ON support_request_events
				FOR EACH ROW WHEN NEW.action = 'created'
				BEGIN
					SELECT RAISE(ABORT, 'injected failure on created');
				END;`)
		}
		if err != nil {
			t.Fatalf("failed to create fail_created trigger: %v", err)
		}
		defer func() {
			_, _ = tdb.DB.ExecContext(ctx, `DROP TRIGGER IF EXISTS trg_fail_created`)
		}()

		in := sampleCreateInput("t-rb-created", "idemp-rb-created-1", strings.Repeat("c", 64))
		_, err = store.CreateTicketRequest(ctx, in)
		if err == nil {
			t.Fatal("expected CreateTicketRequest to fail on created event")
		}

		// Prove rollback: zero rows in support_requests and zero rows in support_request_events
		var reqCount, evCount int
		if err := tdb.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_requests WHERE tenant_id = ?`, in.TenantID).Scan(&reqCount); err != nil {
			t.Fatalf("scan req count failed: %v", err)
		}
		if reqCount != 0 {
			t.Fatalf("expected 0 requests in table after rollback, found %d", reqCount)
		}
		if err := tdb.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_request_events WHERE tenant_id = ?`, in.TenantID).Scan(&evCount); err != nil {
			t.Fatalf("scan ev count failed: %v", err)
		}
		if evCount != 0 {
			t.Fatalf("expected 0 events in table after rollback, found %d", evCount)
		}
	})
}

func TestSupportStore_RollbackOnTicketClaimedFailure(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		// Inject failure specifically on the second event ('ticket_claimed')
		// This proves that even after 'created' event is successfully inserted into the transaction,
		// failure on 'ticket_claimed' rolls back both the request and the 'created' event.
		switch dialect {
		case DialectMariaDB:
			_, err = tdb.DB.ExecContext(ctx, `CREATE TRIGGER trg_fail_claimed BEFORE INSERT ON support_request_events
				FOR EACH ROW
				BEGIN
					IF NEW.action = 'ticket_claimed' THEN
						SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected failure on ticket_claimed';
					END IF;
				END;`)
		default: // SQLite
			_, err = tdb.DB.ExecContext(ctx, `CREATE TRIGGER trg_fail_claimed BEFORE INSERT ON support_request_events
				FOR EACH ROW WHEN NEW.action = 'ticket_claimed'
				BEGIN
					SELECT RAISE(ABORT, 'injected failure on ticket_claimed');
				END;`)
		}
		if err != nil {
			t.Fatalf("failed to create fail_claimed trigger: %v", err)
		}
		defer func() {
			_, _ = tdb.DB.ExecContext(ctx, `DROP TRIGGER IF EXISTS trg_fail_claimed`)
		}()

		in := sampleCreateInput("t-rb-claimed", "idemp-rb-claimed-1", strings.Repeat("d", 64))
		_, err = store.CreateTicketRequest(ctx, in)
		if err == nil {
			t.Fatal("expected CreateTicketRequest to fail on ticket_claimed event")
		}

		// Prove rollback: zero rows in support_requests and zero rows in support_request_events
		var reqCount, evCount int
		if err := tdb.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_requests WHERE tenant_id = ?`, in.TenantID).Scan(&reqCount); err != nil {
			t.Fatalf("scan req count failed: %v", err)
		}
		if reqCount != 0 {
			t.Fatalf("expected 0 requests in table after rollback, found %d", reqCount)
		}
		if err := tdb.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_request_events WHERE tenant_id = ?`, in.TenantID).Scan(&evCount); err != nil {
			t.Fatalf("scan ev count failed: %v", err)
		}
		if evCount != 0 {
			t.Fatalf("expected 0 events in table after rollback (created event rolled back), found %d", evCount)
		}
	})
}

func TestSupportStore_ConcurrentCreationSameIdempotencyKey(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		// Open a secondary connection pool to test concurrency across separate connections/pools
		secDB := tdb.OpenSecondary(t)
		storeSecondary, err := newSupportStore(ctx, secDB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore on secondary failed: %v", err)
		}

		const (
			numGoroutines = 10
			tenantID      = "t-concurrent"
			idempKey      = "idemp-concurrent-key-1"
		)
		fp := strings.Repeat("d", 64)

		startBarrier := make(chan struct{})
		var wg sync.WaitGroup
		var successCount int64
		var createdIDs = make([]string, numGoroutines)
		var errorsList = make([]error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			idx := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-startBarrier

				s := store
				if idx%2 == 1 {
					s = storeSecondary
				}

				in := sampleCreateInput(tenantID, idempKey, fp)
				req, err := s.CreateTicketRequest(context.Background(), in)
				if err != nil {
					errorsList[idx] = err
				} else {
					atomic.AddInt64(&successCount, 1)
					createdIDs[idx] = req.ID
				}
			}()
		}

		close(startBarrier)
		wg.Wait()

		for i, err := range errorsList {
			if err != nil {
				t.Fatalf("goroutine %d failed with: %v", i, err)
			}
		}

		if successCount != numGoroutines {
			t.Fatalf("expected all %d goroutines to succeed, got %d", numGoroutines, successCount)
		}

		// All goroutines must have resolved to the EXACT same ID
		firstID := createdIDs[0]
		for i, id := range createdIDs {
			if id != firstID {
				t.Fatalf("goroutine %d returned different request ID %q vs %q", i, id, firstID)
			}
		}

		// Exactly ONE row created in database
		var count int
		row := tdb.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_requests WHERE tenant_id = ?`, tenantID)
		if err := row.Scan(&count); err != nil {
			t.Fatalf("scan count failed: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 row in support_requests, found %d", count)
		}

		// Exactly ONE created and ONE ticket_claimed event
		events, err := store.ListEvents(ctx, tenantID, firstID)
		if err != nil {
			t.Fatalf("ListEvents failed: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("expected exactly 2 events, got %d", len(events))
		}
	})
}

func TestSupportStore_IdempotencyKeyReusedDifferentPayload(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		tenantID := "t-reused"
		key := "idemp-key-reused-test"
		fp1 := strings.Repeat("1", 64)
		fp2 := strings.Repeat("2", 64)

		in1 := sampleCreateInput(tenantID, key, fp1)
		req1, err := store.CreateTicketRequest(ctx, in1)
		if err != nil {
			t.Fatalf("first creation failed: %v", err)
		}

		// Same key, same payload -> returns existing
		req1Replay, err := store.CreateTicketRequest(ctx, in1)
		if err != nil {
			t.Fatalf("replay failed: %v", err)
		}
		if req1Replay.ID != req1.ID {
			t.Fatalf("replay returned different ID: got %q, want %q", req1Replay.ID, req1.ID)
		}

		// Same key, DIFFERENT payload -> ErrIdempotencyKeyReused
		in2 := sampleCreateInput(tenantID, key, fp2)
		_, err = store.CreateTicketRequest(ctx, in2)
		if err != ErrIdempotencyKeyReused {
			t.Fatalf("expected ErrIdempotencyKeyReused, got %v", err)
		}
	})
}

func TestSupportStore_ExternalIDUniqueness(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		tenantID := "t-ext-uniq"
		extID := "wacalls-fixed-external-id-12345678"

		in1 := sampleCreateInput(tenantID, "idemp-ext-key-001", strings.Repeat("e", 64))
		in1.ExternalID = extID
		_, err = store.CreateTicketRequest(ctx, in1)
		if err != nil {
			t.Fatalf("first creation failed: %v", err)
		}

		// Second request in same tenant with identical ExternalID
		in2 := sampleCreateInput(tenantID, "idemp-ext-key-002", strings.Repeat("f", 64))
		in2.ExternalID = extID
		_, err = store.CreateTicketRequest(ctx, in2)
		if err == nil {
			t.Fatal("expected duplicate external_id error, got nil")
		}
		if !isUniqueViolation(err) {
			t.Fatalf("expected unique violation error, got: %v", err)
		}
	})
}

func TestSupportStore_SnapshotEnrichment(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		in := sampleCreateInput("t-enrich", "idemp-enrich-key-1", strings.Repeat("0", 64))
		req, err := store.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		compID := "glpi-comp-789"
		devBindingID := "binding-456"

		// Enrichment with invalid token must fail with ErrTokenLost
		err = store.EnrichSnapshot(ctx, EnrichSnapshotInput{
			ID:                    req.ID,
			TenantID:              req.TenantID,
			ProcessingToken:       "wrong-token-1234567890abcdef1234",
			TicketGLPIComputerID:  &compID,
			TicketDeviceBindingID: &devBindingID,
			ActorUserID:           "user-1",
		})
		if err != ErrTokenLost {
			t.Fatalf("expected ErrTokenLost on wrong token, got: %v", err)
		}

		// Enrichment with correct token succeeds
		err = store.EnrichSnapshot(ctx, EnrichSnapshotInput{
			ID:                    req.ID,
			TenantID:              req.TenantID,
			ProcessingToken:       req.ProcessingToken,
			TicketGLPIComputerID:  &compID,
			TicketDeviceBindingID: &devBindingID,
			ActorUserID:           "user-1",
		})
		if err != nil {
			t.Fatalf("EnrichSnapshot failed: %v", err)
		}

		updated, err := store.GetByID(ctx, req.TenantID, req.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if updated.TicketGLPIComputerID == nil || *updated.TicketGLPIComputerID != compID {
			t.Fatalf("expected TicketGLPIComputerID %q, got %v", compID, updated.TicketGLPIComputerID)
		}
		if updated.TicketDeviceBindingID == nil || *updated.TicketDeviceBindingID != devBindingID {
			t.Fatalf("expected TicketDeviceBindingID %q, got %v", devBindingID, updated.TicketDeviceBindingID)
		}

		events, err := store.ListEvents(ctx, req.TenantID, req.ID)
		if err != nil {
			t.Fatalf("ListEvents failed: %v", err)
		}
		if len(events) != 3 {
			t.Fatalf("expected 3 events, got %d", len(events))
		}
		if events[2].Action != ActionSnapshotEnriched {
			t.Fatalf("expected ActionSnapshotEnriched, got %q", events[2].Action)
		}
	})
}

func TestSupportStore_FinishProcessing(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		in := sampleCreateInput("t-finish", "idemp-finish-key-1", strings.Repeat("3", 64))
		req, err := store.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		glpiID := "99"
		glpiHref := "/api.php/v2.3/Assistance/Ticket/99"

		// Finish with wrong token fails
		err = store.FinishProcessing(ctx, FinishProcessingInput{
			ID:              req.ID,
			TenantID:        req.TenantID,
			ProcessingToken: "stale-token-1234567890abcdef1234",
			TargetState:     StateSynced,
			GLPITicketID:    &glpiID,
			GLPITicketHref:  &glpiHref,
			ActorType:       ActorTypeUser,
			ActorUserID:     &in.ActorUserID,
		})
		if err != ErrTokenLost {
			t.Fatalf("expected ErrTokenLost on stale token, got: %v", err)
		}

		// Finish with correct token to StateSynced
		err = store.FinishProcessing(ctx, FinishProcessingInput{
			ID:              req.ID,
			TenantID:        req.TenantID,
			ProcessingToken: req.ProcessingToken,
			TargetState:     StateSynced,
			GLPITicketID:    &glpiID,
			GLPITicketHref:  &glpiHref,
			ActorType:       ActorTypeUser,
			ActorUserID:     &in.ActorUserID,
		})
		if err != nil {
			t.Fatalf("FinishProcessing failed: %v", err)
		}

		finished, err := store.GetByID(ctx, req.TenantID, req.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if finished.SyncState != StateSynced {
			t.Fatalf("expected sync_state %q, got %q", StateSynced, finished.SyncState)
		}
		if finished.ProcessingToken != "" {
			t.Fatalf("expected cleared processing_token, got %q", finished.ProcessingToken)
		}
		if finished.ProcessingStartedAt != 0 {
			t.Fatalf("expected processing_started_at=0, got %d", finished.ProcessingStartedAt)
		}
		if finished.ProcessedAt <= 0 || finished.SyncedAt <= 0 {
			t.Fatalf("expected positive processed_at and synced_at, got processed_at=%d synced_at=%d", finished.ProcessedAt, finished.SyncedAt)
		}
		if finished.GLPITicketID == nil || *finished.GLPITicketID != glpiID {
			t.Fatalf("GLPITicketID mismatch: got %v, want %q", finished.GLPITicketID, glpiID)
		}

		// Double finish attempt must fail because token was cleared
		err = store.FinishProcessing(ctx, FinishProcessingInput{
			ID:              req.ID,
			TenantID:        req.TenantID,
			ProcessingToken: req.ProcessingToken,
			TargetState:     StateSynced,
			GLPITicketID:    &glpiID,
			GLPITicketHref:  &glpiHref,
		})
		if err != ErrTokenLost {
			t.Fatalf("expected ErrTokenLost on double finish, got %v", err)
		}
	})
}

func TestSupportStore_ClaimRetryCAS(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		in := sampleCreateInput("t-retry", "idemp-retry-key-01", strings.Repeat("4", 64))
		req, err := store.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		// Move request to retryable_error
		err = store.FinishProcessing(ctx, FinishProcessingInput{
			ID:              req.ID,
			TenantID:        req.TenantID,
			ProcessingToken: req.ProcessingToken,
			TargetState:     StateRetryableError,
			LastErrorCode:   "integration_unavailable",
			ActorType:       ActorTypeUser,
			ActorUserID:     &in.ActorUserID,
		})
		if err != nil {
			t.Fatalf("FinishProcessing failed: %v", err)
		}

		// Claim retry on retryable_error succeeds
		claimed, err := store.ClaimRetry(ctx, ClaimRetryInput{
			ID:          req.ID,
			TenantID:    req.TenantID,
			ActorUserID: "user-retryer",
		})
		if err != nil {
			t.Fatalf("ClaimRetry failed: %v", err)
		}
		if claimed.SyncState != StateProcessing {
			t.Fatalf("expected StateProcessing after retry claim, got %q", claimed.SyncState)
		}
		if claimed.AttemptCount != 2 {
			t.Fatalf("expected attempt_count=2, got %d", claimed.AttemptCount)
		}
		if len(claimed.ProcessingToken) != 32 || claimed.ProcessingToken == req.ProcessingToken {
			t.Fatalf("expected new 32-char processing token, got %q", claimed.ProcessingToken)
		}

		// Immediate second retry claim while in processing must fail with ErrStateConflict
		_, err = store.ClaimRetry(ctx, ClaimRetryInput{
			ID:          req.ID,
			TenantID:    req.TenantID,
			ActorUserID: "user-competing",
		})
		if err != ErrStateConflict {
			t.Fatalf("expected ErrStateConflict on competing retry claim, got: %v", err)
		}
	})
}

func TestSupportStore_RecoverOrphanedProcessing(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		tenantID := "t-recovery"
		now := time.Now().UTC().Unix()

		// Request 1: Old orphan (started 300s ago)
		in1 := sampleCreateInput(tenantID, "idemp-old-orphan", strings.Repeat("5", 64))
		reqOld, err := store.CreateTicketRequest(ctx, in1)
		if err != nil {
			t.Fatalf("create reqOld failed: %v", err)
		}
		oldTime := now - 300
		_, err = tdb.DB.ExecContext(ctx, `UPDATE support_requests SET processing_started_at = ? WHERE id = ?`, oldTime, reqOld.ID)
		if err != nil {
			t.Fatalf("backdating old request failed: %v", err)
		}

		// Request 2: Recent request (started now)
		in2 := sampleCreateInput(tenantID, "idemp-recent-req", strings.Repeat("6", 64))
		reqRecent, err := store.CreateTicketRequest(ctx, in2)
		if err != nil {
			t.Fatalf("create reqRecent failed: %v", err)
		}

		cutoff := now - 150 // Orphan age cutoff: older than 150s

		recoveredCount, err := store.RecoverOrphanedProcessing(ctx, cutoff, nil)
		if err != nil {
			t.Fatalf("RecoverOrphanedProcessing failed: %v", err)
		}
		if recoveredCount != 1 {
			t.Fatalf("expected exactly 1 recovered request, got %d", recoveredCount)
		}

		// Verify old request is unknown
		oldRecovered, err := store.GetByID(ctx, tenantID, reqOld.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if oldRecovered.SyncState != StateUnknown {
			t.Fatalf("expected old request in StateUnknown, got %q", oldRecovered.SyncState)
		}
		if oldRecovered.LastErrorCode != "orphaned_processing" {
			t.Fatalf("expected last_error_code 'orphaned_processing', got %q", oldRecovered.LastErrorCode)
		}
		if oldRecovered.ProcessingToken != "" {
			t.Fatalf("expected cleared processing_token, got %q", oldRecovered.ProcessingToken)
		}

		// Verify recent request is still processing
		recentGot, err := store.GetByID(ctx, tenantID, reqRecent.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if recentGot.SyncState != StateProcessing {
			t.Fatalf("expected recent request still in StateProcessing, got %q", recentGot.SyncState)
		}

		// Idempotency: calling recovery again recovers 0
		recoveredAgain, err := store.RecoverOrphanedProcessing(ctx, cutoff, nil)
		if err != nil {
			t.Fatalf("second recovery failed: %v", err)
		}
		if recoveredAgain != 0 {
			t.Fatalf("expected 0 recovered on second run, got %d", recoveredAgain)
		}
	})
}

func TestSupportStore_TenantIsolation(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		sharedKey := "shared-idempotency-key"
		fp := strings.Repeat("7", 64)

		inA := sampleCreateInput("tenant-A", sharedKey, fp)
		reqA, err := store.CreateTicketRequest(ctx, inA)
		if err != nil {
			t.Fatalf("tenant-A creation failed: %v", err)
		}

		// Tenant-B can use the identical idempotency key without conflict
		inB := sampleCreateInput("tenant-B", sharedKey, fp)
		reqB, err := store.CreateTicketRequest(ctx, inB)
		if err != nil {
			t.Fatalf("tenant-B creation with same key failed: %v", err)
		}

		if reqA.ID == reqB.ID {
			t.Fatal("tenant-A and tenant-B must have distinct IDs")
		}

		// Tenant-B cannot read tenant-A's request
		_, err = store.GetByID(ctx, "tenant-B", reqA.ID)
		if err != ErrSupportRequestNotFound {
			t.Fatalf("tenant-B reading tenant-A expected ErrSupportRequestNotFound, got %v", err)
		}

		// Tenant-B cannot read tenant-A's events
		eventsAFromB, err := store.ListEvents(ctx, "tenant-B", reqA.ID)
		if err != nil {
			t.Fatalf("ListEvents failed: %v", err)
		}
		if len(eventsAFromB) != 0 {
			t.Fatalf("tenant-B saw tenant-A's events: %d found", len(eventsAFromB))
		}
	})
}

func TestSupportStore_StateNewProhibitedInDDL(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		_, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		// Direct SQL INSERT attempting sync_state = 'new' must be rejected by CHECK constraint directly in the database
		const illegalInsert = `INSERT INTO support_requests (
			id, owner_id, tenant_id, session_id, chat_jid,
			hostname_informed, hostname_normalized, requester_name, title, description,
			priority, external_id, idempotency_key, payload_fingerprint,
			sync_state, created_at, updated_at
		) VALUES (
			'req-illegal-new', 'u-1', 't-illegal', 's-1', 'c-1',
			'HOST', 'HOST', 'Req', 'Title', 'Desc',
			1, 'wacalls-illegal-external-id-12345678901', 'idemp-illegal-1234', '1234567890123456789012345678901234567890123456789012345678901234',
			'new', 1000, 1000
		)`

		_, err = tdb.DB.ExecContext(ctx, illegalInsert)
		if err == nil {
			t.Fatalf("%s DDL CHECK constraint failed to reject sync_state='new'", dialect)
		}
		errLower := strings.ToLower(err.Error())
		if !strings.Contains(errLower, "check constraint") && !strings.Contains(errLower, "constraint") {
			t.Fatalf("expected CHECK constraint failure, got: %v", err)
		}
	})
}

func TestSupportStore_ArbitraryStateProhibitedInDDL(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		_, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		// Direct SQL INSERT attempting arbitrary sync_state must be rejected by CHECK constraint directly in the database
		const illegalInsert = `INSERT INTO support_requests (
			id, owner_id, tenant_id, session_id, chat_jid,
			hostname_informed, hostname_normalized, requester_name, title, description,
			priority, external_id, idempotency_key, payload_fingerprint,
			sync_state, created_at, updated_at
		) VALUES (
			'req-illegal-arb', 'u-1', 't-illegal-arb', 's-1', 'c-1',
			'HOST', 'HOST', 'Req', 'Title', 'Desc',
			1, 'wacalls-illegal-external-id-12345678902', 'idemp-illegal-5678', '1234567890123456789012345678901234567890123456789012345678901234',
			'arbitrary_state', 1000, 1000
		)`

		_, err = tdb.DB.ExecContext(ctx, illegalInsert)
		if err == nil {
			t.Fatalf("%s DDL CHECK constraint failed to reject arbitrary sync_state", dialect)
		}
		errLower := strings.ToLower(err.Error())
		if !strings.Contains(errLower, "check constraint") && !strings.Contains(errLower, "constraint") {
			t.Fatalf("expected CHECK constraint failure, got: %v", err)
		}
	})
}

func TestSupportStore_InvalidActorTypeProhibitedInDDL(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		_, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		// Direct SQL INSERT into support_request_events with invalid actor_type must be rejected by CHECK constraint
		const illegalEventInsert = `INSERT INTO support_request_events (
			id, support_request_id, tenant_id, actor_type, action, created_at
		) VALUES (
			'ev-illegal-actor', 'req-1', 't-illegal-actor', 'invalid_actor', 'created', 1000
		)`

		_, err = tdb.DB.ExecContext(ctx, illegalEventInsert)
		if err == nil {
			t.Fatalf("%s DDL CHECK constraint failed to reject invalid actor_type", dialect)
		}
		errLower := strings.ToLower(err.Error())
		if !strings.Contains(errLower, "check constraint") && !strings.Contains(errLower, "constraint") {
			t.Fatalf("expected CHECK constraint failure, got: %v", err)
		}
	})
}

func TestSupportStore_UpdateLocalDevice(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		in := sampleCreateInput("t-device", "idemp-device-key-1", strings.Repeat("8", 64))
		compID := "glpi-fixed-comp"
		in.TicketGLPIComputerID = &compID
		req, err := store.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		// Initial link
		binding1 := "binding-alpha"
		err = store.UpdateLocalDevice(ctx, UpdateLocalDeviceInput{
			ID:               req.ID,
			TenantID:         req.TenantID,
			DeviceBindingID:  &binding1,
			HostnameInformed: "SDE-ARS-RCP-03",
			ActorUserID:      "user-1",
		})
		if err != nil {
			t.Fatalf("first UpdateLocalDevice failed: %v", err)
		}

		updated, err := store.GetByID(ctx, req.TenantID, req.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if updated.DeviceBindingID == nil || *updated.DeviceBindingID != binding1 {
			t.Fatalf("expected deviceBindingId=%q, got %v", binding1, updated.DeviceBindingID)
		}
		if updated.HostnameNormalized != "SDE-ARS-RCP-03" {
			t.Fatalf("expected normalized hostname SDE-ARS-RCP-03, got %q", updated.HostnameNormalized)
		}
		// Attempt snapshot ticket_* MUST NOT be changed
		if updated.TicketGLPIComputerID == nil || *updated.TicketGLPIComputerID != compID {
			t.Fatalf("TicketGLPIComputerID mutated: got %v, want %q", updated.TicketGLPIComputerID, compID)
		}

		// Replace device
		binding2 := "binding-beta"
		err = store.UpdateLocalDevice(ctx, UpdateLocalDeviceInput{
			ID:               req.ID,
			TenantID:         req.TenantID,
			DeviceBindingID:  &binding2,
			HostnameInformed: "SDE-ARS-RCP-04",
			ActorUserID:      "user-1",
		})
		if err != nil {
			t.Fatalf("second UpdateLocalDevice failed: %v", err)
		}

		events, err := store.ListEvents(ctx, req.TenantID, req.ID)
		if err != nil {
			t.Fatalf("ListEvents failed: %v", err)
		}
		// Expected: created, ticket_claimed, device_linked, device_replaced
		if len(events) != 4 {
			t.Fatalf("expected 4 events, got %d", len(events))
		}
		if events[2].Action != ActionDeviceLinked {
			t.Fatalf("event[2] expected ActionDeviceLinked, got %q", events[2].Action)
		}
		if events[3].Action != ActionDeviceReplaced {
			t.Fatalf("event[3] expected ActionDeviceReplaced, got %q", events[3].Action)
		}
	})
}

func TestSupportStore_Reconcile(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		in := sampleCreateInput("t-reconcile", "idemp-reconcile-1", strings.Repeat("9", 64))
		req, err := store.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		// Attempt reconcile while in processing must fail with ErrStateConflict
		glpiID := "505"
		glpiHref := "/api.php/v2.3/Assistance/Ticket/505"
		err = store.Reconcile(ctx, ReconcileInput{
			ID:             req.ID,
			TenantID:       req.TenantID,
			Outcome:        "synced",
			GLPITicketID:   &glpiID,
			GLPITicketHref: &glpiHref,
			ActorUserID:    "admin-1",
		})
		if err != ErrStateConflict {
			t.Fatalf("expected ErrStateConflict when reconciling processing state, got %v", err)
		}

		// Transition to unknown
		err = store.FinishProcessing(ctx, FinishProcessingInput{
			ID:              req.ID,
			TenantID:        req.TenantID,
			ProcessingToken: req.ProcessingToken,
			TargetState:     StateUnknown,
			LastErrorCode:   "ticket_result_unknown",
			ActorType:       ActorTypeSystem,
		})
		if err != nil {
			t.Fatalf("FinishProcessing failed: %v", err)
		}

		// Reconcile unknown -> synced
		err = store.Reconcile(ctx, ReconcileInput{
			ID:             req.ID,
			TenantID:       req.TenantID,
			Outcome:        "synced",
			GLPITicketID:   &glpiID,
			GLPITicketHref: &glpiHref,
			ActorUserID:    "admin-1",
		})
		if err != nil {
			t.Fatalf("Reconcile to synced failed: %v", err)
		}

		reconciled, err := store.GetByID(ctx, req.TenantID, req.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if reconciled.SyncState != StateSynced {
			t.Fatalf("expected StateSynced, got %q", reconciled.SyncState)
		}
		if reconciled.GLPITicketID == nil || *reconciled.GLPITicketID != glpiID {
			t.Fatalf("GLPITicketID mismatch: got %v, want %q", reconciled.GLPITicketID, glpiID)
		}

		events, err := store.ListEvents(ctx, req.TenantID, req.ID)
		if err != nil {
			t.Fatalf("ListEvents failed: %v", err)
		}
		lastEvent := events[len(events)-1]
		if lastEvent.Action != ActionReconciledSynced {
			t.Fatalf("expected ActionReconciledSynced, got %q", lastEvent.Action)
		}
	})
}

func TestSupportStore_OldTokenRejected(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		in := sampleCreateInput("t-token", "idemp-token-123456", strings.Repeat("e", 64))
		req, err := store.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		oldToken := req.ProcessingToken

		// Enrich with correct token succeeds
		compID := "glpi-comp-100"
		err = store.EnrichSnapshot(ctx, EnrichSnapshotInput{
			ID:                   req.ID,
			TenantID:             in.TenantID,
			ProcessingToken:      oldToken,
			TicketGLPIComputerID: &compID,
			ActorUserID:          in.ActorUserID,
		})
		if err != nil {
			t.Fatalf("EnrichSnapshot with valid token failed: %v", err)
		}

		// Finish with correct token succeeds
		ticketID := "GLPI-100"
		ticketHref := "https://glpi.example.com/tickets/100"
		err = store.FinishProcessing(ctx, FinishProcessingInput{
			ID:              req.ID,
			TenantID:        in.TenantID,
			ProcessingToken: oldToken,
			TargetState:     StateSynced,
			GLPITicketID:    &ticketID,
			GLPITicketHref:  &ticketHref,
		})
		if err != nil {
			t.Fatalf("FinishProcessing with valid token failed: %v", err)
		}

		// Attempting to enrich with the old token now that state is synced must return ErrTokenLost
		err = store.EnrichSnapshot(ctx, EnrichSnapshotInput{
			ID:              req.ID,
			TenantID:        in.TenantID,
			ProcessingToken: oldToken,
		})
		if !errors.Is(err, ErrTokenLost) {
			t.Fatalf("expected ErrTokenLost on enrich with old token, got: %v", err)
		}

		// Attempting to finish again with the old token must return ErrTokenLost
		err = store.FinishProcessing(ctx, FinishProcessingInput{
			ID:              req.ID,
			TenantID:        in.TenantID,
			ProcessingToken: oldToken,
			TargetState:     StateFailed,
		})
		if !errors.Is(err, ErrTokenLost) {
			t.Fatalf("expected ErrTokenLost on finish with old token, got: %v", err)
		}
	})
}

func TestSupportStore_RecoverOrphanedProcessingIdempotent(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		in := sampleCreateInput("t-rec-idem", "idemp-rec-idem-1", strings.Repeat("f", 64))
		req, err := store.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		// Artificially age processing_started_at to 200s ago
		cutoff := time.Now().UTC().Unix() - 100
		agedTime := cutoff - 50
		_, err = tdb.DB.ExecContext(ctx, `UPDATE support_requests SET processing_started_at = ? WHERE id = ?`, agedTime, req.ID)
		if err != nil {
			t.Fatalf("failed to age request: %v", err)
		}

		// First recovery must find and recover 1 request
		n, err := store.RecoverOrphanedProcessing(ctx, cutoff, nil)
		if err != nil {
			t.Fatalf("first RecoverOrphanedProcessing failed: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected 1 recovered request on first pass, got %d", n)
		}

		// Second recovery with the same or later cutoff must be idempotent and recover 0 requests
		n2, err := store.RecoverOrphanedProcessing(ctx, cutoff, nil)
		if err != nil {
			t.Fatalf("second RecoverOrphanedProcessing failed: %v", err)
		}
		if n2 != 0 {
			t.Fatalf("expected 0 recovered requests on second pass (idempotent), got %d", n2)
		}

		// Verify state remains unknown
		after, err := store.GetByID(ctx, in.TenantID, req.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if after.SyncState != StateUnknown {
			t.Fatalf("expected sync_state 'unknown', got %q", after.SyncState)
		}
	})
}

func TestSupportStore_ListByConversationDeterministicTiebreak(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		tenantID := "t-tiebreak"
		sessionID := "sess-tb"
		chatJID := "chat-tb@s.whatsapp.net"
		fixedCreatedAt := int64(1700000000)

		// Insert 3 requests directly with the exact same created_at timestamp
		ids := []string{"req-alpha", "req-charlie", "req-bravo"}
		for _, id := range ids {
			extID := "ext-" + id
			idempKey := "idemp-" + id
			fp := strings.Repeat("9", 64)
			const q = `INSERT INTO support_requests (
				id, owner_id, tenant_id, session_id, chat_jid,
				hostname_informed, hostname_normalized, requester_name, title, description,
				priority, external_id, idempotency_key, payload_fingerprint,
				sync_state, created_at, updated_at
			) VALUES (?, 'u-1', ?, ?, ?, 'HOST', 'HOST', 'User', 'Title', 'Desc', 1, ?, ?, ?, 'synced', ?, ?)`
			if _, err := tdb.DB.ExecContext(ctx, q, id, tenantID, sessionID, chatJID, extID, idempKey, fp, fixedCreatedAt, fixedCreatedAt); err != nil {
				t.Fatalf("failed to insert tiebreak request %s: %v", id, err)
			}
		}

		// Query ListByConversation
		list, err := store.ListByConversation(ctx, tenantID, sessionID, chatJID, 10)
		if err != nil {
			t.Fatalf("ListByConversation failed: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("expected 3 requests, got %d", len(list))
		}

		// With ORDER BY created_at DESC, id DESC:
		// Since created_at is equal, they must be ordered strictly by id DESC:
		// req-charlie > req-bravo > req-alpha
		expectedOrder := []string{"req-charlie", "req-bravo", "req-alpha"}
		for i, expID := range expectedOrder {
			if list[i].ID != expID {
				t.Errorf("list[%d].ID = %q, want %q (deterministic tiebreaker)", i, list[i].ID, expID)
			}
		}
	})
}

func TestSupportStore_ForeignKeyRejectsNonexistentRequest(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		_, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		// Direct INSERT of an event pointing to a nonexistent support_request must fail FK constraint
		const q = `INSERT INTO support_request_events (
			id, support_request_id, tenant_id, actor_type, action, created_at
		) VALUES ('ev-fk-ghost', 'nonexistent-req-id', 't-fk-1', 'user', 'created', 1000)`

		_, err = tdb.DB.ExecContext(ctx, q)
		if err == nil {
			t.Fatalf("%s: expected foreign key constraint error for nonexistent request, got nil", dialect)
		}
		errLower := strings.ToLower(err.Error())
		if !strings.Contains(errLower, "foreign key") && !strings.Contains(errLower, "constraint") {
			t.Fatalf("%s: expected foreign key error, got: %v", dialect, err)
		}
	})
}

func TestSupportStore_ForeignKeyRejectsDivergentTenant(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		// Create a valid request for tenant-A
		in := sampleCreateInput("tenant-A", "idemp-fk-divergent-123", strings.Repeat("1", 64))
		req, err := store.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		// Attempt to insert an event with tenant-B pointing to tenant-A's request ID
		// The composite FK (tenant_id, support_request_id) -> (tenant_id, id) must reject this
		const q = `INSERT INTO support_request_events (
			id, support_request_id, tenant_id, actor_type, action, created_at
		) VALUES ('ev-fk-divergent', ?, 'tenant-B', 'user', 'snapshot_enriched', 1000)`

		_, err = tdb.DB.ExecContext(ctx, q, req.ID)
		if err == nil {
			t.Fatalf("%s: expected foreign key constraint error for divergent tenant, got nil", dialect)
		}
		errLower := strings.ToLower(err.Error())
		if !strings.Contains(errLower, "foreign key") && !strings.Contains(errLower, "constraint") {
			t.Fatalf("%s: expected foreign key error, got: %v", dialect, err)
		}
	})
}

func TestSupportStore_ForeignKeyRejectsDeleteParentWithEvents(t *testing.T) {
	forEachBackend(t, func(t *testing.T, tdb *testdb.TestDB, dialect SQLDialect) {
		ctx := context.Background()
		store, err := newSupportStore(ctx, tdb.DB, dialect)
		if err != nil {
			t.Fatalf("newSupportStore failed: %v", err)
		}

		in := sampleCreateInput("t-fk-del", "idemp-fk-del-12345", strings.Repeat("2", 64))
		req, err := store.CreateTicketRequest(ctx, in)
		if err != nil {
			t.Fatalf("CreateTicketRequest failed: %v", err)
		}

		// Direct DELETE of support_requests must be rejected by ON DELETE RESTRICT
		const q = `DELETE FROM support_requests WHERE id = ? AND tenant_id = ?`
		_, err = tdb.DB.ExecContext(ctx, q, req.ID, in.TenantID)
		if err == nil {
			t.Fatalf("%s: expected ON DELETE RESTRICT error, got nil", dialect)
		}
		errLower := strings.ToLower(err.Error())
		if !strings.Contains(errLower, "foreign key") && !strings.Contains(errLower, "constraint") {
			t.Fatalf("%s: expected foreign key error on delete, got: %v", dialect, err)
		}
	})
}
