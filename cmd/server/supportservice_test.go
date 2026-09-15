package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wacalls/internal/glpi"
	"wacalls/internal/tactical"
	"wacalls/internal/testdb"
)

type mockGLPIClient struct {
	mu                sync.Mutex
	findComputerFn    func(ctx context.Context, hostname string) (glpi.Computer, error)
	getComputerFn     func(ctx context.Context, id string) (glpi.Computer, error)
	createTicketFn    func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error)
	getTicketFn       func(ctx context.Context, id string) (glpi.Ticket, error)
	createTicketCalls []glpi.TicketInput
	getTicketCalls    []string
}

func (m *mockGLPIClient) FindComputerByHostname(ctx context.Context, hostname string) (glpi.Computer, error) {
	m.mu.Lock()
	fn := m.findComputerFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, hostname)
	}
	return glpi.Computer{}, glpi.ErrNotFound
}

func (m *mockGLPIClient) GetComputer(ctx context.Context, id string) (glpi.Computer, error) {
	m.mu.Lock()
	fn := m.getComputerFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, id)
	}
	return glpi.Computer{}, glpi.ErrNotFound
}

func (m *mockGLPIClient) CreateTicket(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
	m.mu.Lock()
	m.createTicketCalls = append(m.createTicketCalls, in)
	fn := m.createTicketFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, in)
	}
	return glpi.CreatedTicket{ID: "100", Href: "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/100"}, nil
}

func (m *mockGLPIClient) GetTicket(ctx context.Context, id string) (glpi.Ticket, error) {
	m.mu.Lock()
	m.getTicketCalls = append(m.getTicketCalls, id)
	fn := m.getTicketFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, id)
	}
	return glpi.Ticket{}, glpi.ErrNotFound
}

type mockTacticalClient struct {
	mu          sync.Mutex
	findAgentFn func(ctx context.Context, hostname string) (tactical.Agent, error)
	getAgentFn  func(ctx context.Context, agentID string) (tactical.Agent, error)
}

func (m *mockTacticalClient) FindAgentByHostname(ctx context.Context, hostname string) (tactical.Agent, error) {
	m.mu.Lock()
	fn := m.findAgentFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, hostname)
	}
	return tactical.Agent{}, errors.New("agent not found")
}

func (m *mockTacticalClient) GetAgent(ctx context.Context, agentID string) (tactical.Agent, error) {
	m.mu.Lock()
	fn := m.getAgentFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, agentID)
	}
	return tactical.Agent{}, errors.New("agent not found")
}

func setupTestSupportService(t *testing.T) (*SupportService, *supportStore, *deviceBindingStore, *mockGLPIClient, *mockTacticalClient, *testdb.TestDB) {
	t.Helper()
	ctx := context.Background()
	tdb := testdb.OpenTestDB(t, testdb.BackendSQLite)

	store, err := newSupportStore(ctx, tdb.DB, DialectSQLite)
	if err != nil {
		t.Fatalf("failed to create support store: %v", err)
	}

	bStore, err := newDeviceBindingStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("failed to create device binding store: %v", err)
	}

	mockG := &mockGLPIClient{}
	mockT := &mockTacticalClient{}

	svc := NewSupportService(store, bStore, mockG, mockT, slog.Default())
	return svc, store, bStore, mockG, mockT, tdb
}

func sampleServiceCreateInput(tenantID, key string) ServiceCreateTicketInput {
	host := "SDE-ARS-RCP-02"
	return ServiceCreateTicketInput{
		OwnerID:        "user-1",
		TenantID:       tenantID,
		SessionID:      "sess-1",
		ChatJID:        "5511999999999@s.whatsapp.net",
		Hostname:       &host,
		RequesterName:  "Maria Silva",
		Title:          "Impressora offline",
		Description:    "Não imprime <script>alert(1)</script>",
		Priority:       3,
		IdempotencyKey: key,
		ActorUserID:    "user-1",
	}
}

