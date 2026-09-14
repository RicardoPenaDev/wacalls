package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"wacalls/internal/glpi"
	"wacalls/internal/tactical"
	"wacalls/internal/testdb"
)

type testAPISetup struct {
	srv          *server
	db           *sql.DB
	mockGLPI     *mockGLPIClient
	mockTactical *mockTacticalClient
	store        *supportStore
	bStore       *deviceBindingStore
	user1Token   string
	user1ID      string
	admin1Token  string
	admin1ID     string
	subuserToken string
	subuserID    string
	user2Token   string
	user2ID      string
	sess1ID      string
	sess2ID      string
	chat1JID     string
	chat2JID     string
}

func setupSupportAPITest(t *testing.T, supportEnabled bool, tacticalConfigured bool) *testAPISetup {
	t.Helper()
	ctx := context.Background()
	tdb := testdb.OpenTestDB(t, testdb.BackendSQLite)

	auth, err := newAuthStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}
	sessStore, err := newSessionStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}
	chatMeta, err := newChatMetaStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("failed to create chat meta store: %v", err)
	}
	bStore, err := newDeviceBindingStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("failed to create device binding store: %v", err)
	}
	sStore, err := newSupportStore(ctx, tdb.DB, DialectSQLite)
	if err != nil {
		t.Fatalf("failed to create support store: %v", err)
	}
	settings, err := newSettingsStore(ctx, tdb.DB)
	if err != nil {
		t.Fatalf("failed to create settings store: %v", err)
	}

	mockG := &mockGLPIClient{}
	mockT := &mockTacticalClient{}

	var supSvc supportServiceAPI
	var glpiCli supportGLPIClient
	var tacticalCli supportTacticalClient

	if supportEnabled {
		glpiCli = mockG
		if tacticalConfigured {
			tacticalCli = mockT
		}
		supSvc = NewSupportService(sStore, bStore, glpiCli, tacticalCli)
	}

	container := sqlstore.NewWithDB(tdb.DB, "sqlite", waLog.Noop)
	_ = container.Upgrade(ctx)

	mgr := newSessionManager(ctx, container, NewBroker(), sessStore, waLog.Noop, slog.Default(), 0)
	mgr.UserTenantFn = func(userID string) string {
		if userID == "" {
			return ""
		}
		pid, err := auth.ParentOf(ctx, userID)
		if err == nil && pid != "" {
			return pid
		}
		if err == nil {
			return userID
		}
		return ""
	}
	mgr.IsAdminRoleFn = func(userID string) bool {
		if userID == "" {
			return false
		}
		ok, err := auth.HasRole(ctx, userID, RoleAdmin)
		if err != nil {
			return false
		}
		return ok
	}
	mgr.UserSessionsFn = func(userID string) []string {
		if userID == "" {
			return nil
		}
		ids, err := auth.SessionsFor(ctx, userID)
		if err != nil {
			return nil
		}
		return ids
	}

	srv := &server{
		db:             tdb.DB,
		auth:           auth,
		sessions:       mgr,
		chatMeta:       chatMeta,
		settings:       settings,
		bindings:       bStore,
		supportStore:   sStore,
		supportSvc:     supSvc,
		glpiClient:     glpiCli,
		tacticalClient: tacticalCli,
		loginLimit:     newLoginLimiter(),
		log:            slog.Default(),
	}

	// Create users
	now := time.Now().Unix()
	user1ID := "user-1"
	_, _ = tdb.DB.Exec(`INSERT INTO users (id, email, password_hash, company_name, cpf, active, created_at)
		VALUES (?, 'user1@tenant1.local', 'hash', 'Tenant 1', '111', 1, ?)`, user1ID, now)
	user1Token := "tok-user-1"
	_, _ = tdb.DB.Exec(`INSERT INTO auth_tokens (token, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)`, user1Token, user1ID, now+3600, now)

	admin1ID := "admin-1"
	_, _ = tdb.DB.Exec(`INSERT INTO users (id, email, password_hash, company_name, cpf, active, created_at, parent_id)
		VALUES (?, 'admin1@tenant1.local', 'hash', 'Tenant 1', '112', 1, ?, ?)`, admin1ID, now, user1ID)
	admin1Token := "tok-admin-1"
	_, _ = tdb.DB.Exec(`INSERT INTO auth_tokens (token, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)`, admin1Token, admin1ID, now+3600, now)
	_, _ = tdb.DB.Exec(`INSERT INTO user_roles (user_id, role) VALUES (?, ?)`, admin1ID, RoleAdmin)

	subuserID := "subuser-1"
	_, _ = tdb.DB.Exec(`INSERT INTO users (id, email, password_hash, company_name, cpf, active, created_at, parent_id)
		VALUES (?, 'subuser1@tenant1.local', 'hash', 'Tenant 1', '113', 1, ?, ?)`, subuserID, now, user1ID)
	subuserToken := "tok-subuser-1"
	_, _ = tdb.DB.Exec(`INSERT INTO auth_tokens (token, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)`, subuserToken, subuserID, now+3600, now)

	user2ID := "user-2"
	_, _ = tdb.DB.Exec(`INSERT INTO users (id, email, password_hash, company_name, cpf, active, created_at)
		VALUES (?, 'user2@tenant2.local', 'hash', 'Tenant 2', '222', 1, ?)`, user2ID, now)
	user2Token := "tok-user-2"
	_, _ = tdb.DB.Exec(`INSERT INTO auth_tokens (token, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)`, user2Token, user2ID, now+3600, now)

	// Create sessions with proper whatsmeow client
	sess1ID := "sess-1"
	cli1 := whatsmeow.NewClient(container.NewDevice(), waLog.Noop)
	sess1 := newSession(mgr, sess1ID, "Sess 1", cli1)
	sess1.ownerID = user1ID
	mgr.register(sess1)

	sess2ID := "sess-2"
	cli2 := whatsmeow.NewClient(container.NewDevice(), waLog.Noop)
	sess2 := newSession(mgr, sess2ID, "Sess 2", cli2)
	sess2.ownerID = user2ID
	mgr.register(sess2)

	// Create chats
	chat1JID := "5511999990001@s.whatsapp.net"
	_ = chatMeta.Upsert(ctx, ChatMeta{SessionID: sess1ID, ChatJID: chat1JID, Name: "Customer 1"})

	chat2JID := "5511999990002@s.whatsapp.net"
	_ = chatMeta.Upsert(ctx, ChatMeta{SessionID: sess2ID, ChatJID: chat2JID, Name: "Customer 2"})

	return &testAPISetup{
		srv:          srv,
		db:           tdb.DB,
		mockGLPI:     mockG,
		mockTactical: mockT,
		store:        sStore,
		bStore:       bStore,
		user1Token:   user1Token,
		user1ID:      user1ID,
		admin1Token:  admin1Token,
		admin1ID:     admin1ID,
		subuserToken: subuserToken,
		subuserID:    subuserID,
		user2Token:   user2Token,
		user2ID:      user2ID,
		sess1ID:      sess1ID,
		sess2ID:      sess2ID,
		chat1JID:     chat1JID,
		chat2JID:     chat2JID,
	}
}

