package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *server) registerSupportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{sid}/chats/{jid}/support", s.requireAuth(s.handleGetChatSupport))
	mux.HandleFunc("POST /api/sessions/{sid}/chats/{jid}/support/ticket", s.requireAuth(s.handleCreateChatSupportTicket))
	mux.HandleFunc("GET /api/support/requests/{id}", s.requireAuth(s.handleGetSupportRequest))
	mux.HandleFunc("POST /api/support/requests/{id}/retry", s.requireAuth(s.handleRetrySupportRequest))
	mux.HandleFunc("POST /api/support/requests/{id}/reconcile", s.requireAuth(s.handleReconcileSupportRequest))
	mux.HandleFunc("GET /api/support/devices", s.requireAuth(s.handleSearchSupportDevices))
	mux.HandleFunc("GET /api/support/devices/{id}", s.requireAuth(s.handleGetSupportDevice))
	mux.HandleFunc("PUT /api/support/requests/{id}/device", s.requireAuth(s.handleUpdateSupportRequestDevice))
}

type supportServiceAPI interface {
	ListByConversation(ctx context.Context, tenantID, sessionID, chatJID string, limit int) ([]*SupportRequest, error)
	CreateTicket(ctx context.Context, in ServiceCreateTicketInput) (*ServiceCreateTicketResult, error)
	GetByID(ctx context.Context, tenantID, id string) (*SupportRequest, error)
	Retry(ctx context.Context, in ServiceRetryInput) (*SupportRequest, error)
	Reconcile(ctx context.Context, in ServiceReconcileInput) (*SupportRequest, error)
	UpdateDevice(ctx context.Context, in ServiceUpdateDeviceInput) (*SupportRequest, bool, error)
}

func (s *server) isSupportEnabled() bool {
	if s == nil || s.supportSvc == nil {
		return false
	}
	if svc, ok := s.supportSvc.(*SupportService); ok && svc == nil {
		return false
	}
	return true
}

func (s *server) isTacticalEnabled() bool {
	return s != nil && s.tacticalClient != nil
}

func (s *server) toPublicSupportRequestDTO(r *SupportRequest) SupportRequestPublicDTO {
	webBase := ""
	if s != nil && s.supportCfg != nil {
		webBase = s.supportCfg.GLPIWebBaseURL
	}
	return toPublicSupportRequestDTO(r, webBase)
}

func isValidIdempotencyKey(k string) bool {
	return ValidateIdempotencyKey(k) == nil
}

func isValidID(id string, maxLen int) bool {
	if len(id) == 0 || len(id) > maxLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] <= 32 || id[i] > 126 {
			return false
		}
	}
	return true
}

func isPositiveDecimal(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n > 0
}

func (s *server) checkSessionAndChatAccess(r *http.Request, u *currentUser, sid, jid string) (int, string) {
	if len(sid) < 1 || len(sid) > 128 || len(jid) < 1 || len(jid) > 255 {
		return http.StatusBadRequest, "invalid_conversation_id"
	}
	_, exists := s.sessions.Get(sid)
	if !exists {
		return http.StatusNotFound, "session_not_found"
	}
	if !s.userCanAccessSession(u, sid) {
		tenantOfSession := s.sessions.tenantOf(sid)
		if tenantOfSession == "" || tenantOfSession != u.TenantID() {
			return http.StatusNotFound, "session_not_found"
		}
		return http.StatusForbidden, "session_access_denied"
	}
	if s.chatMeta != nil {
		_, found, err := s.chatMeta.Get(r.Context(), sid, jid)
		if err != nil || !found {
			return http.StatusNotFound, "chat_not_found"
		}
	}
	return http.StatusOK, ""
}