func TestSupportService_CreateTicketSuccess(t *testing.T) {
	svc, store, bStore, mockG, mockT, _ := setupTestSupportService(t)
	ctx := context.Background()

	mockG.findComputerFn = func(ctx context.Context, hostname string) (glpi.Computer, error) {
		if strings.EqualFold(hostname, "SDE-ARS-RCP-02") {
			return glpi.Computer{ID: "505", Name: "SDE-ARS-RCP-02"}, nil
		}
		return glpi.Computer{}, glpi.ErrNotFound
	}
	mockT.findAgentFn = func(ctx context.Context, hostname string) (tactical.Agent, error) {
		if strings.EqualFold(hostname, "SDE-ARS-RCP-02") {
			return tactical.Agent{AgentID: "tac-10", Hostname: "SDE-ARS-RCP-02"}, nil
		}
		return tactical.Agent{}, errors.New("not found")
	}
	mockG.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
		return glpi.CreatedTicket{ID: "999", Href: "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/999"}, nil
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-key-12345678")
	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	if res.IsReplay {
		t.Fatalf("expected IsReplay=false, got true")
	}
	if res.Request.SyncState != StateSynced {
		t.Fatalf("expected StateSynced, got %s", res.Request.SyncState)
	}
	if res.Request.ProcessingToken != "" {
		t.Fatalf("expected empty processing token after finish, got %s", res.Request.ProcessingToken)
	}
	if res.Request.GLPITicketID == nil || *res.Request.GLPITicketID != "999" {
		t.Fatalf("expected GLPITicketID '999', got %v", res.Request.GLPITicketID)
	}
	if res.Request.GLPITicketHref == nil || *res.Request.GLPITicketHref != "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/999" {
		t.Fatalf("expected correct href, got %v", res.Request.GLPITicketHref)
	}
	if res.Request.TicketGLPIComputerID == nil || *res.Request.TicketGLPIComputerID != "505" {
		t.Fatalf("expected snapshot computer ID '505', got %v", res.Request.TicketGLPIComputerID)
	}

	// Verify device binding was upserted with matched status
	binding, found, err := bStore.FindByHostname(ctx, "tenant-1", "SDE-ARS-RCP-02")
	if err != nil || !found {
		t.Fatalf("expected device binding to be created, err: %v, found: %v", err, found)
	}
	if binding.GLPIComputerID != "505" || binding.TacticalAgentID != "tac-10" || binding.MatchStatus != "matched" {
		t.Fatalf("expected binding GLPIComputerID=505, TacticalAgentID=tac-10 and match_status=matched, got %+v", binding)
	}

	// Verify HTML escaping and formatting in GLPI call
	if len(mockG.createTicketCalls) != 1 {
		t.Fatalf("expected 1 GLPI CreateTicket call, got %d", len(mockG.createTicketCalls))
	}
	called := mockG.createTicketCalls[0]
	if strings.Contains(called.Content, "<script>") {
		t.Fatalf("raw <script> found in ticket content: %s", called.Content)
	}
	if !strings.Contains(called.Content, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("escaped script tag not found in content: %s", called.Content)
	}
	if called.ExternalID != res.Request.ExternalID {
		t.Fatalf("expected external ID %s, got %s", res.Request.ExternalID, called.ExternalID)
	}

	// Verify audit events recorded
	events, err := store.ListEvents(ctx, "tenant-1", res.Request.ID)
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	expectedEvents := []SupportAction{
		ActionCreated,
		ActionTicketClaimed,
		ActionSnapshotEnriched,
		ActionTicketSynced,
	}
	if len(events) != len(expectedEvents) {
		t.Fatalf("expected %d events, got %d", len(expectedEvents), len(events))
	}
	for i, exp := range expectedEvents {
		if events[i].Action != exp {
			t.Errorf("event %d: expected action %s, got %s", i, exp, events[i].Action)
		}
	}
}

// TestSupportService_TacticalInvalidLastSeenStillMatches guards a general
// defect independently confirmed by code inspection (whether it is what
// produced the T-007 Gate 4 missing_tactical result is unconfirmed — see
// docs/STATUS.md and D-022): a Tactical agent located with an
// unparseable/untrusted last_seen (tactical.Agent.LastSeenValid=false) must
// still count as found. match_status must be "matched" and
// tactical_agent_id must be populated — last_seen is auxiliary telemetry,
// not an identity signal.
func TestSupportService_TacticalInvalidLastSeenStillMatches(t *testing.T) {
	svc, _, bStore, mockG, mockT, _ := setupTestSupportService(t)
	ctx := context.Background()

	mockG.findComputerFn = func(ctx context.Context, hostname string) (glpi.Computer, error) {
		if strings.EqualFold(hostname, "SDE-ARS-RCP-02") {
			return glpi.Computer{ID: "505", Name: "SDE-ARS-RCP-02"}, nil
		}
		return glpi.Computer{}, glpi.ErrNotFound
	}
	mockT.findAgentFn = func(ctx context.Context, hostname string) (tactical.Agent, error) {
		if strings.EqualFold(hostname, "SDE-ARS-RCP-02") {
			// Simulates the fixed tactical client: the agent is still
			// located (no error) even though its last_seen could not be
			// trusted.
			return tactical.Agent{AgentID: "tac-77", Hostname: "SDE-ARS-RCP-02", LastSeenValid: false}, nil
		}
		return tactical.Agent{}, errors.New("not found")
	}
	mockG.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
		return glpi.CreatedTicket{ID: "1000", Href: "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/1000"}, nil
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-key-lastseen-01")
	if _, err := svc.CreateTicket(ctx, input); err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	binding, found, err := bStore.FindByHostname(ctx, "tenant-1", "SDE-ARS-RCP-02")
	if err != nil || !found {
		t.Fatalf("expected device binding to be created, err: %v, found: %v", err, found)
	}
	if binding.MatchStatus != "matched" {
		t.Fatalf("expected match_status=matched despite unparseable last_seen, got %+v", binding)
	}
	if binding.TacticalAgentID != "tac-77" {
		t.Fatalf("expected tactical_agent_id=tac-77 despite unparseable last_seen, got %+v", binding)
	}
	if binding.GLPIComputerID != "505" {
		t.Fatalf("expected glpi_computer_id=505, got %+v", binding)
	}
}