func doRequest(handler http.Handler, method, path, token string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	var rBody io.Reader
	if body != nil {
		rBody = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rBody)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// 1. Feature Flag Disabled returns 503 support_disabled on all endpoints.
func TestSupportAPI_FeatureFlagDisabled(t *testing.T) {
	setup := setupSupportAPITest(t, false, false)
	h := setup.srv.routes()

	endpoints := []struct {
		method  string
		path    string
		body    []byte
		headers map[string]string
	}{
		{"GET", fmt.Sprintf("/api/sessions/%s/chats/%s/support", setup.sess1ID, setup.chat1JID), nil, nil},
		{"POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), []byte(`{"title":"T","description":"D","requesterName":"R"}`), map[string]string{"Content-Type": "application/json", "Idempotency-Key": "idemp-disabled-01"}},
		{"GET", "/api/support/requests/req-1", nil, nil},
		{"POST", "/api/support/requests/req-1/retry", nil, nil},
		{"POST", "/api/support/requests/req-1/reconcile", []byte(`{"outcome":"safe_to_retry"}`), map[string]string{"Content-Type": "application/json"}},
		{"GET", "/api/support/devices", nil, nil},
		{"GET", "/api/support/devices/dev-1", nil, nil},
		{"PUT", "/api/support/requests/req-1/device", []byte(`{"deviceBindingId":"dev-1"}`), map[string]string{"Content-Type": "application/json"}},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			rec := doRequest(h, ep.method, ep.path, setup.user1Token, ep.body, ep.headers)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d. body: %s", rec.Code, rec.Body.String())
			}
			var env SupportErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("failed to decode error body: %v", err)
			}
			if env.Error.Code != "support_disabled" {
				t.Fatalf("expected code support_disabled, got %s", env.Error.Code)
			}
		})
	}

	// Verify options feature flag
	rec := doRequest(h, "GET", "/api/settings/options", setup.user1Token, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from options, got %d", rec.Code)
	}
	var opt map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &opt)
	feat, ok := opt["features"].(map[string]any)
	if !ok {
		t.Fatalf("features missing in options: %v", opt)
	}
	if feat["support"] != false {
		t.Fatalf("expected features.support=false, got %v", feat["support"])
	}
	if feat["tactical"] != false {
		t.Fatalf("expected features.tactical=false, got %v", feat["tactical"])
	}
}