func (s *server) handleGetChatSupport(w http.ResponseWriter, r *http.Request) {
	if !s.isSupportEnabled() {
		writeSupportError(w, http.StatusServiceUnavailable, "support_disabled", "support feature is disabled", 0)
		return
	}
	if !ensureNoBody(r) {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "GET request must not have a body", 0)
		return
	}
	u := currentUserFromReq(r)
	if u == nil {
		writeSupportError(w, http.StatusUnauthorized, "unauthorized", "unauthorized", 0)
		return
	}
	sid := r.PathValue("sid")
	jid := r.PathValue("jid")

	status, code := s.checkSessionAndChatAccess(r, u, sid, jid)
	if status != http.StatusOK {
		writeSupportError(w, status, code, code, 0)
		return
	}

	requests, err := s.supportSvc.ListByConversation(r.Context(), u.TenantID(), sid, jid, 20)
	if err != nil {
		writeSupportError(w, http.StatusInternalServerError, "internal_error", "failed to list support requests", 0)
		return
	}

	var dtos []SupportRequestPublicDTO
	for _, req := range requests {
		dtos = append(dtos, s.toPublicSupportRequestDTO(req))
	}
	if dtos == nil {
		dtos = []SupportRequestPublicDTO{}
	}

	var current *SupportRequestPublicDTO
	if len(dtos) > 0 {
		current = &dtos[0]
	}

	writeJSON(w, http.StatusOK, ConversationSupportPanelResponse{
		SupportRequests:       dtos,
		CurrentSupportRequest: current,
	})
}

func (s *server) handleCreateChatSupportTicket(w http.ResponseWriter, r *http.Request) {
	if !s.isSupportEnabled() {
		writeSupportError(w, http.StatusServiceUnavailable, "support_disabled", "support feature is disabled", 0)
		return
	}
	u := currentUserFromReq(r)
	if u == nil {
		writeSupportError(w, http.StatusUnauthorized, "unauthorized", "unauthorized", 0)
		return
	}

	if err := enforceJSONContentType(r); err != nil {
		writeSupportError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", 0)
		return
	}

	idempKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !isValidIdempotencyKey(idempKey) {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "valid Idempotency-Key header required", 0)
		return
	}

	sid := r.PathValue("sid")
	jid := r.PathValue("jid")

	status, code := s.checkSessionAndChatAccess(r, u, sid, jid)
	if status != http.StatusOK {
		writeSupportError(w, status, code, code, 0)
		return
	}

	var body CreateSupportTicketReq
	if err := decodeJSONStrict(r, 16384, &body); err != nil {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", 0)
		return
	}

	reqName := strings.TrimSpace(body.RequesterName)
	if len(reqName) < 1 || len(reqName) > 120 {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "requesterName must be between 1 and 120 bytes", 0)
		return
	}
	title := strings.TrimSpace(body.Title)
	if len(title) < 1 || len(title) > 200 {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "title must be between 1 and 200 bytes", 0)
		return
	}
	desc := strings.TrimSpace(body.Description)
	if len(desc) < 1 || len(desc) > 8000 {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "description must be between 1 and 8000 bytes", 0)
		return
	}

	priority := body.Priority
	if priority == 0 {
		priority = 3
	} else if priority < 1 || priority > 5 {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "priority must be between 1 and 5", 0)
		return
	}

	var resolvedHostname *string
	if body.Hostname != nil && strings.TrimSpace(*body.Hostname) != "" {
		h := strings.TrimSpace(*body.Hostname)
		if len(h) > 64 {
			writeSupportError(w, http.StatusBadRequest, "invalid_request", "hostname cannot exceed 64 bytes", 0)
			return
		}
		resolvedHostname = &h
	}

	var resolvedBindingID *string
	if body.DeviceBindingID != nil && strings.TrimSpace(*body.DeviceBindingID) != "" {
		bID := strings.TrimSpace(*body.DeviceBindingID)
		if !isValidID(bID, 128) {
			writeSupportError(w, http.StatusBadRequest, "invalid_request", "invalid deviceBindingId", 0)
			return
		}
		binding, err := s.bindings.GetForTenant(r.Context(), u.TenantID(), bID)
		if err != nil {
			if errors.Is(err, ErrDeviceBindingNotFound) {
				writeSupportError(w, http.StatusNotFound, "not_found", "device binding not found", 0)
				return
			}
			writeSupportError(w, http.StatusInternalServerError, "internal_error", "failed to query device binding", 0)
			return
		}
		if resolvedHostname != nil {
			if normalizeHostname(*resolvedHostname) != binding.HostnameNormalized {
				writeSupportError(w, http.StatusUnprocessableEntity, "device_mismatch", "hostname does not match device binding", 0)
				return
			}
		} else {
			resolvedHostname = &binding.Hostname
		}
		resolvedBindingID = &binding.ID
	}

	var categoryID *string
	if body.CategoryID != nil && strings.TrimSpace(*body.CategoryID) != "" {
		cID := strings.TrimSpace(*body.CategoryID)
		if len(cID) > 32 || !isPositiveDecimal(cID) {
			writeSupportError(w, http.StatusBadRequest, "invalid_request", "categoryId must be a positive decimal string", 0)
			return
		}
		categoryID = &cID
	}

	var locationID *string
	if body.LocationID != nil && strings.TrimSpace(*body.LocationID) != "" {
		lID := strings.TrimSpace(*body.LocationID)
		if len(lID) > 32 || !isPositiveDecimal(lID) {
			writeSupportError(w, http.StatusBadRequest, "invalid_request", "locationId must be a positive decimal string", 0)
			return
		}
		locationID = &lID
	}

	svcInput := ServiceCreateTicketInput{
		TenantID:        u.TenantID(),
		OwnerID:         u.ID,
		ActorUserID:     u.ID,
		SessionID:       sid,
		ChatJID:         jid,
		IdempotencyKey:  idempKey,
		RequesterName:   reqName,
		Title:           title,
		Description:     desc,
		Hostname:        resolvedHostname,
		DeviceBindingID: resolvedBindingID,
		CategoryID:      categoryID,
		LocationID:      locationID,
		Priority:        priority,
	}

	res, err := s.supportSvc.CreateTicket(r.Context(), svcInput)
	if err != nil {
		if errors.Is(err, ErrIdempotencyKeyReused) {
			writeSupportError(w, http.StatusConflict, "idempotency_conflict", "idempotency key already used with different payload", 0)
			return
		}
		if res == nil {
			writeSupportError(w, http.StatusInternalServerError, "internal_error", "failed to create support ticket", 0)
			return
		}
	}

	dto := s.toPublicSupportRequestDTO(res.Request)
	respEnv := SupportTicketResponseEnvelope{
		SupportRequest: dto,
		Warnings:       []string{},
	}

	if res.IsReplay {
		writeJSON(w, http.StatusOK, respEnv)
		return
	}

	switch res.Request.SyncState {
	case StateSynced:
		writeJSON(w, http.StatusCreated, respEnv)
	case StateRetryableError:
		if res.Request.LastErrorCode == "rate_limited" {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, respEnv)
			return
		}
		if res.Request.LastErrorCode == "integration_auth_failed" {
			writeJSON(w, http.StatusBadGateway, respEnv)
			return
		}
		writeJSON(w, http.StatusAccepted, respEnv)
	case StateUnknown:
		writeJSON(w, http.StatusAccepted, respEnv)
	case StateFailed:
		writeJSON(w, http.StatusBadGateway, respEnv)
	default:
		writeJSON(w, http.StatusOK, respEnv)
	}
}