func TestSupportService_ConcurrentCreationSameIdempotencyKey(t *testing.T) {
	svc, _, _, mockG, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	mockG.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
		time.Sleep(30 * time.Millisecond)
		return glpi.CreatedTicket{ID: "concurrent-ticket", Href: "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/concurrent-ticket"}, nil
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-key-concurrent-123")

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)

	results := make([]*ServiceCreateTicketResult, n)
	errs := make([]error, n)

	startBarrier := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startBarrier
			res, err := svc.CreateTicket(ctx, input)
			results[idx] = res
			errs[idx] = err
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	var replayCount int
	var winnerCount int

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d failed with error: %v", i, errs[i])
		}
		if results[i].IsReplay {
			replayCount++
		} else {
			winnerCount++
		}
	}

	if winnerCount != 1 {
		t.Fatalf("expected exactly 1 winner (IsReplay=false), got %d", winnerCount)
	}
	if replayCount != n-1 {
		t.Fatalf("expected %d replays (IsReplay=true), got %d", n-1, replayCount)
	}

	// Exactly 1 GLPI CreateTicket call should have occurred
	mockG.mu.Lock()
	callCount := len(mockG.createTicketCalls)
	mockG.mu.Unlock()

	if callCount != 1 {
		t.Fatalf("expected exactly 1 GLPI CreateTicket call, got %d", callCount)
	}
}

func TestSupportService_ZeroDBTransactionsDuringHTTPCall(t *testing.T) {
	svc, store, _, mockG, _, tdb := setupTestSupportService(t)
	ctx := context.Background()

	httpEntered := make(chan struct{})
	allowHTTPToFinish := make(chan struct{})

	mockG.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
		close(httpEntered)
		<-allowHTTPToFinish
		return glpi.CreatedTicket{ID: "no-tx-ticket", Href: "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/no-tx-ticket"}, nil
	}

	createDone := make(chan struct{})
	go func() {
		defer close(createDone)
		input := sampleServiceCreateInput("tenant-1", "idemp-key-zero-tx-123")
		_, _ = svc.CreateTicket(ctx, input)
	}()

	// Wait until mock GLPI CreateTicket is reached
	select {
	case <-httpEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for HTTP call entry")
	}

	// While GLPI CreateTicket is blocked in flight, verify that we can execute concurrent writes
	// on the SQLite database without lock contention (SQLITE_BUSY).
	testWriteDone := make(chan struct{})
	go func() {
		defer close(testWriteDone)
		_, err := tdb.DB.ExecContext(ctx, `INSERT INTO support_requests (
			id, owner_id, tenant_id, session_id, chat_jid, requester_name, hostname_informed,
			hostname_normalized, title, description, priority, idempotency_key,
			payload_fingerprint, external_id, sync_state, processing_token,
			created_at, updated_at
		) VALUES (
			'req-concurrent-tx', 'user-1', 'tenant-1', 'sess-c', 'jid@s.whatsapp.net', 'Maria',
			'host', 'host', 'concurrent write', 'test', 3, 'idemp-concurrent-write',
			'fp-concurrent', 'ext-concurrent', 'synced', '', 100, 100
		)`)
		if err != nil {
			t.Errorf("concurrent write to db failed while HTTP call in flight: %v", err)
		}
	}()

	select {
	case <-testWriteDone:
		// Succeeded promptly, proving DB was not locked in a transaction by CreateTicket
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent DB write blocked or timed out; transaction was held during HTTP call")
	}

	// Verify we can read the row created by CreateTicket
	req, err := store.GetByIdempotencyKey(ctx, "tenant-1", "idemp-key-zero-tx-123")
	if err != nil {
		t.Fatalf("failed to query support request while HTTP in flight: %v", err)
	}
	if req.SyncState != StateProcessing {
		t.Fatalf("expected state processing while HTTP call is in flight, got %s", req.SyncState)
	}

	// Now unblock the HTTP call and wait for CreateTicket to complete
	close(allowHTTPToFinish)
	<-createDone

	// Verify final state is synced
	reqFinal, err := store.GetByIdempotencyKey(ctx, "tenant-1", "idemp-key-zero-tx-123")
	if err != nil {
		t.Fatalf("failed to query final request: %v", err)
	}
	if reqFinal.SyncState != StateSynced {
		t.Fatalf("expected final state synced, got %s", reqFinal.SyncState)
	}
}

