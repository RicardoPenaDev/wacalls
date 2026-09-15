package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"testing"

	"wacalls/internal/glpi"
	"wacalls/internal/tactical"
)

// 1. Binding explícito válido preserva comportamento e persiste device_binding_id e glpi_computer_id.
func TestD025_ExplicitDeviceBinding(t *testing.T) {
	svc, store, bStore, _, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	binding, err := bStore.Upsert(ctx, DeviceBinding{
		OwnerID:        "user-1",
		TenantID:       "tenant-1",
		Hostname:       "SDE-ARS-RCP-02",
		GLPIComputerID: "59",
		MatchStatus:    "matched",
	})
	if err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-d025-explicit-1")
	input.DeviceBindingID = &binding.ID
	input.Hostname = &binding.Hostname

	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	if res.Request.DeviceBindingID == nil || *res.Request.DeviceBindingID != binding.ID {
		t.Fatalf("expected DeviceBindingID %q, got %v", binding.ID, res.Request.DeviceBindingID)
	}
	if res.Request.TicketGLPIComputerID == nil || *res.Request.TicketGLPIComputerID != "59" {
		t.Fatalf("expected TicketGLPIComputerID '59', got %v", res.Request.TicketGLPIComputerID)
	}

	// Verifica persistência direta no banco
	dbReq, err := store.GetByID(ctx, "tenant-1", res.Request.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if dbReq.DeviceBindingID == nil || *dbReq.DeviceBindingID != binding.ID {
		t.Fatalf("DB device_binding_id expected %q, got %v", binding.ID, dbReq.DeviceBindingID)
	}
}

// 2, 3 e 4. Hostname com correspondência exata: persiste device_binding_id,
// preserva glpi_computer_id e não duplica tactical_agent_id em support_requests.
func TestD025_HostnameExactMatch_PersistsBindingID(t *testing.T) {
	svc, store, bStore, _, _, tdb := setupTestSupportService(t)
	ctx := context.Background()

	binding, err := bStore.Upsert(ctx, DeviceBinding{
		OwnerID:         "user-1",
		TenantID:        "tenant-1",
		Hostname:        "SDE-ARS-RCP-02",
		GLPIComputerID:  "59",
		TacticalAgentID: "agent-tac-1",
		MatchStatus:     "matched",
	})
	if err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	// Chamado criado APENAS com hostname (DeviceBindingID = nil)
	input := sampleServiceCreateInput("tenant-1", "idemp-d025-exact-1")
	hostInput := "  sde-ars-rcp-02  "
	input.Hostname = &hostInput
	input.DeviceBindingID = nil

	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	// 2. Utiliza o binding encontrado e associa
	if res.Request.DeviceBindingID == nil {
		t.Fatalf("expected non-nil DeviceBindingID on request, got nil")
	}
	if *res.Request.DeviceBindingID != binding.ID {
		t.Fatalf("expected DeviceBindingID %q, got %q", binding.ID, *res.Request.DeviceBindingID)
	}

	// 4. Preserva glpi_computer_id
	if res.Request.TicketGLPIComputerID == nil || *res.Request.TicketGLPIComputerID != "59" {
		t.Fatalf("expected TicketGLPIComputerID '59', got %v", res.Request.TicketGLPIComputerID)
	}

	// 3. Persistência real em support_requests no banco
	var dbBindingID sql.NullString
	var dbTicketBindingID sql.NullString
	var dbGLPICompID sql.NullString
	query := `SELECT device_binding_id, ticket_device_binding_id, ticket_glpi_computer_id
		FROM support_requests WHERE id = ? AND tenant_id = ?`
	err = tdb.DB.QueryRowContext(ctx, query, res.Request.ID, "tenant-1").Scan(&dbBindingID, &dbTicketBindingID, &dbGLPICompID)
	if err != nil {
		t.Fatalf("failed to query support_requests: %v", err)
	}

	if !dbBindingID.Valid || dbBindingID.String != binding.ID {
		t.Fatalf("expected support_requests.device_binding_id = %q, got valid=%v val=%q", binding.ID, dbBindingID.Valid, dbBindingID.String)
	}
	if !dbTicketBindingID.Valid || dbTicketBindingID.String != binding.ID {
		t.Fatalf("expected support_requests.ticket_device_binding_id = %q, got valid=%v val=%q", binding.ID, dbTicketBindingID.Valid, dbTicketBindingID.String)
	}
	if !dbGLPICompID.Valid || dbGLPICompID.String != "59" {
		t.Fatalf("expected support_requests.ticket_glpi_computer_id = '59', got valid=%v val=%q", dbGLPICompID.Valid, dbGLPICompID.String)
	}

	// Confirma que store.GetByID lê corretamente
	reqFromStore, err := store.GetByID(ctx, "tenant-1", res.Request.ID)
	if err != nil {
		t.Fatalf("store.GetByID failed: %v", err)
	}
	if reqFromStore.DeviceBindingID == nil || *reqFromStore.DeviceBindingID != binding.ID {
		t.Fatalf("store.GetByID returned DeviceBindingID %v, want %q", reqFromStore.DeviceBindingID, binding.ID)
	}

	// Confirma que tactical_agent_id continua pertencendo a device_bindings e não a support_requests
	bFromStore, err := bStore.GetForTenant(ctx, "tenant-1", binding.ID)
	if err != nil {
		t.Fatalf("bStore.GetForTenant failed: %v", err)
	}
	if bFromStore.TacticalAgentID != "agent-tac-1" {
		t.Fatalf("expected device_bindings.tactical_agent_id = 'agent-tac-1', got %q", bFromStore.TacticalAgentID)
	}
}

// Resolução remota automática (binding não existia previamente):
// Ao encontrar GLPI e Tactical, cria o binding e enriquece support_requests com o ID gerado.
func TestD025_RemoteLookup_CreatesAndEnrichesBindingID(t *testing.T) {
	svc, store, bStore, mockG, mockT, tdb := setupTestSupportService(t)
	ctx := context.Background()

	mockG.findComputerFn = func(ctx context.Context, hostname string) (glpi.Computer, error) {
		if normalizeHostname(hostname) == "SDE-DISCOVER-01" {
			return glpi.Computer{ID: "88", Name: "SDE-DISCOVER-01"}, nil
		}
		return glpi.Computer{}, glpi.ErrNotFound
	}
	mockT.findAgentFn = func(ctx context.Context, hostname string) (tactical.Agent, error) {
		if normalizeHostname(hostname) == "SDE-DISCOVER-01" {
			return tactical.Agent{AgentID: "tac-88", Hostname: "SDE-DISCOVER-01", LastSeenValid: true}, nil
		}
		return tactical.Agent{}, errors.New("not found")
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-d025-remote-1")
	hostInput := "SDE-DISCOVER-01"
	input.Hostname = &hostInput
	input.DeviceBindingID = nil

	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	if res.Request.DeviceBindingID == nil {
		t.Fatalf("expected non-nil DeviceBindingID after remote discovery, got nil")
	}
	if res.Request.TicketGLPIComputerID == nil || *res.Request.TicketGLPIComputerID != "88" {
		t.Fatalf("expected TicketGLPIComputerID '88', got %v", res.Request.TicketGLPIComputerID)
	}

	// Binding foi criado no store
	binding, found, err := bStore.FindByHostname(ctx, "tenant-1", "SDE-DISCOVER-01")
	if err != nil || !found {
		t.Fatalf("expected device binding to be created, found=%v err=%v", found, err)
	}
	if *res.Request.DeviceBindingID != binding.ID {
		t.Fatalf("expected DeviceBindingID %q, got %q", binding.ID, *res.Request.DeviceBindingID)
	}

	// Confirma persistência em support_requests
	var dbBindingID sql.NullString
	query := `SELECT device_binding_id FROM support_requests WHERE id = ? AND tenant_id = ?`
	err = tdb.DB.QueryRowContext(ctx, query, res.Request.ID, "tenant-1").Scan(&dbBindingID)
	if err != nil {
		t.Fatalf("failed to query support_requests: %v", err)
	}
	if !dbBindingID.Valid || dbBindingID.String != binding.ID {
		t.Fatalf("expected DB device_binding_id = %q, got %v", binding.ID, dbBindingID)
	}

	_ = store
}

// 5 e 6. A resposta do POST carrega o equipamento e a telemetria Tactical diretamente no envelope.
func TestD025_ResponseCarriesDeviceAndTacticalTelemetry(t *testing.T) {
	setup := setupSupportAPITest(t, true, true)
	h := setup.srv.routes()

	binding, err := setup.bStore.Upsert(context.Background(), DeviceBinding{
		OwnerID:         setup.user1ID,
		TenantID:        setup.user1ID,
		Hostname:        "SDE-API-RCP-01",
		GLPIComputerID:  "59",
		TacticalAgentID: "tac-telemetry-1",
		MatchStatus:     "matched",
	})
	if err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	setup.mockTactical.getAgentFn = func(ctx context.Context, agentID string) (tactical.Agent, error) {
		if agentID == "tac-telemetry-1" {
			return tactical.Agent{
				AgentID:         "tac-telemetry-1",
				Hostname:        "SDE-API-RCP-01",
				OperatingSystem: "Windows 11 Pro",
				Status:          "online",
				LastSeenValid:   true,
			}, nil
		}
		return tactical.Agent{}, errors.New("agent not found")
	}

	createReq := CreateSupportTicketReq{
		RequesterName: "Carlos Souza",
		Title:         "Computador Lento",
		Description:   "Máquina travando frequentemente",
		Hostname:      &binding.Hostname,
		Priority:      3,
	}
	bodyBytes, _ := json.Marshal(createReq)

	rec := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, bodyBytes, map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-d025-api-resp-1",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d. body: %s", rec.Code, rec.Body.String())
	}

	// Decodifica diretamente o envelope retornado pelo POST
	var respEnv struct {
		SupportRequest SupportRequestPublicDTO `json:"supportRequest"`
		Device         *DeviceBindingPublicDTO `json:"device"`
		TacticalAgent  *tactical.Agent         `json:"tacticalAgent"`
		Warnings       []string                `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &respEnv); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// 5. Retorno do equipamento vinculado diretamente na resposta do POST
	if respEnv.SupportRequest.DeviceBindingID == nil || *respEnv.SupportRequest.DeviceBindingID != binding.ID {
		t.Fatalf("expected SupportRequest.DeviceBindingID %q, got %v", binding.ID, respEnv.SupportRequest.DeviceBindingID)
	}
	if respEnv.Device == nil || respEnv.Device.ID != binding.ID {
		t.Fatalf("expected Device in response envelope with ID %q, got %v", binding.ID, respEnv.Device)
	}

	// 6. Telemetria Tactical presente DIRETAMENTE na resposta do POST (sem requisição GET posterior)
	if respEnv.TacticalAgent == nil || respEnv.TacticalAgent.AgentID != "tac-telemetry-1" {
		t.Fatalf("expected tactical telemetry with agentId 'tac-telemetry-1' on POST response, got %v", respEnv.TacticalAgent)
	}
	if respEnv.TacticalAgent.OperatingSystem != "Windows 11 Pro" {
		t.Fatalf("expected OS 'Windows 11 Pro' on POST response, got %q", respEnv.TacticalAgent.OperatingSystem)
	}
	if len(respEnv.Warnings) != 0 {
		t.Fatalf("expected empty warnings on healthy tactical response, got %v", respEnv.Warnings)
	}

	// Replay idempotente: 2ª chamada POST idêntica retorna o mesmo envelope com device e telemetria
	recReplay := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, bodyBytes, map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-d025-api-resp-1",
	})
	if recReplay.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on replay, got %d", recReplay.Code)
	}
	var replayEnv struct {
		SupportRequest SupportRequestPublicDTO `json:"supportRequest"`
		Device         *DeviceBindingPublicDTO `json:"device"`
		TacticalAgent  *tactical.Agent         `json:"tacticalAgent"`
		Warnings       []string                `json:"warnings"`
	}
	if err := json.Unmarshal(recReplay.Body.Bytes(), &replayEnv); err != nil {
		t.Fatalf("unmarshal replay error: %v", err)
	}
	if replayEnv.Device == nil || replayEnv.Device.ID != binding.ID {
		t.Fatalf("expected Device on replay response, got %v", replayEnv.Device)
	}
	if replayEnv.TacticalAgent == nil || replayEnv.TacticalAgent.AgentID != "tac-telemetry-1" {
		t.Fatalf("expected TacticalAgent on replay response, got %v", replayEnv.TacticalAgent)
	}
}

// Telemetria Tactical: comportamento com falha do Tactical e com Tactical desabilitado.
func TestD025_ResponseTacticalFallbackAndWarnings(t *testing.T) {
	t.Run("tactical unavailable adds warning and tacticalAgent null", func(t *testing.T) {
		setup := setupSupportAPITest(t, true, true)
		h := setup.srv.routes()

		binding, err := setup.bStore.Upsert(context.Background(), DeviceBinding{
			OwnerID:         setup.user1ID,
			TenantID:        setup.user1ID,
			Hostname:        "SDE-ERR-TAC-01",
			GLPIComputerID:  "60",
			TacticalAgentID: "tac-error-agent",
			MatchStatus:     "matched",
		})
		if err != nil {
			t.Fatalf("failed to create binding: %v", err)
		}

		// Mock falha no GetAgent
		setup.mockTactical.getAgentFn = func(ctx context.Context, agentID string) (tactical.Agent, error) {
			return tactical.Agent{}, errors.New("upstream tactical timeout")
		}

		createReq := CreateSupportTicketReq{
			RequesterName: "Maria",
			Title:         "Monitor piscando",
			Description:   "Descrição",
			Hostname:      &binding.Hostname,
			Priority:      3,
		}
		bodyBytes, _ := json.Marshal(createReq)

		rec := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, bodyBytes, map[string]string{
			"Content-Type":    "application/json",
			"Idempotency-Key": "idemp-d025-tac-fail-1",
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d", rec.Code)
		}

		var respEnv struct {
			SupportRequest SupportRequestPublicDTO `json:"supportRequest"`
			Device         *DeviceBindingPublicDTO `json:"device"`
			TacticalAgent  *tactical.Agent         `json:"tacticalAgent"`
			Warnings       []string                `json:"warnings"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &respEnv); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if respEnv.TacticalAgent != nil {
			t.Fatalf("expected tacticalAgent=nil on tactical failure, got %v", respEnv.TacticalAgent)
		}
		hasWarning := false
		for _, w := range respEnv.Warnings {
			if w == "tactical_unavailable" {
				hasWarning = true
				break
			}
		}
		if !hasWarning {
			t.Fatalf("expected warning 'tactical_unavailable', got %v", respEnv.Warnings)
		}
	})

	t.Run("tactical disabled returns tacticalAgent null without warnings", func(t *testing.T) {
		setup := setupSupportAPITest(t, true, false) // tacticalConfigured = false
		h := setup.srv.routes()

		binding, err := setup.bStore.Upsert(context.Background(), DeviceBinding{
			OwnerID:         setup.user1ID,
			TenantID:        setup.user1ID,
			Hostname:        "SDE-DIS-TAC-01",
			GLPIComputerID:  "61",
			TacticalAgentID: "tac-ignored-agent",
			MatchStatus:     "matched",
		})
		if err != nil {
			t.Fatalf("failed to create binding: %v", err)
		}

		createReq := CreateSupportTicketReq{
			RequesterName: "Maria",
			Title:         "Monitor piscando",
			Description:   "Descrição",
			Hostname:      &binding.Hostname,
			Priority:      3,
		}
		bodyBytes, _ := json.Marshal(createReq)

		rec := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, bodyBytes, map[string]string{
			"Content-Type":    "application/json",
			"Idempotency-Key": "idemp-d025-tac-dis-1",
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d", rec.Code)
		}

		var respEnv struct {
			SupportRequest SupportRequestPublicDTO `json:"supportRequest"`
			Device         *DeviceBindingPublicDTO `json:"device"`
			TacticalAgent  *tactical.Agent         `json:"tacticalAgent"`
			Warnings       []string                `json:"warnings"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &respEnv); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if respEnv.TacticalAgent != nil {
			t.Fatalf("expected tacticalAgent=nil when disabled, got %v", respEnv.TacticalAgent)
		}
		if len(respEnv.Warnings) != 0 {
			t.Fatalf("expected no warnings when tactical is disabled, got %v", respEnv.Warnings)
		}
	})
}

// mockBindingsStoreWithError permite injetar falhas no repositório de bindings
type mockBindingsStoreWithError struct {
	deviceBindingStoreBackend
	findByHostnameErr error
	upsertErr         error
}

func (m *mockBindingsStoreWithError) FindByHostname(ctx context.Context, tenantID, hostname string) (DeviceBinding, bool, error) {
	if m.findByHostnameErr != nil {
		return DeviceBinding{}, false, m.findByHostnameErr
	}
	return m.deviceBindingStoreBackend.FindByHostname(ctx, tenantID, hostname)
}

func (m *mockBindingsStoreWithError) Upsert(ctx context.Context, b DeviceBinding) (DeviceBinding, error) {
	if m.upsertErr != nil {
		return DeviceBinding{}, m.upsertErr
	}
	return m.deviceBindingStoreBackend.Upsert(ctx, b)
}

// 1. Falhas em FindByHostname e Upsert não são silenciadas e impedem chamada ao GLPI CreateTicket.
func TestD025_InjectedStoreErrors_NeverCallsGLPI(t *testing.T) {
	ctx := context.Background()

	t.Run("FindByHostname error aborts before GLPI CreateTicket", func(t *testing.T) {
		_, store, bStore, mockG, mockT, _ := setupTestSupportService(t)
		injectedErr := errors.New("database connection broken during FindByHostname")
		spyBindings := &mockBindingsStoreWithError{
			deviceBindingStoreBackend: bStore,
			findByHostnameErr:         injectedErr,
		}
		svc := NewSupportService(store, spyBindings, mockG, mockT, slog.Default())

		input := sampleServiceCreateInput("tenant-1", "idemp-d025-fail-find")
		host := "SDE-ERR-HOST"
		input.Hostname = &host
		input.DeviceBindingID = nil

		res, err := svc.CreateTicket(ctx, input)
		if err == nil {
			t.Fatalf("expected error from CreateTicket when FindByHostname fails, got nil")
		}
		if !errors.Is(err, injectedErr) {
			t.Fatalf("expected injected error %v, got %v", injectedErr, err)
		}
		if res != nil {
			t.Fatalf("expected nil result on failure, got %v", res)
		}
		if len(mockG.createTicketCalls) != 0 {
			t.Fatalf("CRITICAL: GLPI CreateTicket must NEVER be called when local store fails! calls: %d", len(mockG.createTicketCalls))
		}
	})

	t.Run("Upsert error aborts before GLPI CreateTicket", func(t *testing.T) {
		_, store, bStore, mockG, mockT, _ := setupTestSupportService(t)
		injectedErr := errors.New("database disk full during Upsert")
		spyBindings := &mockBindingsStoreWithError{
			deviceBindingStoreBackend: bStore,
			upsertErr:                 injectedErr,
		}
		svc := NewSupportService(store, spyBindings, mockG, mockT, slog.Default())

		// Upstream localiza computador, mas Upsert do binding falhará
		mockG.findComputerFn = func(ctx context.Context, hostname string) (glpi.Computer, error) {
			return glpi.Computer{ID: "77", Name: hostname}, nil
		}

		input := sampleServiceCreateInput("tenant-1", "idemp-d025-fail-upsert")
		host := "SDE-UP-HOST"
		input.Hostname = &host
		input.DeviceBindingID = nil

		res, err := svc.CreateTicket(ctx, input)
		if err == nil {
			t.Fatalf("expected error from CreateTicket when Upsert fails, got nil")
		}
		if !errors.Is(err, injectedErr) {
			t.Fatalf("expected injected error %v, got %v", injectedErr, err)
		}
		if res != nil {
			t.Fatalf("expected nil result on failure, got %v", res)
		}
		if len(mockG.createTicketCalls) != 0 {
			t.Fatalf("CRITICAL: GLPI CreateTicket must NEVER be called when local Upsert fails! calls: %d", len(mockG.createTicketCalls))
		}
	})
}

// 3. Cobertura de multitenancy automática: mesmo hostname em tenants diferentes
// vincula exclusivamente o binding do tenant correto e nunca vaza dados entre tenants.
func TestD025_MultiTenancyAutomaticBinding(t *testing.T) {
	setup := setupSupportAPITest(t, true, true)
	h := setup.srv.routes()
	ctx := context.Background()

	// Tenant 1 (user-1) possui SDE-SHARED-PC com GLPIComputerID "101"
	bTenant1, err := setup.bStore.Upsert(ctx, DeviceBinding{
		OwnerID:        setup.user1ID,
		TenantID:       setup.user1ID,
		Hostname:       "SDE-SHARED-PC",
		GLPIComputerID: "101",
		MatchStatus:    "matched",
	})
	if err != nil {
		t.Fatalf("failed to create binding tenant 1: %v", err)
	}

	// Tenant 2 (user-2) possui O MESMO hostname SDE-SHARED-PC com GLPIComputerID "202"
	bTenant2, err := setup.bStore.Upsert(ctx, DeviceBinding{
		OwnerID:        setup.user2ID,
		TenantID:       setup.user2ID,
		Hostname:       "SDE-SHARED-PC",
		GLPIComputerID: "202",
		MatchStatus:    "matched",
	})
	if err != nil {
		t.Fatalf("failed to create binding tenant 2: %v", err)
	}

	if bTenant1.ID == bTenant2.ID {
		t.Fatalf("bindings across tenants must have distinct IDs, got identical %q", bTenant1.ID)
	}

	// Usuário do Tenant 1 abre chamado informando apenas o hostname compartilhado
	host := "SDE-SHARED-PC"
	createReq := CreateSupportTicketReq{
		RequesterName: "Solicitante Tenant 1",
		Title:         "Chamado Tenant 1",
		Description:   "Problema na máquina do Tenant 1",
		Hostname:      &host,
		Priority:      3,
	}
	bodyBytes, _ := json.Marshal(createReq)

	rec := doRequest(h, "POST", fmt.Sprintf("/api/sessions/%s/chats/%s/support/ticket", setup.sess1ID, setup.chat1JID), setup.user1Token, bodyBytes, map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "idemp-d025-multitenant-t1",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d. body: %s", rec.Code, rec.Body.String())
	}

	var respEnv SupportTicketResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &respEnv); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Comprova associação exclusiva ao binding do Tenant 1
	if respEnv.SupportRequest.DeviceBindingID == nil || *respEnv.SupportRequest.DeviceBindingID != bTenant1.ID {
		t.Fatalf("expected SupportRequest.DeviceBindingID %q (Tenant 1), got %v", bTenant1.ID, respEnv.SupportRequest.DeviceBindingID)
	}
	if respEnv.Device == nil || respEnv.Device.ID != bTenant1.ID {
		t.Fatalf("expected Device %q (Tenant 1), got %v", bTenant1.ID, respEnv.Device)
	}
	if respEnv.Device.GLPIComputerID != "101" {
		t.Fatalf("expected GLPIComputerID '101' for Tenant 1, got %q", respEnv.Device.GLPIComputerID)
	}

	// Comprova que Tenant 1 não consegue consultar o binding do Tenant 2 (segregação de dados)
	recLeak := doRequest(h, "GET", fmt.Sprintf("/api/support/devices/%s", bTenant2.ID), setup.user1Token, nil, nil)
	if recLeak.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant device access must return 404, got %d", recLeak.Code)
	}
}

// 7. Hostname inexistente: não associa equipamento nem inventa binding.
func TestD025_NonExistentHostname_NoBinding(t *testing.T) {
	svc, store, bStore, mockG, mockT, _ := setupTestSupportService(t)
	ctx := context.Background()

	mockG.findComputerFn = func(ctx context.Context, hostname string) (glpi.Computer, error) {
		return glpi.Computer{}, glpi.ErrNotFound
	}
	mockT.findAgentFn = func(ctx context.Context, hostname string) (tactical.Agent, error) {
		return tactical.Agent{}, errors.New("not found")
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-d025-nonexist-1")
	hostInput := "COMPUTADOR-FANTASMA-99"
	input.Hostname = &hostInput
	input.DeviceBindingID = nil

	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	if res.Request.DeviceBindingID != nil {
		t.Fatalf("expected nil DeviceBindingID for non-existent host, got %v", res.Request.DeviceBindingID)
	}

	dbReq, err := store.GetByID(ctx, "tenant-1", res.Request.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if dbReq.DeviceBindingID != nil {
		t.Fatalf("expected DB device_binding_id=nil, got %v", dbReq.DeviceBindingID)
	}

	// Nenhum binding criado
	_, found, _ := bStore.FindByHostname(ctx, "tenant-1", "COMPUTADOR-FANTASMA-99")
	if found {
		t.Fatalf("expected no binding in store for non-existent host")
	}
}

// 8. Correspondência ambígua: não associa silenciosamente.
func TestD025_AmbiguousHostname_NoBinding(t *testing.T) {
	svc, store, _, mockG, mockT, _ := setupTestSupportService(t)
	ctx := context.Background()

	mockG.findComputerFn = func(ctx context.Context, hostname string) (glpi.Computer, error) {
		return glpi.Computer{}, glpi.ErrAmbiguous
	}
	mockT.findAgentFn = func(ctx context.Context, hostname string) (tactical.Agent, error) {
		return tactical.Agent{}, tactical.ErrAmbiguous
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-d025-ambig-1")
	hostInput := "AMBIGUOUS-PC"
	input.Hostname = &hostInput
	input.DeviceBindingID = nil

	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	// Ambiguidade não associa silenciosamente
	if res.Request.DeviceBindingID != nil {
		t.Fatalf("expected nil DeviceBindingID on ambiguous match, got %v", res.Request.DeviceBindingID)
	}

	dbReq, err := store.GetByID(ctx, "tenant-1", res.Request.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if dbReq.DeviceBindingID != nil {
		t.Fatalf("expected nil DB device_binding_id on ambiguous match, got %v", dbReq.DeviceBindingID)
	}
}

// 9. Binding sem Tactical (missing_tactical): associa GLPI corretamente e não inventa tactical_agent_id.
func TestD025_BindingWithoutTactical(t *testing.T) {
	svc, store, bStore, _, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	binding, err := bStore.Upsert(ctx, DeviceBinding{
		OwnerID:         "user-1",
		TenantID:        "tenant-1",
		Hostname:        "SDE-GLPI-ONLY",
		GLPIComputerID:  "59",
		TacticalAgentID: "",
		MatchStatus:     "missing_tactical",
	})
	if err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-d025-glpi-only-1")
	hostInput := "SDE-GLPI-ONLY"
	input.Hostname = &hostInput
	input.DeviceBindingID = nil

	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	if res.Request.DeviceBindingID == nil || *res.Request.DeviceBindingID != binding.ID {
		t.Fatalf("expected DeviceBindingID %q, got %v", binding.ID, res.Request.DeviceBindingID)
	}
	if res.Request.TicketGLPIComputerID == nil || *res.Request.TicketGLPIComputerID != "59" {
		t.Fatalf("expected TicketGLPIComputerID '59', got %v", res.Request.TicketGLPIComputerID)
	}

	// Confirma que tactical_agent_id no binding não foi inventado
	bFromStore, err := bStore.GetForTenant(ctx, "tenant-1", binding.ID)
	if err != nil {
		t.Fatalf("bStore.GetForTenant failed: %v", err)
	}
	if bFromStore.TacticalAgentID != "" {
		t.Fatalf("expected empty TacticalAgentID, got %q", bFromStore.TacticalAgentID)
	}

	_ = store
}

// 10. Binding completo GLPI + Tactical: ambos IDs mantidos e device_binding_id persistido.
func TestD025_BindingCompleteGLPIAndTactical(t *testing.T) {
	svc, _, bStore, _, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	binding, err := bStore.Upsert(ctx, DeviceBinding{
		OwnerID:         "user-1",
		TenantID:        "tenant-1",
		Hostname:        "SDE-FULL-EQUIP",
		GLPIComputerID:  "59",
		TacticalAgentID: "tac-999",
		MatchStatus:     "matched",
	})
	if err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-d025-full-1")
	hostInput := "SDE-FULL-EQUIP"
	input.Hostname = &hostInput
	input.DeviceBindingID = nil

	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	if res.Request.DeviceBindingID == nil || *res.Request.DeviceBindingID != binding.ID {
		t.Fatalf("expected DeviceBindingID %q, got %v", binding.ID, res.Request.DeviceBindingID)
	}
	if res.Request.TicketGLPIComputerID == nil || *res.Request.TicketGLPIComputerID != "59" {
		t.Fatalf("expected TicketGLPIComputerID '59', got %v", res.Request.TicketGLPIComputerID)
	}

	b, _ := bStore.GetForTenant(ctx, "tenant-1", binding.ID)
	if b.GLPIComputerID != "59" || b.TacticalAgentID != "tac-999" {
		t.Fatalf("expected both IDs preserved: glpi=59, tactical=tac-999; got glpi=%q, tac=%q", b.GLPIComputerID, b.TacticalAgentID)
	}
}

// 11. Replay idempotente sem duplicação: não cria outro chamado e preserva o mesmo device_binding_id.
func TestD025_IdempotentReplay_PreservesBindingID(t *testing.T) {
	svc, store, bStore, mockG, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	binding, err := bStore.Upsert(ctx, DeviceBinding{
		OwnerID:        "user-1",
		TenantID:       "tenant-1",
		Hostname:       "SDE-ARS-RCP-02",
		GLPIComputerID: "59",
		MatchStatus:    "matched",
	})
	if err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	input := sampleServiceCreateInput("tenant-1", "idemp-d025-replay-key")
	hostInput := "SDE-ARS-RCP-02"
	input.Hostname = &hostInput
	input.DeviceBindingID = nil

	res1, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("1st CreateTicket failed: %v", err)
	}
	if res1.IsReplay {
		t.Fatalf("expected 1st call to not be replay")
	}
	if res1.Request.DeviceBindingID == nil || *res1.Request.DeviceBindingID != binding.ID {
		t.Fatalf("expected DeviceBindingID %q, got %v", binding.ID, res1.Request.DeviceBindingID)
	}

	// 2ª chamada idêntica (replay)
	res2, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("2nd CreateTicket failed: %v", err)
	}
	if !res2.IsReplay {
		t.Fatalf("expected 2nd call to be replay")
	}
	if res2.Request.ID != res1.Request.ID {
		t.Fatalf("expected same request ID on replay, got %q vs %q", res1.Request.ID, res2.Request.ID)
	}
	if res2.Request.DeviceBindingID == nil || *res2.Request.DeviceBindingID != binding.ID {
		t.Fatalf("expected same DeviceBindingID %q on replay, got %v", binding.ID, res2.Request.DeviceBindingID)
	}

	// Exatamente 1 chamada ao GLPI
	if len(mockG.createTicketCalls) != 1 {
		t.Fatalf("expected 1 call to GLPI CreateTicket, got %d", len(mockG.createTicketCalls))
	}

	// Exatamente 1 ticket no banco
	list, err := store.ListByConversation(ctx, "tenant-1", input.SessionID, input.ChatJID, 10)
	if err != nil {
		t.Fatalf("ListByConversation failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 ticket in conversation, got %d", len(list))
	}
}

// 12. Regressão da criação sem equipamento: chamado sem hostname nem binding funciona normalmente.
func TestD025_Regression_CreationWithoutEquipment(t *testing.T) {
	svc, store, _, _, _, _ := setupTestSupportService(t)
	ctx := context.Background()

	input := sampleServiceCreateInput("tenant-1", "idemp-d025-no-equip-1")
	input.Hostname = nil
	input.DeviceBindingID = nil

	res, err := svc.CreateTicket(ctx, input)
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	if res.Request.DeviceBindingID != nil {
		t.Fatalf("expected nil DeviceBindingID, got %v", res.Request.DeviceBindingID)
	}
	if res.Request.HostnameInformed != "" {
		t.Fatalf("expected empty HostnameInformed, got %q", res.Request.HostnameInformed)
	}

	dbReq, err := store.GetByID(ctx, "tenant-1", res.Request.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if dbReq.DeviceBindingID != nil {
		t.Fatalf("expected nil DB device_binding_id, got %v", dbReq.DeviceBindingID)
	}
}