func (s *server) handleGetSupportRequest(w http.ResponseWriter, r *http.Request) {
	if !s.isSupportEnabled() {
		writeSupportError(w, http.StatusServiceUnavailable, "support_disabled", "support feature is disabled", 0)
		return
	}
	if !ensureNoBody(r) {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "GET request must not have a body", 0)
		return
	}
	u := currentUserFromReq(r)
	if u == nil {
		writeSupportError(w, http.StatusUnauthorized, "unauthorized", "unauthorized", 0)
		return
	}
	id := r.PathValue("id")
	if !isValidID(id, 36) {
		writeSupportError(w, http.StatusBadRequest, "invalid_id", "invalid support request id", 0)
		return
	}
	req, err := s.supportSvc.GetByID(r.Context(), u.TenantID(), id)
	if err != nil || req == nil {
		writeSupportError(w, http.StatusNotFound, "not_found", "support request not found", 0)
		return
	}
	if !s.userCanAccessSession(u, req.SessionID) {
		writeSupportError(w, http.StatusForbidden, "forbidden", "access denied to support request conversation", 0)
		return
	}
	dto := s.toPublicSupportRequestDTO(req)
	var deviceDTO *DeviceBindingPublicDTO
	if req.DeviceBindingID != nil && *req.DeviceBindingID != "" {
		if b, bErr := s.bindings.GetForTenant(r.Context(), u.TenantID(), *req.DeviceBindingID); bErr == nil {
			d := toPublicDeviceBindingDTO(b)
			deviceDTO = &d
		}
	}
	writeJSON(w, http.StatusOK, SupportTicketResponseEnvelope{
		SupportRequest: dto,
		Device:         deviceDTO,
		Warnings:       []string{},
	})
}