func TestSupportService_FailureClassification(t *testing.T) {
	cases := []struct {
		name          string
		glpiErr       error
		expectedState SupportSyncState
		expectedCode  string
	}{
		{
			name: "token auth failure",
			glpiErr: &glpi.Error{
				Op:   "token",
				Kind: glpi.ErrAuth,
			},
			expectedState: StateFailed,
			expectedCode:  "integration_auth_failed",
		},
		{
			name: "token unavailable",
			glpiErr: &glpi.Error{
				Op:   "token",
				Kind: glpi.ErrUnavailable,
			},
			expectedState: StateRetryableError,
			expectedCode:  "integration_unavailable",
		},
		{
			name: "post 429 rate limited",
			glpiErr: &glpi.Error{
				Op:         "create ticket",
				StatusCode: 429,
			},
			expectedState: StateRetryableError,
			expectedCode:  "rate_limited",
		},
		{
			name: "post 401 auth failed",
			glpiErr: &glpi.Error{
				Op:         "create ticket",
				StatusCode: 401,
			},
			expectedState: StateRetryableError,
			expectedCode:  "integration_auth_failed",
		},
		{
			name: "post 400 rejected",
			glpiErr: &glpi.Error{
				Op:         "create ticket",
				StatusCode: 400,
			},
			expectedState: StateFailed,
			expectedCode:  "ticket_rejected",
		},
		{
			name: "post 403 rejected",
			glpiErr: &glpi.Error{
				Op:         "create ticket",
				StatusCode: 403,
			},
			expectedState: StateFailed,
			expectedCode:  "ticket_rejected",
		},
		{
			name: "post 404 rejected",
			glpiErr: &glpi.Error{
				Op:         "create ticket",
				StatusCode: 404,
			},
			expectedState: StateFailed,
			expectedCode:  "ticket_rejected",
		},
		{
			name: "post 409 rejected",
			glpiErr: &glpi.Error{
				Op:         "create ticket",
				StatusCode: 409,
			},
			expectedState: StateFailed,
			expectedCode:  "ticket_rejected",
		},
		{
			name: "post 422 rejected",
			glpiErr: &glpi.Error{
				Op:         "create ticket",
				StatusCode: 422,
			},
			expectedState: StateFailed,
			expectedCode:  "ticket_rejected",
		},
		{
			name: "post 500 unknown",
			glpiErr: &glpi.Error{
				Op:         "create ticket",
				StatusCode: 500,
			},
			expectedState: StateUnknown,
			expectedCode:  "ticket_result_unknown",
		},
		{
			name: "post 503 unknown",
			glpiErr: &glpi.Error{
				Op:         "create ticket",
				StatusCode: 503,
			},
			expectedState: StateUnknown,
			expectedCode:  "ticket_result_unknown",
		},
		{
			name:          "generic network error unknown",
			glpiErr:       errors.New("connection reset by peer"),
			expectedState: StateUnknown,
			expectedCode:  "ticket_result_unknown",
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, store, _, mockG, _, _ := setupTestSupportService(t)
			ctx := context.Background()

			mockG.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
				return glpi.CreatedTicket{}, tc.glpiErr
			}

			key := fmt.Sprintf("idemp-fail-class-%d-abcdef", i)
			input := sampleServiceCreateInput("tenant-1", key)

			res, err := svc.CreateTicket(ctx, input)
			if err == nil {
				t.Fatalf("expected error from CreateTicket, got nil")
			}

			if res.Request.SyncState != tc.expectedState {
				t.Fatalf("expected SyncState %s, got %s", tc.expectedState, res.Request.SyncState)
			}
			if res.Request.LastErrorCode != tc.expectedCode {
				t.Fatalf("expected LastErrorCode %s, got %s", tc.expectedCode, res.Request.LastErrorCode)
			}
			if res.Request.ProcessingToken != "" {
				t.Fatalf("expected empty processing token, got %s", res.Request.ProcessingToken)
			}

			// Verify in DB
			dbReq, err := store.GetByIdempotencyKey(ctx, "tenant-1", key)
			if err != nil {
				t.Fatalf("failed to fetch from store: %v", err)
			}
			if dbReq.SyncState != tc.expectedState {
				t.Fatalf("DB state expected %s, got %s", tc.expectedState, dbReq.SyncState)
			}
			if dbReq.LastErrorCode != tc.expectedCode {
				t.Fatalf("DB last_error_code expected %s, got %s", tc.expectedCode, dbReq.LastErrorCode)
			}
		})
	}
}

func TestSupportService_Retry(t *testing.T) {
	svc, store, _, mockG, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	// 1. Initial creation fails with 429 -> retryable_error
	mockG.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
		return glpi.CreatedTicket{}, &glpi.Error{Op: "create ticket", StatusCode: 429}
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-key-retry-12345")
	res, err := svc.CreateTicket(ctx, input)
	if err == nil {
		t.Fatalf("expected 429 error, got nil")
	}
	if res.Request.SyncState != StateRetryableError {
		t.Fatalf("expected retryable_error, got %s", res.Request.SyncState)
	}

	reqID := res.Request.ID

	// 2. Successful retry: GLPI now returns 200
	mockG.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
		return glpi.CreatedTicket{ID: "77", Href: "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/77"}, nil
	}

	retryReq, err := svc.Retry(ctx, ServiceRetryInput{
		ID:          reqID,
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("Retry failed: %v", err)
	}

	if retryReq.SyncState != StateSynced {
		t.Fatalf("expected synced, got %s", retryReq.SyncState)
	}
	if retryReq.GLPITicketID == nil || *retryReq.GLPITicketID != "77" {
		t.Fatalf("expected GLPITicketID '77', got %v", retryReq.GLPITicketID)
	}

	// Verify events in store
	events, err := store.ListEvents(ctx, "tenant-1", reqID)
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	hasTicketClaimed := false
	hasTicketSynced := false
	for _, ev := range events {
		if ev.Action == ActionTicketClaimed {
			hasTicketClaimed = true
		}
		if ev.Action == ActionTicketSynced {
			hasTicketSynced = true
		}
	}
	if !hasTicketClaimed || !hasTicketSynced {
		t.Fatalf("missing ticket_claimed or ticket_synced events, got: %+v", events)
	}

	// 3. Competing retry on already synced request returns ErrStateConflict
	_, err = svc.Retry(ctx, ServiceRetryInput{
		ID:          reqID,
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
	})
	if !errors.Is(err, ErrStateConflict) {
		t.Fatalf("expected ErrStateConflict on synced request, got %v", err)
	}
}