// 2. Authentication required: missing token returns 401.
func TestSupportAPI_AuthenticationRequired(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", fmt.Sprintf("/api/sessions/%s/chats/%s/support", setup.sess1ID, setup.chat1JID)},
		{"POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID)},
		{"GET", "/api/support/requests/req-1"},
		{"POST", "/api/support/requests/req-1/retry"},
		{"POST", "/api/support/requests/req-1/reconcile"},
		{"GET", "/api/support/devices"},
		{"GET", "/api/support/devices/dev-1"},
		{"PUT", "/api/support/requests/req-1/device"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			rec := doRequest(h, ep.method, ep.path, "", nil, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d. body: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// 3. IDOR and Tenant Isolation.
func TestSupportAPI_IDORAndTenantIsolation(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()
	ctx := context.Background()

	// Pre-create device binding in Tenant 2
	b2, err := setup.bStore.Upsert(ctx, DeviceBinding{
		OwnerID:  setup.user2ID,
		TenantID: setup.user2ID,
		Hostname: "PC-TENANT-2",
	})
	if err != nil {
		t.Fatalf("failed to insert binding in tenant 2: %v", err)
	}

	// Pre-create device binding in Tenant 1
	b1, err := setup.bStore.Upsert(ctx, DeviceBinding{
		OwnerID:  setup.user1ID,
		TenantID: setup.user1ID,
		Hostname: "PC-TENANT-1",
	})
	if err != nil {
		t.Fatalf("failed to insert binding in tenant 1: %v", err)
	}

	// User 1 requests Device 2 (Tenant 2) -> Must return 404
	rec := doRequest(h, "GET", "/api/support/devices/"+b2.ID, setup.user1Token, nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant device get, got %d. body: %s", rec.Code, rec.Body.String())
	}

	// User 1 searches devices -> only finds PC-TENANT-1, never PC-TENANT-2
	rec = doRequest(h, "GET", "/api/support/devices", setup.user1Token, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("search devices failed: %d", rec.Code)
	}
	var searchResp struct {
		Devices []DeviceBindingPublicDTO `json:"devices"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &searchResp)
	if len(searchResp.Devices) != 1 || searchResp.Devices[0].ID != b1.ID {
		t.Fatalf("expected exactly 1 device from tenant 1, got %d: %+v", len(searchResp.Devices), searchResp.Devices)
	}

	// Pre-create support request in Tenant 2
	req2, err := setup.store.CreateTicketRequest(ctx, CreateSupportTicketInput{
		OwnerID:            setup.user2ID,
		TenantID:           setup.user2ID,
		SessionID:          setup.sess2ID,
		ChatJID:            setup.chat2JID,
		RequesterName:      "Alice",
		Title:              "Ticket Tenant 2",
		Description:        "Desc",
		IdempotencyKey:     "idemp-tenant2-0001",
		ExternalID:         "ext-t2-1",
		ActorUserID:        setup.user2ID,
		ProcessingToken:    "tok-t2-1",
		HostnameInformed:   "PC-TENANT-2",
		PayloadFingerprint: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("failed to create request in tenant 2: %v", err)
	}

	// User 1 requests request in Tenant 2 -> Must return 404 (never 403 or 200)
	rec = doRequest(h, "GET", "/api/support/requests/"+req2.ID, setup.user1Token, nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant support request get, got %d. body: %s", rec.Code, rec.Body.String())
	}

	// Pre-create support request in Tenant 1
	req1, err := setup.store.CreateTicketRequest(ctx, CreateSupportTicketInput{
		OwnerID:            setup.user1ID,
		TenantID:           setup.user1ID,
		SessionID:          setup.sess1ID,
		ChatJID:            setup.chat1JID,
		RequesterName:      "Bob",
		Title:              "Ticket Tenant 1",
		Description:        "Desc",
		IdempotencyKey:     "idemp-tenant1-0001",
		ExternalID:         "ext-t1-1",
		ActorUserID:        setup.user1ID,
		ProcessingToken:    "tok-t1-1",
		HostnameInformed:   "PC-TENANT-1",
		PayloadFingerprint: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("failed to create request in tenant 1: %v", err)
	}

	// User 1 attempts to bind device from Tenant 2 to Tenant 1 request -> Must return 404
	putBody, _ := json.Marshal(UpdateDeviceReq{DeviceBindingID: b2.ID})
	rec = doRequest(h, "PUT", "/api/support/requests/"+req1.ID+"/device", setup.user1Token, putBody, map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant device update, got %d. body: %s", rec.Code, rec.Body.String())
	}
}

// 4. Conversation Access Control: same tenant but user without session access receives 403.
func TestSupportAPI_ConversationAccessControl(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()
	ctx := context.Background()

	// subuser-1 belongs to tenant 1 (parent is user-1), but does NOT have session sess-1 linked.
	// GET chat support for sess-1 by subuser-1 -> 403
	rec := doRequest(h, "GET", fmt.Sprintf("/api/sessions/%s/chats/%s/support", setup.sess1ID, setup.chat1JID), setup.subuserToken, nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unauthorized session access in same tenant, got %d. body: %s", rec.Code, rec.Body.String())
	}

	// User 2 (tenant 2) tries to access sess-1 (tenant 1) -> 404 (not 403)
	rec = doRequest(h, "GET", fmt.Sprintf("/api/sessions/%s/chats/%s/support", setup.sess1ID, setup.chat1JID), setup.user2Token, nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant session access, got %d. body: %s", rec.Code, rec.Body.String())
	}

	// Pre-create request in sess-1
	req1, err := setup.store.CreateTicketRequest(ctx, CreateSupportTicketInput{
		OwnerID:            setup.user1ID,
		TenantID:           setup.user1ID,
		SessionID:          setup.sess1ID,
		ChatJID:            setup.chat1JID,
		RequesterName:      "Bob",
		Title:              "Ticket 1",
		Description:        "Desc",
		IdempotencyKey:     "idemp-access-00001",
		ExternalID:         "ext-access-1",
		ActorUserID:        setup.user1ID,
		ProcessingToken:    "tok-access-1",
		HostnameInformed:   "PC-1",
		PayloadFingerprint: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Subuser-1 trying to GET request in sess-1 -> 403
	rec = doRequest(h, "GET", "/api/support/requests/"+req1.ID, setup.subuserToken, nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unauthorized session on request get, got %d. body: %s", rec.Code, rec.Body.String())
	}
}

// 5. Create Ticket, Idempotency, and Replay.
func TestSupportAPI_CreateTicketAndIdempotency(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()

	createReq := CreateSupportTicketReq{
		RequesterName: "Maria Silva",
		Title:         "Impressora Offline",
		Description:   "A impressora do segundo andar parou de responder.",
		Priority:      3,
	}
	bodyBytes, _ := json.Marshal(createReq)

	// 1st request -> 201 Created
	rec := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, bodyBytes, map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-key-create-12345",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d. body: %s", rec.Code, rec.Body.String())
	}

	var respEnv SupportTicketResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &respEnv); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if respEnv.SupportRequest.SyncState != "synced" {
		t.Fatalf("expected syncState synced, got %s", respEnv.SupportRequest.SyncState)
	}
	if respEnv.SupportRequest.Title != "Impressora Offline" {
		t.Fatalf("expected title Impressora Offline, got %s", respEnv.SupportRequest.Title)
	}

	// Verify exactly 1 call was made to GLPI CreateTicket
	if len(setup.mockGLPI.createTicketCalls) != 1 {
		t.Fatalf("expected 1 call to GLPI CreateTicket, got %d", len(setup.mockGLPI.createTicketCalls))
	}

	// 2nd request with SAME key and SAME payload -> 200 OK (Replay)
	rec2 := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, bodyBytes, map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-key-create-12345",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on replay, got %d. body: %s", rec2.Code, rec2.Body.String())
	}
	// Verify zero additional calls to GLPI
	if len(setup.mockGLPI.createTicketCalls) != 1 {
		t.Fatalf("replay must not call GLPI CreateTicket! calls count: %d", len(setup.mockGLPI.createTicketCalls))
	}

	// 3rd request with SAME key and DIFFERENT payload -> 409 Conflict (idempotency_conflict)
	diffReq := createReq
	diffReq.Title = "Different Title"
	diffBytes, _ := json.Marshal(diffReq)
	rec3 := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, diffBytes, map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-key-create-12345",
	})
	if rec3.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on payload conflict, got %d. body: %s", rec3.Code, rec3.Body.String())
	}
	var errEnv SupportErrorEnvelope
	_ = json.Unmarshal(rec3.Body.Bytes(), &errEnv)
	if errEnv.Error.Code != "idempotency_conflict" {
		t.Fatalf("expected code idempotency_conflict, got %s", errEnv.Error.Code)
	}
}

// 6. Retry CAS Concurrency: winner gets 202 (or 200), loser gets 409 state_conflict.
func TestSupportAPI_RetryCASConcurrency(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()
	ctx := context.Background()

	// Pre-create request in retryable_error state
	req, err := setup.store.CreateTicketRequest(ctx, CreateSupportTicketInput{
		OwnerID:            setup.user1ID,
		TenantID:           setup.user1ID,
		SessionID:          setup.sess1ID,
		ChatJID:            setup.chat1JID,
		RequesterName:      "Maria",
		Title:              "Retry Me",
		Description:        "Desc",
		IdempotencyKey:     "idemp-retry-cas-0001",
		ExternalID:         "ext-retry-cas-1",
		ActorUserID:        setup.user1ID,
		ProcessingToken:    "tok-retry-cas-1",
		HostnameInformed:   "PC-1",
		PayloadFingerprint: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Transition to retryable_error
	err = setup.store.FinishProcessing(ctx, FinishProcessingInput{
		ID:              req.ID,
		TenantID:        setup.user1ID,
		ProcessingToken: req.ProcessingToken,
		TargetState:     StateRetryableError,
		LastErrorCode:   "glpi_auth_failed",
		ActorUserID:     &setup.user1ID,
	})
	if err != nil {
		t.Fatalf("failed to finish processing: %v", err)
	}

	// Make GLPI CreateTicket slow to test concurrency
	slowDone := make(chan struct{})
	setup.mockGLPI.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
		<-slowDone
		return glpi.CreatedTicket{ID: "201", Href: "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/201"}, nil
	}

	var wg sync.WaitGroup
	results := make([]int, 2)
	wg.Add(2)

	// Fire two concurrent retries
	for i := 0; i < 2; i++ {
		idx := i
		go func() {
			defer wg.Done()
			rec := doRequest(h, "POST", "/api/support/requests/"+req.ID+"/retry", setup.user1Token, nil, nil)
			results[idx] = rec.Code
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(slowDone)
	wg.Wait()

	// One must be 202 Accepted (or 200), the other must be 409 Conflict
	hasAccepted := (results[0] == http.StatusAccepted || results[0] == http.StatusOK) || (results[1] == http.StatusAccepted || results[1] == http.StatusOK)
	hasConflict := (results[0] == http.StatusConflict) || (results[1] == http.StatusConflict)

	if !hasAccepted || !hasConflict {
		t.Fatalf("expected one winner (202/200) and one loser (409), got %d and %d", results[0], results[1])
	}
}

// 7. Reconcile: admin only, remote validation against GLPI GetTicket.
func TestSupportAPI_Reconcile(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()
	ctx := context.Background()

	// Pre-create request in unknown state
	req, err := setup.store.CreateTicketRequest(ctx, CreateSupportTicketInput{
		OwnerID:            setup.user1ID,
		TenantID:           setup.user1ID,
		SessionID:          setup.sess1ID,
		ChatJID:            setup.chat1JID,
		RequesterName:      "Maria",
		Title:              "Reconcile Me",
		Description:        "Desc",
		IdempotencyKey:     "idemp-reconcile-001",
		ExternalID:         "ext-rec-1",
		ActorUserID:        setup.user1ID,
		ProcessingToken:    "tok-rec-1",
		HostnameInformed:   "PC-1",
		PayloadFingerprint: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	_ = setup.store.FinishProcessing(ctx, FinishProcessingInput{
		ID:              req.ID,
		TenantID:        setup.user1ID,
		ProcessingToken: req.ProcessingToken,
		TargetState:     StateUnknown,
		LastErrorCode:   "glpi_ambiguous",
		ActorUserID:     &setup.user1ID,
	})

	// Non-admin user calls reconcile -> 403
	bodySynced, _ := json.Marshal(ReconcileReq{Outcome: "synced", GLPITicketID: strPtr("77")})
	rec := doRequest(h, "POST", "/api/support/requests/"+req.ID+"/reconcile", setup.user1Token, bodySynced, map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin reconcile, got %d", rec.Code)
	}

	// Admin calls with ticket not found in GLPI -> 404
	setup.mockGLPI.getTicketFn = func(ctx context.Context, id string) (glpi.Ticket, error) {
		return glpi.Ticket{}, glpi.ErrNotFound
	}
	rec = doRequest(h, "POST", "/api/support/requests/"+req.ID+"/reconcile", setup.admin1Token, bodySynced, map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent remote ticket, got %d", rec.Code)
	}

	// Admin calls with ticket external_id mismatch -> 422
	setup.mockGLPI.getTicketFn = func(ctx context.Context, id string) (glpi.Ticket, error) {
		return glpi.Ticket{
			ID:         "77",
			ExternalID: "DIFFERENT-EXTERNAL-ID",
			Href:       "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/77",
		}, nil
	}
	rec = doRequest(h, "POST", "/api/support/requests/"+req.ID+"/reconcile", setup.admin1Token, bodySynced, map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for external_id mismatch, got %d. body: %s", rec.Code, rec.Body.String())
	}

	// Admin calls with matching external_id -> 200 OK, transitions to synced
	setup.mockGLPI.getTicketFn = func(ctx context.Context, id string) (glpi.Ticket, error) {
		return glpi.Ticket{
			ID:         "77",
			ExternalID: req.ExternalID,
			Href:       "https://glpi.invalid/api.php/v2.3/Assistance/Ticket/77",
		}, nil
	}
	rec = doRequest(h, "POST", "/api/support/requests/"+req.ID+"/reconcile", setup.admin1Token, bodySynced, map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid reconcile, got %d. body: %s", rec.Code, rec.Body.String())
	}
	var recResp SupportTicketResponseEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &recResp)
	if recResp.SupportRequest.SyncState != "synced" {
		t.Fatalf("expected syncState synced, got %s", recResp.SupportRequest.SyncState)
	}
	if recResp.SupportRequest.GLPITicketID == nil || *recResp.SupportRequest.GLPITicketID != "77" {
		t.Fatalf("expected glpiTicketId 77, got %v", recResp.SupportRequest.GLPITicketID)
	}
}

// 8. Content-Type, body limits, and strict JSON document checks.
func TestSupportAPI_ContentTypeAndBodyLimits(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()

	ticketPath := fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID)
	validJSON := []byte(`{"title":"T","description":"D","requesterName":"R"}`)

	// Missing Content-Type on POST ticket -> 415
	rec := doRequest(h, "POST", ticketPath, setup.user1Token, validJSON, map[string]string{"Idempotency-Key": "idemp-ct-000000001"})
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for missing Content-Type, got %d", rec.Code)
	}

	// Multiple JSON documents in body -> 400
	multiJSON := []byte(`{"title":"T","description":"D","requesterName":"R"}{"extra":"data"}`)
	rec = doRequest(h, "POST", ticketPath, setup.user1Token, multiJSON, map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-ct-000000002",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for multiple JSON documents, got %d", rec.Code)
	}

	// Unknown fields disallowed -> 400
	unknownFields := []byte(`{"title":"T","description":"D","requesterName":"R","unknownField":"hack"}`)
	rec = doRequest(h, "POST", ticketPath, setup.user1Token, unknownFields, map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-ct-000000003",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown fields, got %d", rec.Code)
	}

	// GET request with body -> 400
	rec = doRequest(h, "GET", "/api/support/devices", setup.user1Token, []byte("some body"), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for GET request with body, got %d", rec.Code)
	}
}

// 9. Tactical Degradation: handles disabled and failing tactical integration cleanly.
func TestSupportAPI_TacticalDegradation(t *testing.T) {
	ctx := context.Background()

	// Setup with Tactical DISABLED
	setupDisabled := setupSupportAPITest(t, true, false)
	b1, err := setupDisabled.bStore.Upsert(ctx, DeviceBinding{
		OwnerID:         setupDisabled.user1ID,
		TenantID:        setupDisabled.user1ID,
		Hostname:        "HOST-DIS",
		TacticalAgentID: "agent-123",
	})
	if err != nil {
		t.Fatalf("failed to insert binding: %v", err)
	}
	rec := doRequest(setupDisabled.srv.routes(), "GET", "/api/support/devices/"+b1.ID, setupDisabled.user1Token, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp1 struct {
		Device        DeviceBindingPublicDTO `json:"device"`
		TacticalAgent any                    `json:"tacticalAgent"`
		Warnings      []string               `json:"warnings"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp1)
	if resp1.TacticalAgent != nil {
		t.Fatalf("expected tacticalAgent null when disabled, got %v", resp1.TacticalAgent)
	}
	if len(resp1.Warnings) != 0 {
		t.Fatalf("expected 0 warnings when disabled, got %v", resp1.Warnings)
	}

	// Setup with Tactical ENABLED but FAILING
	setupEnabled := setupSupportAPITest(t, true, true)
	b2, err := setupEnabled.bStore.Upsert(ctx, DeviceBinding{
		OwnerID:         setupEnabled.user1ID,
		TenantID:        setupEnabled.user1ID,
		Hostname:        "HOST-FAIL",
		TacticalAgentID: "agent-456",
	})
	if err != nil {
		t.Fatalf("failed to insert binding: %v", err)
	}
	setupEnabled.mockTactical.getAgentFn = func(ctx context.Context, agentID string) (tactical.Agent, error) {
		return tactical.Agent{}, errors.New("connection refused")
	}
	rec = doRequest(setupEnabled.srv.routes(), "GET", "/api/support/devices/"+b2.ID, setupEnabled.user1Token, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on tactical degradation, got %d", rec.Code)
	}
	var resp2 struct {
		Device        DeviceBindingPublicDTO `json:"device"`
		TacticalAgent any                    `json:"tacticalAgent"`
		Warnings      []string               `json:"warnings"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp2)
	if resp2.TacticalAgent != nil {
		t.Fatalf("expected tacticalAgent null on failure, got %v", resp2.TacticalAgent)
	}
	if len(resp2.Warnings) != 1 || resp2.Warnings[0] != "tactical_unavailable" {
		t.Fatalf("expected warning tactical_unavailable, got %v", resp2.Warnings)
	}
}

// 10. Response Security: processing_token and payload_fingerprint never leak in JSON.
func TestSupportAPI_ResponseSecurityNoTokens(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()

	createReq := CreateSupportTicketReq{
		RequesterName: "Maria Silva",
		Title:         "Vazamento Test",
		Description:   "Descricao teste seguranca",
	}
	bodyBytes, _ := json.Marshal(createReq)

	rec := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, bodyBytes, map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-leak-test-001",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", rec.Code)
	}

	rawJSON := rec.Body.String()
	lowerJSON := strings.ToLower(rawJSON)

	if strings.Contains(lowerJSON, "processing_token") || strings.Contains(lowerJSON, "processingtoken") {
		t.Fatalf("CRITICAL SECURITY LEAK: processing_token found in JSON response: %s", rawJSON)
	}
	if strings.Contains(lowerJSON, "payload_fingerprint") || strings.Contains(lowerJSON, "payloadfingerprint") {
		t.Fatalf("CRITICAL SECURITY LEAK: payload_fingerprint found in JSON response: %s", rawJSON)
	}
}

// 11. Server boot wiring and startup recovery.
func TestSupportAPI_StartupOrphanRecovery(t *testing.T) {
	ctx := context.Background()
	tdb := testdb.OpenTestDB(t, testdb.BackendSQLite)

	store, err := newSupportStore(ctx, tdb.DB, DialectSQLite)
	if err != nil {
		t.Fatalf("failed to create support store: %v", err)
	}

	// Insert orphaned processing request (started 100 seconds ago)
	now := time.Now().UTC().Unix()
	past := now - 100
	req, err := store.CreateTicketRequest(ctx, CreateSupportTicketInput{
		OwnerID:            "user-1",
		TenantID:           "tenant-1",
		SessionID:          "sess-1",
		ChatJID:            "chat-1@s.whatsapp.net",
		RequesterName:      "Alice",
		Title:              "Orphaned Request",
		Description:        "Desc",
		IdempotencyKey:     "idemp-orphan-00001",
		ExternalID:         "ext-orphan-1",
		ActorUserID:        "user-1",
		ProcessingToken:    "tok-orphan-1",
		HostnameInformed:   "PC-1",
		PayloadFingerprint: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("failed to insert request: %v", err)
	}

	// Manually backdate processing_started_at to simulate old crash
	_, err = tdb.DB.Exec(`UPDATE support_requests SET processing_started_at = ? WHERE id = ?`, past, req.ID)
	if err != nil {
		t.Fatalf("failed to backdate processing_started_at: %v", err)
	}

	// Run recovery with cutoff 30 seconds ago
	cutoff := now - 30
	recovered, err := store.RecoverOrphanedProcessing(ctx, cutoff, nil)
	if err != nil {
		t.Fatalf("RecoverOrphanedProcessing failed: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("expected 1 recovered request, got %d", recovered)
	}

	// Check that request is now in StateUnknown
	updated, err := store.GetByID(ctx, "tenant-1", req.ID)
	if err != nil {
		t.Fatalf("failed to get request: %v", err)
	}
	if updated.SyncState != StateUnknown {
		t.Fatalf("expected state unknown, got %s", updated.SyncState)
	}
	if updated.LastErrorCode != "orphaned_processing" {
		t.Fatalf("expected last_error_code orphaned_processing, got %s", updated.LastErrorCode)
	}
}

func TestSupportAPI_StartupConfigValidation(t *testing.T) {
	// Case 1: WACALLS_SUPPORT_ENABLED=false
	t.Run("SupportDisabled", func(t *testing.T) {
		t.Setenv("WACALLS_SUPPORT_ENABLED", "false")
		cfg, err := loadSupportConfig()
		if err != nil {
			t.Fatalf("expected nil error when support disabled, got: %v", err)
		}
		if cfg.Enabled {
			t.Fatalf("expected cfg.Enabled = false")
		}
	})

	// Case 2: WACALLS_SUPPORT_ENABLED=true but GLPI missing
	t.Run("GLPIMissing", func(t *testing.T) {
		t.Setenv("WACALLS_SUPPORT_ENABLED", "true")
		t.Setenv("WACALLS_GLPI_BASE_URL", "")
		_, err := loadSupportConfig()
		if err == nil {
			t.Fatalf("expected error when GLPI base URL missing")
		}
	})

	// Case 3: WACALLS_SUPPORT_ENABLED=true, GLPI valid, Tactical empty
	t.Run("GLPIValidTacticalEmpty", func(t *testing.T) {
		t.Setenv("WACALLS_SUPPORT_ENABLED", "true")
		t.Setenv("WACALLS_GLPI_BASE_URL", "https://glpi.example.com")
		t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-id")
		t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "client-secret")
		t.Setenv("WACALLS_GLPI_USERNAME", "glpi-user")
		t.Setenv("WACALLS_GLPI_PASSWORD", "glpi-pass")
		t.Setenv("WACALLS_TACTICAL_BASE_URL", "")
		t.Setenv("WACALLS_TACTICAL_API_KEY", "")

		cfg, err := loadSupportConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.Enabled {
			t.Fatalf("expected cfg.Enabled = true")
		}
		if cfg.TacticalConfig != nil {
			t.Fatalf("expected cfg.TacticalConfig == nil when both empty")
		}
	})

	// Case 4: WACALLS_SUPPORT_ENABLED=true, GLPI valid, Tactical partial (only URL)
	t.Run("TacticalPartialURLOnly", func(t *testing.T) {
		t.Setenv("WACALLS_SUPPORT_ENABLED", "true")
		t.Setenv("WACALLS_GLPI_BASE_URL", "https://glpi.example.com")
		t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-id")
		t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "client-secret")
		t.Setenv("WACALLS_GLPI_USERNAME", "glpi-user")
		t.Setenv("WACALLS_GLPI_PASSWORD", "glpi-pass")
		t.Setenv("WACALLS_TACTICAL_BASE_URL", "https://rmm.example.com")
		t.Setenv("WACALLS_TACTICAL_API_KEY", "")

		_, err := loadSupportConfig()
		if err == nil {
			t.Fatalf("expected error for partial tactical configuration")
		}
		if !strings.Contains(err.Error(), "incomplete Tactical RMM configuration") {
			t.Fatalf("expected 'incomplete Tactical RMM configuration' error, got: %v", err)
		}
	})

	// Case 5: WACALLS_SUPPORT_ENABLED=true, GLPI valid, Tactical valid
	t.Run("BothValid", func(t *testing.T) {
		t.Setenv("WACALLS_SUPPORT_ENABLED", "true")
		t.Setenv("WACALLS_GLPI_BASE_URL", "https://glpi.example.com")
		t.Setenv("WACALLS_GLPI_CLIENT_ID", "client-id")
		t.Setenv("WACALLS_GLPI_CLIENT_SECRET", "client-secret")
		t.Setenv("WACALLS_GLPI_USERNAME", "glpi-user")
		t.Setenv("WACALLS_GLPI_PASSWORD", "glpi-pass")
		t.Setenv("WACALLS_TACTICAL_BASE_URL", "https://rmm.example.com")
		t.Setenv("WACALLS_TACTICAL_API_KEY", "rmm-api-key")

		cfg, err := loadSupportConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.TacticalConfig == nil {
			t.Fatalf("expected cfg.TacticalConfig != nil")
		}
	})
}

func TestSupportAPI_EventsEndpointNotRegistered(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()

	rec := doRequest(h, "GET", "/api/support/requests/req-1/events", setup.user1Token, nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for removed /events route, got %d. body: %s", rec.Code, rec.Body.String())
	}
}

func TestSupportAPI_BodyExcessDetection(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()

	path := fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID)

	// Construct base valid JSON
	baseJSON := []byte(`{"requesterName":"Maria Silva","title":"Impressora","description":"Falha de impressao"}`)
	if len(baseJSON) > 16384 {
		t.Fatalf("baseJSON too large: %d", len(baseJSON))
	}

	// 1. Exactly at limit: 16384 bytes (padded with spaces)
	bodyExact := make([]byte, 16384)
	copy(bodyExact, baseJSON)
	for i := len(baseJSON); i < 16384; i++ {
		bodyExact[i] = ' '
	}
	headersExact := map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-exact-limit-16384-bytes",
	}
	recExact := doRequest(h, "POST", path, setup.user1Token, bodyExact, headersExact)
	if recExact.Code != http.StatusCreated && recExact.Code != http.StatusOK {
		t.Fatalf("expected 201/200 for body exactly at limit (16384 bytes), got %d: %s", recExact.Code, recExact.Body.String())
	}

	// 2. Limit + 1: 16385 bytes
	bodyExcess := make([]byte, 16385)
	copy(bodyExcess, baseJSON)
	for i := len(baseJSON); i < 16385; i++ {
		bodyExcess[i] = ' '
	}
	headersExcess := map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-excess-limit-16385-bytes",
	}
	recExcess := doRequest(h, "POST", path, setup.user1Token, bodyExcess, headersExcess)
	if recExcess.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 invalid_request for body at limit + 1 (16385 bytes), got %d: %s", recExcess.Code, recExcess.Body.String())
	}
	var env SupportErrorEnvelope
	if err := json.Unmarshal(recExcess.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode error envelope: %v", err)
	}
	if env.Error.Code != "invalid_request" {
		t.Fatalf("expected error code 'invalid_request', got: %s", env.Error.Code)
	}
}

func TestSupportAPI_ReconcileAdminPermissions(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()
	ctx := context.Background()

	// Create request in tenant-1 in StateUnknown
	req, err := setup.store.CreateTicketRequest(ctx, CreateSupportTicketInput{
		OwnerID:            setup.user1ID,
		TenantID:           setup.user1ID,
		SessionID:          setup.sess1ID,
		ChatJID:            setup.chat1JID,
		RequesterName:      "Requester",
		Title:              "Title",
		Description:        "Desc",
		Priority:           3,
		IdempotencyKey:     "idemp-reconcile-perm-12345",
		ExternalID:         "ext-reconcile-perm-1",
		PayloadFingerprint: strings.Repeat("e", 64),
		ProcessingToken:    "token-reconcile-perm-1",
	})
	if err != nil {
		t.Fatalf("failed to insert ticket request: %v", err)
	}
	// Finish as unknown
	if err := setup.store.FinishProcessing(ctx, FinishProcessingInput{
		ID:              req.ID,
		TenantID:        setup.user1ID,
		ProcessingToken: req.ProcessingToken,
		TargetState:     StateUnknown,
		LastErrorCode:   "ticket_result_unknown",
		ActorType:       ActorTypeUser,
		ActorUserID:     &setup.user1ID,
	}); err != nil {
		t.Fatalf("failed to finish processing as unknown: %v", err)
	}

	reconcilePath := "/api/support/requests/" + req.ID + "/reconcile"
	reconcileBody := []byte(`{"outcome":"safe_to_retry"}`)
	jsonHeaders := map[string]string{"Content-Type": "application/json"}

	// 1. Regular non-admin of tenant-1 gets 403
	recNonAdmin := doRequest(h, "POST", reconcilePath, setup.user1Token, reconcileBody, jsonHeaders)
	if recNonAdmin.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin reconcile, got %d: %s", recNonAdmin.Code, recNonAdmin.Body.String())
	}

	// 2. Admin of tenant-2 gets 404 (anti-IDOR)
	now := time.Now().Unix()
	admin2ID := "admin-2"
	_, _ = setup.srv.db.Exec(`INSERT INTO users (id, email, password_hash, company_name, cpf, active, created_at, parent_id)
		VALUES (?, 'admin2@tenant2.local', 'hash', 'Tenant 2', '223', 1, ?, ?)`, admin2ID, now, setup.user2ID)
	admin2Token := "tok-admin-2"
	_, _ = setup.srv.db.Exec(`INSERT INTO auth_tokens (token, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)`, admin2Token, admin2ID, now+3600, now)
	_, _ = setup.srv.db.Exec(`INSERT INTO user_roles (user_id, role) VALUES (?, ?)`, admin2ID, RoleAdmin)

	recOtherAdmin := doRequest(h, "POST", reconcilePath, admin2Token, reconcileBody, jsonHeaders)
	if recOtherAdmin.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for other tenant admin reconcile, got %d: %s", recOtherAdmin.Code, recOtherAdmin.Body.String())
	}

	// 3. Admin of tenant-1 can reconcile successfully without userCanAccessSession
	recAdmin := doRequest(h, "POST", reconcilePath, setup.admin1Token, reconcileBody, jsonHeaders)
	if recAdmin.Code != http.StatusOK {
		t.Fatalf("expected 200 for tenant admin reconcile, got %d: %s", recAdmin.Code, recAdmin.Body.String())
	}
}

func strPtr(s string) *string {
	return &s
}

type spySupportService struct {
	supportServiceAPI
	createTicketCalls int
	retryCalls        int
	failCreateResNil  bool
	failRetryResNil   bool
}

func (s *spySupportService) CreateTicket(ctx context.Context, in ServiceCreateTicketInput) (*ServiceCreateTicketResult, error) {
	s.createTicketCalls++
	if s.failCreateResNil {
		return nil, errors.New("underlying DB failure during create ticket")
	}
	return s.supportServiceAPI.CreateTicket(ctx, in)
}

func (s *spySupportService) Retry(ctx context.Context, in ServiceRetryInput) (*SupportRequest, error) {
	s.retryCalls++
	if s.failRetryResNil {
		return nil, errors.New("underlying DB failure during retry")
	}
	return s.supportServiceAPI.Retry(ctx, in)
}

// 12. GLPI classified error -> HTTP status code mapping for ticket creation.
func TestSupportAPI_CreateTicket_GLPIErrorStatusMapping(t *testing.T) {
	assertNoInternalLeak := func(t *testing.T, body []byte) {
		t.Helper()
		raw := string(body)
		for _, s := range []string{"processingToken", "payloadFingerprint", "glpiTicketHref"} {
			if strings.Contains(raw, s) {
				t.Fatalf("internal field %q leaked in response body: %s", s, raw)
			}
		}
	}

	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()

	cases := []struct {
		name              string
		glpiErr           error
		wantStatus        int
		wantRetryAfter    string
		wantSyncState     string
		wantLastErrorCode string
	}{
		{
			// classifyGLPIError: StatusCode 429 -> StateRetryableError, "rate_limited".
			// handler: StateRetryableError + rate_limited -> 429 + Retry-After: 60.
			name:              "rate_limited_429",
			glpiErr:           &glpi.Error{Op: "create ticket", StatusCode: 429, RetryAfter: 60 * time.Second},
			wantStatus:        http.StatusTooManyRequests,
			wantRetryAfter:    "60",
			wantSyncState:     "retryable_error",
			wantLastErrorCode: "rate_limited",
		},
		{
			// classifyGLPIError: StatusCode 401 -> StateRetryableError, "integration_auth_failed".
			// handler: StateRetryableError + integration_auth_failed -> 502 Bad Gateway with safe envelope.
			name:              "integration_auth_failed_401",
			glpiErr:           &glpi.Error{Op: "create ticket", StatusCode: 401},
			wantStatus:        http.StatusBadGateway,
			wantSyncState:     "retryable_error",
			wantLastErrorCode: "integration_auth_failed",
		},
		{
			// classifyGLPIError: error is not a *glpi.Error (errors.As fails) -> default
			// branch -> StateUnknown, "ticket_result_unknown".
			// handler: StateUnknown -> 202 Accepted (confirmed from the actual switch in
			// handleCreateChatSupportTicket; StateUnknown is NOT mapped to 503/500).
			name:              "unclassified_transport_failure_unknown",
			glpiErr:           errors.New("transport failure"),
			wantStatus:        http.StatusAccepted,
			wantSyncState:     "unknown",
			wantLastErrorCode: "ticket_result_unknown",
		},
		{
			// classifyGLPIError: StatusCode 400 -> StateFailed, "ticket_rejected".
			// handler: StateFailed -> 502 Bad Gateway.
			name:              "bad_request_400_failed",
			glpiErr:           &glpi.Error{Op: "create ticket", StatusCode: 400},
			wantStatus:        http.StatusBadGateway,
			wantSyncState:     "failed",
			wantLastErrorCode: "ticket_rejected",
		},
	}

	for i, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			setup.mockGLPI.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
				return glpi.CreatedTicket{}, tc.glpiErr
			}

			createReq := CreateSupportTicketReq{
				RequesterName: "Maria Silva",
				Title:         fmt.Sprintf("GLPI Error Mapping %d", i),
				Description:   "Verifying GLPI classified error status mapping.",
				Priority:      3,
			}
			bodyBytes, _ := json.Marshal(createReq)

			rec := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, bodyBytes, map[string]string{
				"Content-Type":    "application/json",
				"Idempotency-Key": fmt.Sprintf("idemp-glpi-status-map-%d", i),
			})

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d. body: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if tc.wantRetryAfter != "" {
				if got := rec.Header().Get("Retry-After"); got != tc.wantRetryAfter {
					t.Fatalf("expected Retry-After %q, got %q", tc.wantRetryAfter, got)
				}
			} else if got := rec.Header().Get("Retry-After"); got != "" {
				t.Fatalf("unexpected Retry-After header %q", got)
			}

			var respEnv SupportTicketResponseEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &respEnv); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}
			if respEnv.SupportRequest.SyncState != tc.wantSyncState {
				t.Fatalf("expected syncState %q, got %q", tc.wantSyncState, respEnv.SupportRequest.SyncState)
			}
			gotLastErrorCode := ""
			if respEnv.SupportRequest.LastErrorCode != nil {
				gotLastErrorCode = *respEnv.SupportRequest.LastErrorCode
			}
			if gotLastErrorCode != tc.wantLastErrorCode {
				t.Fatalf("expected lastErrorCode %q, got %q", tc.wantLastErrorCode, gotLastErrorCode)
			}

			assertNoInternalLeak(t, rec.Body.Bytes())
		})
	}

	// Internal error / res == nil path (500 internal_error):
	// Reaches handleCreateChatSupportTicket, passes through device binding
	// resolution, invokes SupportService.CreateTicket, and when CreateTicket
	// returns res=nil and err!=nil, hits the exact res==nil guard in supportapi.go.
	t.Run("create_ticket_res_nil_guard_hits_500", func(t *testing.T) {
		ctx := context.Background()
		b1, err := setup.bStore.Upsert(ctx, DeviceBinding{
			ID:                 "binding-test-res-nil-01",
			OwnerID:            setup.user1ID,
			TenantID:           setup.user1ID,
			Hostname:           "PC-RES-NIL",
			HostnameNormalized: "PC-RES-NIL",
			MatchStatus:        "matched",
		})
		if err != nil {
			t.Fatalf("failed to insert test device binding: %v", err)
		}

		spy := &spySupportService{
			supportServiceAPI: setup.srv.supportSvc,
			failCreateResNil:  true,
		}
		setup.srv.supportSvc = spy
		t.Cleanup(func() {
			setup.srv.supportSvc = spy.supportServiceAPI
		})

		createReq := CreateSupportTicketReq{
			RequesterName:   "Maria Silva",
			Title:           "Internal Error Res Nil",
			Description:     "Verifying 500 when service returns res=nil.",
			DeviceBindingID: &b1.ID,
			Priority:        3,
		}
		bodyBytes, _ := json.Marshal(createReq)

		rec := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, bodyBytes, map[string]string{
			"Content-Type":    "application/json",
			"Idempotency-Key": "idemp-create-ticket-res-nil-guard-500",
		})

		if spy.createTicketCalls != 1 {
			t.Fatalf("expected exactly 1 call to CreateTicket, got %d", spy.createTicketCalls)
		}
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d. body: %s", rec.Code, rec.Body.String())
		}
		var errEnv SupportErrorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &errEnv); err != nil {
			t.Fatalf("failed to unmarshal error response: %v", err)
		}
		if errEnv.Error.Code != "internal_error" {
			t.Fatalf("expected error code internal_error, got %q", errEnv.Error.Code)
		}
		if errEnv.Error.Message != "failed to create support ticket" {
			t.Fatalf("expected message 'failed to create support ticket', got %q", errEnv.Error.Message)
		}
		if strings.Contains(rec.Body.String(), "supportRequest") {
			t.Fatalf("response body must not contain partial supportRequest DTO: %s", rec.Body.String())
		}
		assertNoInternalLeak(t, rec.Body.Bytes())
	})
}

// 13. GLPI classified error -> HTTP status code mapping for ticket retry.
func TestSupportAPI_Retry_GLPIErrorStatusMapping(t *testing.T) {
	setup := setupSupportAPITest(t, true, false)
	h := setup.srv.routes()
	ctx := context.Background()

	assertNoInternalLeak := func(t *testing.T, body []byte) {
		t.Helper()
		raw := string(body)
		for _, s := range []string{"processingToken", "payloadFingerprint", "glpiTicketHref"} {
			if strings.Contains(raw, s) {
				t.Fatalf("internal field %q leaked in response body: %s", s, raw)
			}
		}
	}

	newRetryableRequest := func(t *testing.T, idempKey, externalID, procToken string) *SupportRequest {
		t.Helper()
		req, err := setup.store.CreateTicketRequest(ctx, CreateSupportTicketInput{
			OwnerID:            setup.user1ID,
			TenantID:           setup.user1ID,
			SessionID:          setup.sess1ID,
			ChatJID:            setup.chat1JID,
			RequesterName:      "Maria",
			Title:              "Retry Status Mapping",
			Description:        "Desc",
			IdempotencyKey:     idempKey,
			ExternalID:         externalID,
			ActorUserID:        setup.user1ID,
			ProcessingToken:    procToken,
			HostnameInformed:   "PC-1",
			PayloadFingerprint: strings.Repeat("a", 64),
		})
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		if err := setup.store.FinishProcessing(ctx, FinishProcessingInput{
			ID:              req.ID,
			TenantID:        setup.user1ID,
			ProcessingToken: req.ProcessingToken,
			TargetState:     StateRetryableError,
			LastErrorCode:   "glpi_auth_failed",
			ActorUserID:     &setup.user1ID,
		}); err != nil {
			t.Fatalf("failed to finish processing: %v", err)
		}
		return req
	}

	cases := []struct {
		name              string
		glpiErr           error
		wantStatus        int
		wantRetryAfter    string
		wantSyncState     string
		wantLastErrorCode string
	}{
		{
			// classifyGLPIError: StatusCode 429 -> StateRetryableError, "rate_limited".
			// handler: StateRetryableError + rate_limited -> 429 + Retry-After: 60.
			name:              "rate_limited_429",
			glpiErr:           &glpi.Error{Op: "create ticket", StatusCode: 429, RetryAfter: 60 * time.Second},
			wantStatus:        http.StatusTooManyRequests,
			wantRetryAfter:    "60",
			wantSyncState:     "retryable_error",
			wantLastErrorCode: "rate_limited",
		},
		{
			// classifyGLPIError: StatusCode 401 -> StateRetryableError, "integration_auth_failed".
			// handler: StateRetryableError + integration_auth_failed -> 502 Bad Gateway with safe envelope.
			name:              "integration_auth_failed_401",
			glpiErr:           &glpi.Error{Op: "create ticket", StatusCode: 401},
			wantStatus:        http.StatusBadGateway,
			wantSyncState:     "retryable_error",
			wantLastErrorCode: "integration_auth_failed",
		},
		{
			// classifyGLPIError: StatusCode 400 -> StateFailed, "ticket_rejected".
			// handler: StateFailed -> 502 Bad Gateway.
			name:              "bad_request_400_failed",
			glpiErr:           &glpi.Error{Op: "create ticket", StatusCode: 400},
			wantStatus:        http.StatusBadGateway,
			wantSyncState:     "failed",
			wantLastErrorCode: "ticket_rejected",
		},
	}

	for i, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := newRetryableRequest(t,
				fmt.Sprintf("idemp-retry-status-map-%d", i),
				fmt.Sprintf("ext-retry-status-map-%d", i),
				fmt.Sprintf("tok-retry-status-map-%d", i))

			setup.mockGLPI.createTicketFn = func(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error) {
				return glpi.CreatedTicket{}, tc.glpiErr
			}

			rec := doRequest(h, "POST", "/api/support/requests/"+req.ID+"/retry", setup.user1Token, nil, nil)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d. body: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if tc.wantRetryAfter != "" {
				if got := rec.Header().Get("Retry-After"); got != tc.wantRetryAfter {
					t.Fatalf("expected Retry-After %q, got %q", tc.wantRetryAfter, got)
				}
			} else if got := rec.Header().Get("Retry-After"); got != "" {
				t.Fatalf("unexpected Retry-After header %q", got)
			}

			var respEnv SupportTicketResponseEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &respEnv); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}
			if respEnv.SupportRequest.SyncState != tc.wantSyncState {
				t.Fatalf("expected syncState %q, got %q", tc.wantSyncState, respEnv.SupportRequest.SyncState)
			}
			gotLastErrorCode := ""
			if respEnv.SupportRequest.LastErrorCode != nil {
				gotLastErrorCode = *respEnv.SupportRequest.LastErrorCode
			}
			if gotLastErrorCode != tc.wantLastErrorCode {
				t.Fatalf("expected lastErrorCode %q, got %q", tc.wantLastErrorCode, gotLastErrorCode)
			}

			assertNoInternalLeak(t, rec.Body.Bytes())
		})
	}

	// Internal error / res == nil path for retry (500 internal_error):
	// Reaches handleRetrySupportRequest, invokes SupportService.Retry, and when
	// Retry returns res=nil and err!=nil, hits the exact res==nil guard in supportapi.go.
	t.Run("retry_res_nil_guard_hits_500", func(t *testing.T) {
		req := newRetryableRequest(t, "idemp-retry-res-nil-guard", "ext-retry-res-nil-guard", "tok-retry-res-nil-guard")

		spy := &spySupportService{
			supportServiceAPI: setup.srv.supportSvc,
			failRetryResNil:   true,
		}
		setup.srv.supportSvc = spy
		t.Cleanup(func() {
			setup.srv.supportSvc = spy.supportServiceAPI
		})

		rec := doRequest(h, "POST", "/api/support/requests/"+req.ID+"/retry", setup.user1Token, nil, nil)

		if spy.retryCalls != 1 {
			t.Fatalf("expected exactly 1 call to Retry, got %d", spy.retryCalls)
		}
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d. body: %s", rec.Code, rec.Body.String())
		}
		var errEnv SupportErrorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &errEnv); err != nil {
			t.Fatalf("failed to unmarshal error response: %v", err)
		}
		if errEnv.Error.Code != "internal_error" {
			t.Fatalf("expected error code internal_error, got %q", errEnv.Error.Code)
		}
		if errEnv.Error.Message != "retry failed" {
			t.Fatalf("expected message 'retry failed', got %q", errEnv.Error.Message)
		}
		if strings.Contains(rec.Body.String(), "supportRequest") {
			t.Fatalf("response body must not contain partial supportRequest DTO: %s", rec.Body.String())
		}
		assertNoInternalLeak(t, rec.Body.Bytes())
	})
}