func (s *server) handleRetrySupportRequest(w http.ResponseWriter, r *http.Request) {
	if !s.isSupportEnabled() {
		writeSupportError(w, http.StatusServiceUnavailable, "support_disabled", "support feature is disabled", 0)
		return
	}
	u := currentUserFromReq(r)
	if u == nil {
		writeSupportError(w, http.StatusUnauthorized, "unauthorized", "unauthorized", 0)
		return
	}
	if r.Body != nil {
		var buf [2]byte
		n, _ := r.Body.Read(buf[:])
		if n > 0 {
			if err := enforceJSONContentType(r); err != nil {
				writeSupportError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", 0)
				return
			}
			combined := io.MultiReader(bytes.NewReader(buf[:n]), r.Body)
			limited := io.LimitReader(combined, 1024+1)
			dec := json.NewDecoder(limited)
			dec.DisallowUnknownFields()
			var dummy struct{}
			if err := dec.Decode(&dummy); err != nil {
				writeSupportError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", 0)
				return
			}
			var trailing any
			if err := dec.Decode(&trailing); err != io.EOF {
				writeSupportError(w, http.StatusBadRequest, "invalid_request", "multiple JSON documents not allowed", 0)
				return
			}
		}
	}

	id := r.PathValue("id")
	if !isValidID(id, 36) {
		writeSupportError(w, http.StatusBadRequest, "invalid_id", "invalid support request id", 0)
		return
	}

	req, err := s.supportSvc.GetByID(r.Context(), u.TenantID(), id)
	if err != nil || req == nil {
		writeSupportError(w, http.StatusNotFound, "not_found", "support request not found", 0)
		return
	}

	if !s.userCanAccessSession(u, req.SessionID) {
		writeSupportError(w, http.StatusForbidden, "forbidden", "access denied to support request conversation", 0)
		return
	}

	if req.SyncState != StateRetryableError {
		writeSupportError(w, http.StatusConflict, "state_conflict", "only retryable_error requests can be retried", 0)
		return
	}

	res, err := s.supportSvc.Retry(r.Context(), ServiceRetryInput{
		ID:          id,
		TenantID:    u.TenantID(),
		ActorUserID: u.ID,
	})
	if err != nil {
		if errors.Is(err, ErrStateConflict) {
			writeSupportError(w, http.StatusConflict, "state_conflict", "concurrent retry in progress or state changed", 0)
			return
		}
		if res == nil {
			writeSupportError(w, http.StatusInternalServerError, "internal_error", "retry failed", 0)
			return
		}
	}

	dto := s.toPublicSupportRequestDTO(res)
	respEnv := SupportTicketResponseEnvelope{
		SupportRequest: dto,
		Warnings:       []string{},
	}

	switch res.SyncState {
	case StateSynced:
		writeJSON(w, http.StatusAccepted, respEnv)
	case StateRetryableError:
		if res.LastErrorCode == "rate_limited" {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, respEnv)
			return
		}
		if res.LastErrorCode == "integration_auth_failed" {
			writeJSON(w, http.StatusBadGateway, respEnv)
			return
		}
		writeJSON(w, http.StatusAccepted, respEnv)
	case StateUnknown:
		writeJSON(w, http.StatusAccepted, respEnv)
	case StateFailed:
		writeJSON(w, http.StatusBadGateway, respEnv)
	default:
		writeJSON(w, http.StatusOK, respEnv)
	}
}