func TestSupportService_RetryConcurrency(t *testing.T) {
	svc, _, _, mockG, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	// Initial creation fails with 429 -> retryable_error
	mockG.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
		return glpi.CreatedTicket{}, &glpi.Error{Op: "create ticket", StatusCode: 429}
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-key-retry-race-123")
	res, _ := svc.CreateTicket(ctx, input)
	reqID := res.Request.ID

	// Now configure mock to succeed with a short sleep
	mockG.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
		time.Sleep(20 * time.Millisecond)
		return glpi.CreatedTicket{ID: "race-ticket-88", Href: "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/88"}, nil
	}

	var wg sync.WaitGroup
	var successCount int32
	var conflictCount int32

	startBarrier := make(chan struct{})
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier
			_, err := svc.Retry(ctx, ServiceRetryInput{
				ID:          reqID,
				TenantID:    "tenant-1",
				ActorUserID: "user-1",
			})
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			} else if errors.Is(err, ErrStateConflict) {
				atomic.AddInt32(&conflictCount, 1)
			}
		}()
	}

	close(startBarrier)
	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", successCount)
	}
	if conflictCount != 4 {
		t.Fatalf("expected 4 state conflicts, got %d", conflictCount)
	}
}

func TestSupportService_Reconcile(t *testing.T) {
	svc, store, _, mockG, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	// Create request that ends in unknown
	mockG.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
		return glpi.CreatedTicket{}, &glpi.Error{Op: "create ticket", StatusCode: 500}
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-reconcile-12345")
	res, _ := svc.CreateTicket(ctx, input)
	reqID := res.Request.ID
	extID := res.Request.ExternalID

	t.Run("synced external_id mismatch", func(t *testing.T) {
		mockG.getTicketFn = func(ctx context.Context, id string) (glpi.Ticket, error) {
			return glpi.Ticket{ID: "999", ExternalID: "mismatched-external-id", Href: "https://glpi/999"}, nil
		}
		ticketID := "999"
		_, err := svc.Reconcile(ctx, ServiceReconcileInput{
			ID:           reqID,
			TenantID:     "tenant-1",
			Outcome:      "synced",
			GLPITicketID: &ticketID,
			ActorUserID:  "admin-1",
		})
		if !errors.Is(err, ErrReconcileExternalIDMismatch) {
			t.Fatalf("expected ErrReconcileExternalIDMismatch, got %v", err)
		}
	})

	t.Run("synced ticket not found", func(t *testing.T) {
		mockG.getTicketFn = func(ctx context.Context, id string) (glpi.Ticket, error) {
			return glpi.Ticket{}, glpi.ErrNotFound
		}
		ticketID := "999"
		_, err := svc.Reconcile(ctx, ServiceReconcileInput{
			ID:           reqID,
			TenantID:     "tenant-1",
			Outcome:      "synced",
			GLPITicketID: &ticketID,
			ActorUserID:  "admin-1",
		})
		if !errors.Is(err, ErrTicketNotFound) {
			t.Fatalf("expected ErrTicketNotFound, got %v", err)
		}
	})

	t.Run("synced success", func(t *testing.T) {
		mockG.getTicketFn = func(ctx context.Context, id string) (glpi.Ticket, error) {
			return glpi.Ticket{ID: "999", ExternalID: extID, Href: "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/999"}, nil
		}
		ticketID := "999"
		reconciled, err := svc.Reconcile(ctx, ServiceReconcileInput{
			ID:           reqID,
			TenantID:     "tenant-1",
			Outcome:      "synced",
			GLPITicketID: &ticketID,
			ActorUserID:  "admin-1",
		})
		if err != nil {
			t.Fatalf("Reconcile synced failed: %v", err)
		}
		if reconciled.SyncState != StateSynced {
			t.Fatalf("expected synced, got %s", reconciled.SyncState)
		}
		if reconciled.GLPITicketID == nil || *reconciled.GLPITicketID != "999" {
			t.Fatalf("expected GLPITicketID '999', got %v", reconciled.GLPITicketID)
		}

		// Verify event in store
		events, err := store.ListEvents(ctx, "tenant-1", reqID)
		if err != nil {
			t.Fatalf("failed to list events: %v", err)
		}
		var foundReconciled bool
		for _, ev := range events {
			if ev.Action == ActionReconciledSynced {
				foundReconciled = true
			}
		}
		if !foundReconciled {
			t.Fatalf("expected reconciled_synced event in audit log")
		}
	})

	t.Run("safe_to_retry", func(t *testing.T) {
		// Create another request ending in unknown
		input2 := sampleServiceCreateInput("tenant-1", "idemp-safe-retry-12345")
		res2, _ := svc.CreateTicket(ctx, input2)

		reconciled, err := svc.Reconcile(ctx, ServiceReconcileInput{
			ID:          res2.Request.ID,
			TenantID:    "tenant-1",
			Outcome:     "safe_to_retry",
			ActorUserID: "admin-1",
		})
		if err != nil {
			t.Fatalf("Reconcile safe_to_retry failed: %v", err)
		}
		if reconciled.SyncState != StateRetryableError {
			t.Fatalf("expected retryable_error, got %s", reconciled.SyncState)
		}
	})

	t.Run("processing_orphaned", func(t *testing.T) {
		// Insert an orphaned processing request with processing_started_at in the past
		past := time.Now().UTC().Add(-600 * time.Second).Unix()
		token, _ := GenerateProcessingToken()
		hostVal := "host-orph"
		fp, _ := CalculatePayloadFingerprintV2(CanonicalPayloadV2{
			V:             2,
			TenantID:      "tenant-1",
			SessionID:     "sess-orph",
			ChatJID:       "jid@s.whatsapp.net",
			Hostname:      OptionalField[string]{Present: true, Value: hostVal},
			RequesterName: "Test",
			Title:         "Orphaned",
			Priority:      OptionalField[int]{Present: true, Value: 3},
		})

		created, err := store.CreateTicketRequest(ctx, CreateSupportTicketInput{
			OwnerID:            "user-1",
			TenantID:           "tenant-1",
			SessionID:          "sess-orph",
			ChatJID:            "jid@s.whatsapp.net",
			HostnameInformed:   hostVal,
			RequesterName:      "Test",
			Title:              "Orphaned",
			Priority:           3,
			IdempotencyKey:     "idemp-orphaned-12345",
			PayloadFingerprint: fp,
			ProcessingToken:    token,
			ActorUserID:        "user-1",
		})
		if err != nil {
			t.Fatalf("failed to insert orphan req: %v", err)
		}

		// Update processing_started_at directly into the past
		_, err = store.db.ExecContext(ctx, `UPDATE support_requests SET processing_started_at = ? WHERE id = ?`, past, created.ID)
		if err != nil {
			t.Fatalf("failed to backdate processing_started_at: %v", err)
		}

		cutoff := time.Now().UTC().Add(-300 * time.Second).Unix()
		reconciled, err := svc.Reconcile(ctx, ServiceReconcileInput{
			ID:          created.ID,
			TenantID:    "tenant-1",
			Outcome:     "processing_orphaned",
			Cutoff:      cutoff,
			ActorUserID: "admin-1",
		})
		if err != nil {
			t.Fatalf("Reconcile processing_orphaned failed: %v", err)
		}
		if reconciled.SyncState != StateUnknown {
			t.Fatalf("expected state unknown, got %s", reconciled.SyncState)
		}
	})

	t.Run("invalid outcome returns error", func(t *testing.T) {
		_, err := svc.Reconcile(ctx, ServiceReconcileInput{
			ID:          reqID,
			TenantID:    "tenant-1",
			Outcome:     "nonexistent_outcome",
			ActorUserID: "admin-1",
		})
		if !errors.Is(err, ErrInvalidReconcileOutcome) {
			t.Fatalf("expected ErrInvalidReconcileOutcome, got %v", err)
		}
	})
}

func TestSupportService_UpdateDevice(t *testing.T) {
	svc, store, bStore, _, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	// Pre-create two device bindings
	b1, err := bStore.Upsert(ctx, DeviceBinding{
		OwnerID:  "user-1",
		TenantID: "tenant-1",
		Hostname: "INITIAL-PC",
	})
	if err != nil {
		t.Fatalf("failed to create binding 1: %v", err)
	}
	b2, err := bStore.Upsert(ctx, DeviceBinding{
		OwnerID:  "user-1",
		TenantID: "tenant-1",
		Hostname: "REPLACEMENT-PC",
	})
	if err != nil {
		t.Fatalf("failed to create binding 2: %v", err)
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-update-dev-12345")
	input.DeviceBindingID = &b1.ID
	input.Hostname = &b1.Hostname

	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	// Update device to b2
	updatedReq, glpiContextUpdated, err := svc.UpdateDevice(ctx, ServiceUpdateDeviceInput{
		ID:              res.Request.ID,
		TenantID:        "tenant-1",
		DeviceBindingID: b2.ID,
		ActorUserID:     "user-1",
	})
	if err != nil {
		t.Fatalf("UpdateDevice failed: %v", err)
	}

	// Must return glpiContextUpdated = false
	if glpiContextUpdated {
		t.Fatalf("expected glpiContextUpdated=false, got true")
	}

	// Local device updated
	if updatedReq.DeviceBindingID == nil || *updatedReq.DeviceBindingID != b2.ID {
		t.Fatalf("expected updated DeviceBindingID %s, got %v", b2.ID, updatedReq.DeviceBindingID)
	}
	if updatedReq.HostnameInformed != "REPLACEMENT-PC" {
		t.Fatalf("expected HostnameInformed REPLACEMENT-PC, got %s", updatedReq.HostnameInformed)
	}

	// Snapshot must remain frozen as INITIAL-PC / b1
	dbReq, err := store.GetByID(ctx, "tenant-1", res.Request.ID)
	if err != nil {
		t.Fatalf("failed to query DB: %v", err)
	}
	if dbReq.TicketDeviceBindingID == nil || *dbReq.TicketDeviceBindingID != b1.ID {
		t.Fatalf("frozen snapshot TicketDeviceBindingID changed! expected %s, got %v", b1.ID, dbReq.TicketDeviceBindingID)
	}
	if dbReq.TicketHostnameInformed == nil || *dbReq.TicketHostnameInformed != "INITIAL-PC" {
		t.Fatalf("frozen snapshot TicketHostnameInformed changed! expected INITIAL-PC, got %v", dbReq.TicketHostnameInformed)
	}

	// Updating with cross-tenant binding is rejected
	crossTenantBinding, err := bStore.Upsert(ctx, DeviceBinding{
		OwnerID:  "user-2",
		TenantID: "tenant-2",
		Hostname: "OTHER-TENANT-PC",
	})
	if err != nil {
		t.Fatalf("failed to create cross-tenant binding: %v", err)
	}

	_, _, err = svc.UpdateDevice(ctx, ServiceUpdateDeviceInput{
		ID:              res.Request.ID,
		TenantID:        "tenant-1",
		DeviceBindingID: crossTenantBinding.ID,
		ActorUserID:     "user-1",
	})
	if !errors.Is(err, ErrDeviceBindingNotFound) {
		t.Fatalf("expected ErrDeviceBindingNotFound for cross-tenant binding, got %v", err)
	}
}

func TestSupportService_TenantIsolation(t *testing.T) {
	svc, _, _, _, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	input := sampleServiceCreateInput("tenant-A", "idemp-tenant-iso-12345")
	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}
	reqID := res.Request.ID

	// Tenant-B cannot get by ID
	_, err = svc.GetByID(ctx, "tenant-B", reqID)
	if !errors.Is(err, ErrSupportRequestNotFound) {
		t.Fatalf("expected ErrSupportRequestNotFound for cross-tenant GetByID, got %v", err)
	}

	// Tenant-B cannot retry
	_, err = svc.Retry(ctx, ServiceRetryInput{
		ID:          reqID,
		TenantID:    "tenant-B",
		ActorUserID: "user-b",
	})
	if !errors.Is(err, ErrSupportRequestNotFound) {
		t.Fatalf("expected ErrSupportRequestNotFound for cross-tenant Retry, got %v", err)
	}

	// Tenant-B cannot reconcile
	_, err = svc.Reconcile(ctx, ServiceReconcileInput{
		ID:          reqID,
		TenantID:    "tenant-B",
		Outcome:     "safe_to_retry",
		ActorUserID: "admin-b",
	})
	if !errors.Is(err, ErrSupportRequestNotFound) {
		t.Fatalf("expected ErrSupportRequestNotFound for cross-tenant Reconcile, got %v", err)
	}

	// Tenant-B cannot list conversation
	list, err := svc.ListByConversation(ctx, "tenant-B", "sess-1", "5511999999999@s.whatsapp.net", 50)
	if err != nil {
		t.Fatalf("ListByConversation failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 requests for cross-tenant list, got %d", len(list))
	}
}