func (s *server) handleReconcileSupportRequest(w http.ResponseWriter, r *http.Request) {
	if !s.isSupportEnabled() {
		writeSupportError(w, http.StatusServiceUnavailable, "support_disabled", "support feature is disabled", 0)
		return
	}
	u := currentUserFromReq(r)
	if u == nil {
		writeSupportError(w, http.StatusUnauthorized, "unauthorized", "unauthorized", 0)
		return
	}
	if !u.IsAdmin() {
		writeSupportError(w, http.StatusForbidden, "forbidden", "admin role required for reconcile", 0)
		return
	}
	if err := enforceJSONContentType(r); err != nil {
		writeSupportError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", 0)
		return
	}
	var body ReconcileReq
	if err := decodeJSONStrict(r, 4096, &body); err != nil {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", 0)
		return
	}
	id := r.PathValue("id")
	if !isValidID(id, 36) {
		writeSupportError(w, http.StatusBadRequest, "invalid_id", "invalid support request id", 0)
		return
	}

	req, err := s.supportSvc.GetByID(r.Context(), u.TenantID(), id)
	if err != nil || req == nil {
		writeSupportError(w, http.StatusNotFound, "not_found", "support request not found", 0)
		return
	}

	outcome := strings.TrimSpace(body.Outcome)
	if outcome != "synced" && outcome != "safe_to_retry" && outcome != "processing_orphaned" {
		writeSupportError(w, http.StatusBadRequest, "invalid_outcome", "invalid reconcile outcome", 0)
		return
	}

	cutoff := time.Now().UTC().Add(-30 * time.Second).Unix()
	reconcileInput := ServiceReconcileInput{
		ID:           id,
		TenantID:     u.TenantID(),
		Outcome:      outcome,
		GLPITicketID: body.GLPITicketID,
		ActorUserID:  u.ID,
		Cutoff:       cutoff,
	}

	res, err := s.supportSvc.Reconcile(r.Context(), reconcileInput)
	if err != nil {
		if errors.Is(err, ErrStateConflict) {
			writeSupportError(w, http.StatusConflict, "state_conflict", "cannot reconcile request in its current state", 0)
			return
		}
		if errors.Is(err, ErrTicketNotFound) {
			writeSupportError(w, http.StatusNotFound, "not_found", "ticket not found in GLPI", 0)
			return
		}
		if errors.Is(err, ErrReconcileExternalIDMismatch) {
			writeSupportError(w, http.StatusUnprocessableEntity, "reconcile_external_id_mismatch", "ticket external_id does not match support request", 0)
			return
		}
		if errors.Is(err, ErrInvalidReconcileOutcome) {
			writeSupportError(w, http.StatusBadRequest, "invalid_outcome", "invalid reconcile outcome", 0)
			return
		}
		writeSupportError(w, http.StatusInternalServerError, "internal_error", "reconciliation failed", 0)
		return
	}

	dto := s.toPublicSupportRequestDTO(res)
	writeJSON(w, http.StatusOK, SupportTicketResponseEnvelope{
		SupportRequest: dto,
		Warnings:       []string{},
	})
}

func (s *server) handleSearchSupportDevices(w http.ResponseWriter, r *http.Request) {
	if !s.isSupportEnabled() {
		writeSupportError(w, http.StatusServiceUnavailable, "support_disabled", "support feature is disabled", 0)
		return
	}
	if !ensureNoBody(r) {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "GET request must not have a body", 0)
		return
	}
	u := currentUserFromReq(r)
	if u == nil {
		writeSupportError(w, http.StatusUnauthorized, "unauthorized", "unauthorized", 0)
		return
	}
	q := r.URL.Query().Get("query")
	if len(q) > 64 {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "query parameter too long", 0)
		return
	}
	limit := 20
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		n, err := strconv.Atoi(rawLimit)
		if err != nil || n <= 0 {
			writeSupportError(w, http.StatusBadRequest, "invalid_request", "limit must be a positive integer", 0)
			return
		}
		if n > 100 {
			n = 100
		}
		limit = n
	}
	bindings, err := s.bindings.Search(r.Context(), u.TenantID(), q)
	if err != nil {
		writeSupportError(w, http.StatusInternalServerError, "internal_error", "failed to search devices", 0)
		return
	}
	if len(bindings) > limit {
		bindings = bindings[:limit]
	}
	var dtos []DeviceBindingPublicDTO
	for _, b := range bindings {
		dtos = append(dtos, toPublicDeviceBindingDTO(b))
	}
	if dtos == nil {
		dtos = []DeviceBindingPublicDTO{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"devices": dtos,
	})
}