type bindingSpyBackend struct {
	getForTenantCalls   []struct{ tenantID, id string }
	findByHostnameCalls []struct{ tenantID, host string }
	upsertCalls         []DeviceBinding
	searchCalls         []struct{ tenantID, q string }
	getForTenantFn      func(ctx context.Context, tenantID, id string) (DeviceBinding, error)
}

func (s *bindingSpyBackend) GetForTenant(ctx context.Context, tenantID, id string) (DeviceBinding, error) {
	s.getForTenantCalls = append(s.getForTenantCalls, struct{ tenantID, id string }{tenantID, id})
	if s.getForTenantFn != nil {
		return s.getForTenantFn(ctx, tenantID, id)
	}
	return DeviceBinding{}, ErrDeviceBindingNotFound
}

func (s *bindingSpyBackend) FindByHostname(ctx context.Context, tenantID, hostname string) (DeviceBinding, bool, error) {
	s.findByHostnameCalls = append(s.findByHostnameCalls, struct{ tenantID, host string }{tenantID, hostname})
	return DeviceBinding{}, false, nil
}

func (s *bindingSpyBackend) Upsert(ctx context.Context, b DeviceBinding) (DeviceBinding, error) {
	s.upsertCalls = append(s.upsertCalls, b)
	return b, nil
}

func (s *bindingSpyBackend) Search(ctx context.Context, tenantID, query string) ([]DeviceBinding, error) {
	s.searchCalls = append(s.searchCalls, struct{ tenantID, q string }{tenantID, query})
	return nil, nil
}

func TestSupportService_CreateTicket_CrossTenantDeviceBindingRejected(t *testing.T) {
	svc, _, bStore, mockG, _, tdb := setupTestSupportService(t)
	ctx := context.Background()

	// 1. Create a binding belonging to tenant-B
	bTenantB, err := bStore.Upsert(ctx, DeviceBinding{
		OwnerID:  "user-b",
		TenantID: "tenant-B",
		Hostname: "HOST-TENANT-B",
	})
	if err != nil {
		t.Fatalf("failed to create tenant-B binding: %v", err)
	}

	// 2. Attempt to create ticket in tenant-A pointing to tenant-B's binding
	input := sampleServiceCreateInput("tenant-A", "idemp-crosstenant-dev-12345")
	input.DeviceBindingID = &bTenantB.ID
	h := "HOST-TENANT-B"
	input.Hostname = &h

	res, err := svc.CreateTicket(ctx, input)
	if !errors.Is(err, ErrDeviceBindingNotFound) {
		t.Fatalf("expected ErrDeviceBindingNotFound, got err=%v, res=%+v", err, res)
	}

	// 3. Verify zero calls to GLPI
	if len(mockG.createTicketCalls) != 0 {
		t.Fatalf("expected 0 GLPI CreateTicket calls, got %d", len(mockG.createTicketCalls))
	}

	// 4. Verify no support request was created in DB
	var countReq int
	if err := tdb.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_requests WHERE tenant_id = 'tenant-A'`).Scan(&countReq); err != nil {
		t.Fatalf("failed to query support_requests: %v", err)
	}
	if countReq != 0 {
		t.Fatalf("expected 0 support_requests in DB, got %d", countReq)
	}

	// 5. Verify no support request events were created in DB
	var countEvents int
	if err := tdb.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_request_events WHERE tenant_id = 'tenant-A'`).Scan(&countEvents); err != nil {
		t.Fatalf("failed to query support_request_events: %v", err)
	}
	if countEvents != 0 {
		t.Fatalf("expected 0 support_request_events in DB, got %d", countEvents)
	}

	// 6. Test with a spy backend that only implements deviceBindingStoreBackend (no Get method)
	spy := &bindingSpyBackend{}
	svcWithSpy := NewSupportService(svc.store, spy, mockG, nil, slog.Default())
	bID := "dev-binding-spy-1"
	spyInput := sampleServiceCreateInput("tenant-A", "idemp-spy-dev-1234567")
	spyInput.DeviceBindingID = &bID

	_, spyErr := svcWithSpy.CreateTicket(ctx, spyInput)
	if !errors.Is(spyErr, ErrDeviceBindingNotFound) {
		t.Fatalf("expected ErrDeviceBindingNotFound with spy, got %v", spyErr)
	}
	if len(spy.getForTenantCalls) != 1 {
		t.Fatalf("expected exactly 1 call to GetForTenant, got %d", len(spy.getForTenantCalls))
	}
	if spy.getForTenantCalls[0].tenantID != "tenant-A" || spy.getForTenantCalls[0].id != bID {
		t.Fatalf("unexpected GetForTenant args: %+v", spy.getForTenantCalls[0])
	}
}

func TestSupportService_InputValidation(t *testing.T) {
	svc, _, _, _, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	valid := sampleServiceCreateInput("tenant-1", "idemp-val-12345678")

	tests := []struct {
		name    string
		modify  func(*ServiceCreateTicketInput)
		wantErr error
	}{
		{
			name:    "missing tenant ID",
			modify:  func(in *ServiceCreateTicketInput) { in.TenantID = "" },
			wantErr: ErrMissingTenantID,
		},
		{
			name:    "missing session ID",
			modify:  func(in *ServiceCreateTicketInput) { in.SessionID = "" },
			wantErr: ErrMissingSessionOrChat,
		},
		{
			name:    "missing chat JID",
			modify:  func(in *ServiceCreateTicketInput) { in.ChatJID = "" },
			wantErr: ErrMissingSessionOrChat,
		},
		{
			name:    "missing title",
			modify:  func(in *ServiceCreateTicketInput) { in.Title = "   " },
			wantErr: ErrMissingRequesterOrTitle,
		},
		{
			name:    "missing requester name",
			modify:  func(in *ServiceCreateTicketInput) { in.RequesterName = "" },
			wantErr: ErrMissingRequesterOrTitle,
		},
		{
			name:    "priority below 0",
			modify:  func(in *ServiceCreateTicketInput) { in.Priority = -1 },
			wantErr: ErrInvalidPriority,
		},
		{
			name:    "priority above 6",
			modify:  func(in *ServiceCreateTicketInput) { in.Priority = 7 },
			wantErr: ErrInvalidPriority,
		},
		{
			name:    "missing idempotency key",
			modify:  func(in *ServiceCreateTicketInput) { in.IdempotencyKey = "" },
			wantErr: ErrInvalidIdempotencyKey,
		},
		{
			name:    "short idempotency key",
			modify:  func(in *ServiceCreateTicketInput) { in.IdempotencyKey = "short" },
			wantErr: ErrInvalidIdempotencyKey,
		},
		{
			name:    "invalid characters in idempotency key",
			modify:  func(in *ServiceCreateTicketInput) { in.IdempotencyKey = "key with spaces 12345" },
			wantErr: ErrInvalidIdempotencyKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.modify(&in)
			_, err := svc.CreateTicket(ctx, in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
		})
	}
}