func (s *server) handleGetSupportDevice(w http.ResponseWriter, r *http.Request) {
	if !s.isSupportEnabled() {
		writeSupportError(w, http.StatusServiceUnavailable, "support_disabled", "support feature is disabled", 0)
		return
	}
	if !ensureNoBody(r) {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "GET request must not have a body", 0)
		return
	}
	u := currentUserFromReq(r)
	if u == nil {
		writeSupportError(w, http.StatusUnauthorized, "unauthorized", "unauthorized", 0)
		return
	}
	id := r.PathValue("id")
	if !isValidID(id, 128) {
		writeSupportError(w, http.StatusBadRequest, "invalid_id", "invalid device binding id", 0)
		return
	}
	binding, err := s.bindings.GetForTenant(r.Context(), u.TenantID(), id)
	if err != nil {
		if errors.Is(err, ErrDeviceBindingNotFound) {
			writeSupportError(w, http.StatusNotFound, "not_found", "device binding not found", 0)
			return
		}
		writeSupportError(w, http.StatusInternalServerError, "internal_error", "failed to fetch device binding", 0)
		return
	}
	dto := toPublicDeviceBindingDTO(binding)
	var warnings []string
	var tacticalAgent any = nil
	if s.isTacticalEnabled() && s.tacticalClient != nil {
		if binding.TacticalAgentID != "" {
			agent, err := s.tacticalClient.GetAgent(r.Context(), binding.TacticalAgentID)
			if err == nil {
				tacticalAgent = agent
			} else {
				warnings = append(warnings, "tactical_unavailable")
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device":        dto,
		"glpiComputer":  nil,
		"tacticalAgent": tacticalAgent,
		"warnings":      warnings,
	})
}

func (s *server) handleUpdateSupportRequestDevice(w http.ResponseWriter, r *http.Request) {
	if !s.isSupportEnabled() {
		writeSupportError(w, http.StatusServiceUnavailable, "support_disabled", "support feature is disabled", 0)
		return
	}
	u := currentUserFromReq(r)
	if u == nil {
		writeSupportError(w, http.StatusUnauthorized, "unauthorized", "unauthorized", 0)
		return
	}
	if err := enforceJSONContentType(r); err != nil {
		writeSupportError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", 0)
		return
	}
	var body UpdateDeviceReq
	if err := decodeJSONStrict(r, 4096, &body); err != nil {
		writeSupportError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body", 0)
		return
	}
	id := r.PathValue("id")
	if !isValidID(id, 36) {
		writeSupportError(w, http.StatusBadRequest, "invalid_id", "invalid support request id", 0)
		return
	}
	deviceBindingID := strings.TrimSpace(body.DeviceBindingID)
	if !isValidID(deviceBindingID, 128) {
		writeSupportError(w, http.StatusBadRequest, "invalid_device_binding_id", "invalid device binding id", 0)
		return
	}

	req, err := s.supportSvc.GetByID(r.Context(), u.TenantID(), id)
	if err != nil || req == nil {
		writeSupportError(w, http.StatusNotFound, "not_found", "support request not found", 0)
		return
	}

	if !s.userCanAccessSession(u, req.SessionID) {
		writeSupportError(w, http.StatusForbidden, "forbidden", "access denied to support request conversation", 0)
		return
	}

	binding, err := s.bindings.GetForTenant(r.Context(), u.TenantID(), deviceBindingID)
	if err != nil {
		if errors.Is(err, ErrDeviceBindingNotFound) {
			writeSupportError(w, http.StatusNotFound, "not_found", "device binding not found", 0)
			return
		}
		writeSupportError(w, http.StatusInternalServerError, "internal_error", "failed to fetch device binding", 0)
		return
	}

	updatedReq, glpiContextUpdated, err := s.supportSvc.UpdateDevice(r.Context(), ServiceUpdateDeviceInput{
		ID:              id,
		TenantID:        u.TenantID(),
		DeviceBindingID: binding.ID,
		ActorUserID:     u.ID,
	})
	if err != nil {
		writeSupportError(w, http.StatusInternalServerError, "internal_error", "failed to update device", 0)
		return
	}

	reqDTO := s.toPublicSupportRequestDTO(updatedReq)
	bDTO := toPublicDeviceBindingDTO(binding)
	writeJSON(w, http.StatusOK, SupportTicketResponseEnvelope{
		SupportRequest:     reqDTO,
		Device:             &bDTO,
		GLPIContextUpdated: &glpiContextUpdated,
		Warnings:           []string{},
	})
}
